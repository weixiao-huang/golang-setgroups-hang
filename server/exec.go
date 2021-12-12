package main

import (
	"context"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

type Exec struct {
	Command string
}

func (s *session) handleExec(ctx context.Context, req *ssh.Request) bool {
	var execReq Exec
	if err := ssh.Unmarshal(req.Payload, &execReq); err != nil {
		log.WithError(err).Error("Unmarshal exec")
		return false
	}

	cmd, err := makeCommand(ctx, execReq.Command)
	if err != nil {
		log.WithError(err).Error("Make command")
		return false
	}
	s.cmd = cmd
	if s.tty != nil {
		cmd.Stdin = s.tty
		cmd.Stdout = s.tty
		cmd.Stderr = s.tty
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.Setctty = true
		cmd.SysProcAttr.Setsid = true
	} else {
		cmd.Stdin = s.sshChan
		cmd.Stdout = s.sshChan
		cmd.Stderr = s.sshChan
	}
	log.WithField("Command", cmd.String()).Info("Execute")
	if err := cmd.Start(); err != nil {
		log.WithError(err).Error("Start command")
		return false
	}

	go func() {
		err := cmd.Wait()
		if err != nil {
			log.WithError(err).Error("Exit")
		} else {
			log.Info("Exit successfully")
		}
		s.sshChan.Close()
	}()
	return true
}

type Request struct {
	UID      int   // or 0 to not change
	GID      int   // or 0 to not change
	GIDs     []int // supplemental
	Path     string
	Env      []string
	Argv     []string // must include Path as argv[0]
	Dir      string
	NumFiles int // new nfile fd rlimit, or 0 to not change
}

// req is b64+gob encoded Request.
func makeCommand(ctx context.Context, req string) (*exec.Cmd, error) {
	lr := new(Request)
	d := gob.NewDecoder(base64.NewDecoder(base64.StdEncoding, strings.NewReader(req)))
	err := d.Decode(lr)
	if err != nil {
		return nil, fmt.Errorf("decode Request in child: %v", err)
	}
	log.Infof("========= 1 ==========: %+v", lr)

	runtime.LockOSThread()
	if lr.NumFiles != 0 {
		var lim syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
			return nil, fmt.Errorf("get NOFILE rlimit: %v", err)
		}
		lim.Cur = uint64(lr.NumFiles)
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
			return nil, fmt.Errorf("set NOFILE rlimit: %v", err)
		}
	}
	log.Infof("========= 2 ==========: %+v", lr.GIDs)
	if len(lr.GIDs) != 0 {
		if err := syscall.Setgroups(lr.GIDs); err != nil {
			log.WithError(err).Warn("Setgroups")
		}
	}
	log.Infof("========= 3 ==========: %+v", lr.GID)
	if lr.GID != 0 {
		if err := Setgid(lr.GID); err != nil {
			return nil, fmt.Errorf("setgid(%d): %v", lr.GID, err)
		}
	}
	log.Infof("========= 4 ==========: %+v", lr.UID)
	if lr.UID != 0 {
		if err := Setuid(lr.UID); err != nil {
			return nil, fmt.Errorf("setuid(%d)): %v", lr.UID, err)
		}
	}
	if lr.Path != "" {
		err = os.Chdir(lr.Dir)
		if err != nil {
			return nil, fmt.Errorf("chdir to %q: %v", lr.Dir, err)
		}
	}

	// Resolve the binary path.
	for _, env := range lr.Env {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k, v := parts[0], parts[1]
		if k != "PATH" {
			continue
		}
		os.Setenv(k, v)
	}

	cmd := exec.CommandContext(ctx, lr.Argv[0], lr.Argv[1:]...)
	cmd.Env = lr.Env
	return cmd, nil
}

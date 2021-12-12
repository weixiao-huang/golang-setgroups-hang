package main

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	terminal "golang.org/x/term"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// SSHClient implement Client interface for SSH protocol.
type SSHClient struct {
	ctx context.Context
	key *rsa.PrivateKey
}

// NewClient new a client.
func NewSSHClient(ctx context.Context, key *rsa.PrivateKey) *SSHClient {
	return &SSHClient{
		ctx: ctx,
		key: key,
	}
}

type ExecOptions struct {
	isBPMode bool
	predict  bool
	Command  string
	Args     []string
	Envs     []string
	Streams  genericclioptions.IOStreams
}

func (c *SSHClient) Exec(server string, opts *ExecOptions) (int, error) {
	conn, err := c.dial(server)
	if err != nil {
		return -1, err
	}
	defer conn.client.Close()

	if err := conn.sendPreparedRequest(3); err != nil {
		return 0, err
	}
	if err := conn.startSession(opts.Streams); err != nil {
		return 0, fmt.Errorf("start session: %v", err)
	}
	if conn.originalTerminalState != nil {
		defer terminal.Restore(conn.ttyFd, conn.originalTerminalState)
	}
	if err := conn.exec(opts); err != nil {
		return 0, fmt.Errorf("execute: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.session.Wait()
	}()

	select {
	case err := <-errCh:
		switch err := err.(type) {
		case *ssh.ExitMissingError:
			return 0, fmt.Errorf("connection lost")
		case *ssh.ExitError:
			return err.ExitStatus(), nil
		default:
			return 0, err
		}
	case <-c.ctx.Done():
		return 0, fmt.Errorf("cancel context")
	}
}

type SSHConn struct {
	ctx                   context.Context
	client                *ssh.Client
	session               *ssh.Session
	ttyFd                 int
	originalTerminalState *terminal.State
	droppedEnvs           map[string]bool
	handleForwardedOnce   sync.Once
}

func (c *SSHClient) dial(server string) (*SSHConn, error) {
	signer, err := ssh.NewSignerFromKey(c.key)
	if err != nil {
		return nil, err
	}
	publicKey, err := ssh.NewPublicKey(&c.key.PublicKey)
	if err != nil {
		return nil, err
	}

	conn := &SSHConn{
		ctx:         c.ctx,
		droppedEnvs: map[string]bool{},
	}

	startTime := time.Now()
	for time.Now().Before(startTime.Add(time.Minute)) {
		conn.client, err = ssh.Dial("tcp", server, &ssh.ClientConfig{
			User:            "root",
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			Timeout:         500 * time.Millisecond,
			HostKeyCallback: ssh.FixedHostKey(publicKey),
		})
		if err == nil {
			break
		} else {
			log.WithError(err).Debugln("Waiting for server to start")
			time.Sleep(500 * time.Millisecond)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("connect to server: %v", err)
	}
	return conn, nil
}

func (c *SSHConn) sendPreparedRequest(retryLimit int) error {
	for i := 0; ; i++ {
		ok, _, err := c.client.SendRequest("prepared", true, nil)
		if err == nil && ok {
			break
		}
		if i > retryLimit {
			return fmt.Errorf("send prepared request: %v", err)
		}
		log.WithError(err).WithField("result", ok).Warnf("send prepared request: %d/3", i)
	}
	return nil
}

func (c *SSHConn) startSession(ioStreams genericclioptions.IOStreams) error {
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	c.session = session

	session.Stdout = ioStreams.Out
	session.Stderr = ioStreams.ErrOut
	session.Stdin = ioStreams.In

	var (
		ttyEnabled bool
		ttyFd      int
	)
	if session.Stdin != nil {
		if file, ok := session.Stdin.(*os.File); ok {
			ttyFd = int(file.Fd())
			ttyEnabled = terminal.IsTerminal(ttyFd)
		}
	}
	if ttyEnabled {
		modes := ssh.TerminalModes{
			ssh.ECHO:          0,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		c.originalTerminalState, err = terminal.MakeRaw(ttyFd)
		if err != nil {
			return err
		}
		c.ttyFd = ttyFd

		termWidth, termHeight, err := terminal.GetSize(c.ttyFd)
		if err != nil {
			return err
		}

		err = session.RequestPty(os.Getenv("TERM"), termHeight, termWidth, modes)
		if err != nil {
			return err
		}

		sigs := make(chan os.Signal)
		var supportedSignals []os.Signal
		for sig := range SignalName {
			supportedSignals = append(supportedSignals, sig)
		}
		signal.Notify(sigs, supportedSignals...)
		go func() {
			for sig := range sigs {
				log.WithField("signal", sig).Debug("Caught signal")
				err := session.Signal(ssh.Signal(SignalName[sig]))
				if err != nil {
					log.WithError(err).Error("Signal")
				}
			}
		}()

		go func() {
			last := WindowChange{
				Width:       uint32(termWidth),
				Height:      uint32(termHeight),
				WidthPixel:  0,
				HeightPixel: 0,
			}
			for {
				termWidth, termHeight, err := terminal.GetSize(c.ttyFd)
				if err != nil {
					log.WithError(err).Error("Get terminal size")
				} else {
					cur := WindowChange{
						Width:       uint32(termWidth),
						Height:      uint32(termHeight),
						WidthPixel:  0,
						HeightPixel: 0,
					}
					if cur != last {
						session.SendRequest("window-change", false, ssh.Marshal(cur))
						last = cur
					}
				}
				time.Sleep(time.Second)
			}
		}()
	}
	return nil
}

var SignalName = map[os.Signal]string{
	syscall.SIGHUP:  "HUP",
	syscall.SIGINT:  "INT",
	syscall.SIGQUIT: "QUIT",
	syscall.SIGTERM: "TERM",
	syscall.SIGUSR1: "USR1",
	syscall.SIGUSR2: "USR2",
	syscall.SIGTSTP: "TSTP",
}

type WindowChange struct {
	Width       uint32
	Height      uint32
	WidthPixel  uint32
	HeightPixel uint32
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

func (c *SSHConn) exec(options *ExecOptions) error {
	var lr Request
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 && c.droppedEnvs[parts[0]] {
			continue
		}
		lr.Env = append(lr.Env, env)
	}
	lr.Env = append(lr.Env, options.Envs...)
	lr.UID = os.Getuid()
	lr.GID = os.Getgid()
	lr.GIDs, _ = syscall.Getgroups()
	lr.Dir, _ = os.Getwd()
	lr.Path = options.Command
	lr.Argv = append([]string{options.Command}, options.Args...)
	var rlimit syscall.Rlimit
	syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlimit)
	lr.NumFiles = int(rlimit.Cur)

	var buf bytes.Buffer
	b64enc := base64.NewEncoder(base64.StdEncoding, &buf)
	err := gob.NewEncoder(b64enc).Encode(lr)
	b64enc.Close()
	if err != nil {
		return err
	}

	return c.session.Start(buf.String())
}

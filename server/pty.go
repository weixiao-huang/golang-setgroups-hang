package main

import (
	"context"
	"io"
	"syscall"
	"unsafe"

	krpty "github.com/kr/pty"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

type PtyReq struct {
	Term        string
	Width       uint32
	Height      uint32
	WidthPixel  uint32
	HeightPixel uint32
	TermModes   string
}

func (s *session) handlePtyReq(ctx context.Context, req *ssh.Request) bool {
	if s.pty != nil {
		return false
	}

	var ptyReq PtyReq
	err := ssh.Unmarshal(req.Payload, &ptyReq)
	if err != nil {
		log.WithError(err).Error("Unmarshal pty-req")
		return false
	}

	if s.pty, s.tty, err = krpty.Open(); err != nil {
		log.WithError(err).Error("krpty.Open")
		return false
	}

	if err = setWinsize(s.pty.Fd(), ptyReq.Width, ptyReq.Height); err != nil {
		log.WithError(err).Error("Set window initial size")
		return false
	}

	go CopyWithContext(ctx, s.sshChan, s.pty)
	return true
}

// setWinsize sets the size of the given pty.
func setWinsize(fd uintptr, w, h uint32) error {
	type Winsize struct {
		Height uint16
		Width  uint16
		x      uint16 // unused
		y      uint16 // unused
	}
	ws := &Winsize{Width: uint16(w), Height: uint16(h), x: 0, y: 0}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(ws)))
	if errno != 0 {
		return errno
	}
	return nil
}

func CopyWithContext(ctx context.Context, a, b io.ReadWriteCloser) {
	log.Infof("~~~~~~~~~~ 1 ~~~~~~~~~")
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		log.Infof("[copy sshChan to pty] ~~~~~~~~~~ 1.1.1 ~~~~~~~~~")
		io.Copy(a, b)
		cancel()
		log.Infof("[copy sshChan to pty] ~~~~~~~~~~ 1.1.2 ~~~~~~~~~")
	}()
	go func() {
		log.Infof("[copy pty to sshChan] ~~~~~~~~~~ 1.2.1 ~~~~~~~~~")
		io.Copy(b, a)
		cancel()
		log.Infof("[copy pty to sshChan] ~~~~~~~~~~ 1.2.1 ~~~~~~~~~")
	}()
	<-ctx.Done()
	a.Close()
	b.Close()
	log.Infof("~~~~~~~~~~~~ 2 ~~~~~~~~~~~~~")
}

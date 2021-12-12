package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

func handleTCP(conn net.Conn, config *ssh.ServerConfig, stopCh <-chan struct{}) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-stopCh
		cancel()
	}()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		log.WithError(err).Error("Handshake")
		return err
	}
	defer sshConn.Close()

	go func() {
		<-ctx.Done()
		log.Info("Closing connection")
		sshConn.Close()
	}()

	log.WithField("remote", sshConn.RemoteAddr()).Info("Connection established")

	ch := make(chan int, 1)
	go func() {
		for req := range reqs {
			go prepared(req, ch)
		}
	}()
	<-ch // must wait before wg

	for newChan := range chans {
		switch t := newChan.ChannelType(); t {
		case "session":
			go handleSession(ctx, newChan, sshConn)
		default:
			log.WithField("channelType", t).Warn("Unsupported channel type")
			newChan.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
		}
	}
	return nil
}

func prepared(req *ssh.Request, ch chan<- int) {
	if req.WantReply {
		req.Reply(true, nil)
	}
	ch <- 1
}

type session struct {
	sshConn  *ssh.ServerConn
	sshChan  ssh.Channel
	pty, tty *os.File
	cmd      *exec.Cmd
}

func handleSession(ctx context.Context, newChan ssh.NewChannel, sshConn *ssh.ServerConn) {
	ctx, cancel := context.WithCancel(ctx)
	defer log.Println("session quited")
	defer cancel()

	sshChan, reqs, err := newChan.Accept()
	if err != nil {
		log.Errorf("accept session: %v", err)
		return
	}

	s := &session{sshConn: sshConn, sshChan: sshChan}
	s.serveRequests(ctx, reqs)

	if s.tty != nil {
		s.tty.Close()
	}
	if s.pty != nil {
		s.pty.Close()
	}
}

func (s *session) serveRequests(ctx context.Context, reqs <-chan *ssh.Request) {
	for req := range reqs {
		ok := false
		switch req.Type {
		case "exec":
			log.Infof("--------- exec ---------")
			ok = s.handleExec(ctx, req)
		case "pty-req":
			log.Infof("--------- pty-req ---------")
			ok = s.handlePtyReq(ctx, req)
		default:
			log.WithField("requestType", req.Type).Warn("Unsupported session request")
		}
		if req.WantReply {
			req.Reply(ok, nil)
		}
	}
}

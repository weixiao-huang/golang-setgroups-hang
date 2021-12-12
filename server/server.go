package main

import (
	"bytes"
	"crypto/rsa"
	"errors"
	"flag"
	"net"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"k8s.io/apiserver/pkg/server"

	"github.com/weixiao-huang/golang-setgroups-hang/utils"
)

func main() {
	var (
		flagBind          = flag.String("bind", "", "Server bind endpoint")
		flagTimeoutSecond = flag.Int64("timeout-seconds", 60, "The server waits for the client until it times out")
		flagKeyPath       = flag.String("key-path", os.ExpandEnv("${HOME}/.launch/key"), "Location to store the shared secret between server and client")
	)
	flag.Parse()

	var privateKey *rsa.PrivateKey
	if _, err := os.Stat(*flagKeyPath); errors.Is(err, os.ErrNotExist) {
		key, err := utils.GenerateKey(*flagKeyPath)
		if err != nil {
			panic(err)
		}
		privateKey = key
	} else if err != nil {
		panic(err)
	} else {
		privateKey, err = utils.GetPrivateKey(*flagKeyPath)
		if err != nil {
			panic(err)
		}
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		panic(err)
	}
	pubKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		panic(err)
	}
	config := &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-launch",
		PublicKeyCallback: func(conn ssh.ConnMetadata, clientKey ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(pubKey.Marshal(), clientKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("invalid public key")
		},
	}
	config.AddHostKey(signer)
	if err := Run(server.SetupSignalHandler(), *flagBind, *flagTimeoutSecond, config); err != nil {
		panic(err)
	}
}

func Run(stopCh <-chan struct{}, bind string, timeoutSecond int64, config *ssh.ServerConfig) error {
	lis, err := net.Listen("tcp", bind)
	if err != nil {
		return err
	}
	log.Info("Waiting for connection")
	go func() {
		<-time.After(time.Duration(timeoutSecond) * time.Second)
		lis.Close()
	}()
	tcpConn, err := lis.Accept()
	if err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") {
			log.Warn("Timeout, exiting")
		} else {
			log.Errorf("accept: %v", err)
		}
		return err
	}
	lis.Close()
	if err = tcpConn.(*net.TCPConn).SetKeepAlive(true); err != nil {
		log.WithError(err).Error("SetKeepAlive")
		return err
	}
	if err = tcpConn.(*net.TCPConn).SetKeepAlivePeriod(time.Minute); err != nil {
		log.WithError(err).Error("SetKeepAlivePeriod")
		return err
	}
	return handleTCP(tcpConn, config, stopCh)
}

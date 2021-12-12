package main

import (
	"context"
	"crypto/rsa"
	"errors"
	"flag"
	"os"

	log "github.com/sirupsen/logrus"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/weixiao-huang/golang-setgroups-hang/utils"
)

func main() {
	var (
		flagServer  = flag.String("server", "", "")
		flagKeyPath = flag.String("key-path", os.ExpandEnv("${HOME}/.launch/key"), "Location to store the shared secret between server and client")
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

	ctx := context.TODO()
	client := NewSSHClient(ctx, privateKey)
	exitCode, err := client.Exec(*flagServer, &ExecOptions{
		Command: "/bin/bash",
		Streams: genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr},
	})
	log.Infof("exitCode: %+v, err: %+v", exitCode, err)
}

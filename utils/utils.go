package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

func GenerateKey(keyPath string) (*rsa.PrivateKey, error) {
	key, err := GetPrivateKey(keyPath)
	if err == nil {
		return key, nil
	}
	log.Debugln("Generating new key")
	key, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	bytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	log.Debugln("Saving key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0777); err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile(keyPath, bytes, 0644); err != nil {
		return nil, err
	}
	return key, nil
}

func GetPrivateKey(keyPath string) (*rsa.PrivateKey, error) {
	log.WithField("path", keyPath).Debugln("Loading key")
	bytes, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(bytes)
	if block == nil {
		return nil, fmt.Errorf("invalid pem file: %v", keyPath)
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return key, nil
}

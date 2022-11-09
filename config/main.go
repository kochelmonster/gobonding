package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"

	"github.com/kochelmonster/gobonding"
	"gopkg.in/yaml.v2"
)

func generateTLSConfig() ([]byte, []byte) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	return keyPEM, certPEM
}

func main() {
	confName := flag.String("config file", "gobonding.yml", "Configuration file name to generate")
	version := flag.Bool("version", false, "print gobonding version")
	flag.Parse()

	if *version {
		fmt.Println(gobonding.AppVersion)
		os.Exit(0)
	}

	keyPEM, certPEM := generateTLSConfig()
	config := gobonding.Config{
		RouterTunName: "tun0",
		ProxyTunName:  "tun0",
		ProxyIP:       "myproxy.net:41414",
		Channels:      []string{"wan1", "wan2"},
		PrivateKey:    string(keyPEM),
		Certificate:   string(certPEM),
		OrderWindow:   128,
	}

	text, err := yaml.Marshal(config)
	if err != nil {
		log.Panicln("Error marshalling config", err)
	}

	err = os.WriteFile(*confName, text, 0666)
	if err != nil {
		log.Panicln("Error marshalling config", err)
	}
}

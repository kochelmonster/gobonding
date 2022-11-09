package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kochelmonster/gobonding"
	"github.com/lucas-clemente/quic-go"
)

func startDispatcher(ctx context.Context, cm *gobonding.ConnManager, config *gobonding.Config) {
	tlsCert, err := tls.X509KeyPair([]byte(config.Certificate), []byte(config.PrivateKey))
	if err != nil {
		panic(err)
	}
	tlsConf := &tls.Config{
		NextProtos:   []string{"bonding-proxy"},
		Certificates: []tls.Certificate{tlsCert},
		ClientAuth:   tls.RequireAnyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			for _, c1 := range rawCerts {
				for _, c2 := range tlsCert.Certificate {
					if bytes.Equal(c1, c2) {
						log.Println("Certificates Equal")
						return nil
					}
				}
			}
			return errors.New("certificates not equal")
		},
	}

	listener, err := quic.ListenAddr(config.ProxyIP, tlsConf, nil)
	if err != nil {
		panic(err)
	}

	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			log.Println("Error Accept", err)
			return
		}

		go func() {
			stream, err := conn.AcceptStream(ctx)
			if err != nil {
				return
			}

			go func() {
				for {
					chunk := cm.AllocChunk()
					size, err := stream.Read(chunk.Buffer())
					if err != nil {
						cm.FreeChunk(chunk)
						return
					}
					chunk.Decode(uint16(size))
					select {
					case cm.CollectChannel <- chunk:
					case <-ctx.Done():
						stream.Close()
						return
					}
				}
			}()

			go func() {
				for {
					select {
					case chunk := <-cm.DispatchChannel:
						_, err := stream.Write(chunk.ToSend())
						if err != nil {
							cm.DispatchChannel <- chunk
							stream.Close()
							return
						}
					case <-ctx.Done():
						stream.Close()
						return
					}
				}
			}()
		}()
	}
}

func main() {
	confName := flag.String("config file", "gobonding.yml", "Configuration file name")
	version := flag.Bool("version", false, "print gobonding version")
	flag.Parse()

	if *version {
		fmt.Println(gobonding.AppVersion)
		os.Exit(0)
	}

	config, err := gobonding.LoadConfig(*confName)
	if err != nil {
		panic(err)
	}

	iface := gobonding.IfaceSetup(config.ProxyTunName)

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	cm := gobonding.NewConnMananger(ctx, config)

	go startDispatcher(ctx, cm, config)
	go gobonding.WriteToIface(ctx, iface, cm)
	go gobonding.ReadFromIface(ctx, iface, cm)

	exitChan := make(chan os.Signal, 1)
	signal.Notify(exitChan, syscall.SIGTERM)
	signal.Notify(exitChan, syscall.SIGINT)
	<-exitChan
	cancel()
}

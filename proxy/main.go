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
	"sync"
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

	addr := fmt.Sprintf(":%v", config.ProxyPort)
	listener, err := quic.ListenAddr(addr, tlsConf, nil)
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
			log.Println("Stream established", conn.RemoteAddr())

			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				for {
					msg, err := gobonding.ReadMessage(stream, cm)
					log.Println("Receive from", conn.RemoteAddr(), msg)
					if err != nil {
						log.Println("Closing receive stream to", conn.RemoteAddr(), err)
						conn.CloseWithError(1, "stream error")
						return
					}
					msg.Action(ctx, cm)
				}
			}()

			go func() {
				defer wg.Done()
				for {
					select {
					case msg := <-cm.DispatchChannel:
						log.Println("Send to", conn.RemoteAddr(), msg)
						err := msg.Write(stream)
						if err != nil {
							cm.DispatchChannel <- msg
							log.Println("Closing send stream to", conn.RemoteAddr(), err)
							conn.CloseWithError(1, "stream error")
							return
						}
					case <-ctx.Done():
						conn.CloseWithError(2, "close server")
						return
					}
				}
			}()

			cm.ActiveChannels++
			log.Println("Connection established", conn.RemoteAddr(), cm.ActiveChannels)
			wg.Wait()
			cm.ActiveChannels--
			log.Println("Close connection", conn.RemoteAddr(), cm.ActiveChannels)
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

	iface := gobonding.IfaceSetup(config.TunName)

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

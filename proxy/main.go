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
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

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

	udpAddr, err := net.ResolveUDPAddr("udp", config.ProxyIP)
	if err != nil {
		panic(err)
	}
	addr := fmt.Sprintf(":%v", udpAddr.Port)
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
		log.Println("Connection established", conn.RemoteAddr())

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
					chunk := cm.AllocChunk()
					size, err := stream.Read(chunk.Data[0:])
					if err != nil {
						cm.FreeChunk(chunk)
						conn.CloseWithError(1, "stream error")
						return
					}
					if size == 1 {
						// ping message
						cm.FreeChunk(chunk)
						continue
					}
					// log.Println("Received buffer", size, chunk.Data[:size])
					chunk.Decode(uint16(size))
					log.Println("Receive", chunk)
					select {
					case cm.CollectChannel <- chunk:
					case <-ctx.Done():
						conn.CloseWithError(2, "close server")
						return
					}
				}
			}()

			go func() {
				defer wg.Done()
				nothing_send := true
				for {
					select {
					case chunk := <-cm.DispatchChannel:
						nothing_send = false
						log.Println("Send", chunk, len(chunk.ToSend()))
						_, err := stream.Write(chunk.ToSend())
						if err != nil {
							cm.DispatchChannel <- chunk
							conn.CloseWithError(1, "stream error")
							return
						}

					case <-time.After(20 * time.Second):
						if nothing_send {
							_, err := stream.Write([]byte{1})
							if err != nil {
								stream.Close()
								return
							}
						}
						nothing_send = true

					case <-ctx.Done():
						conn.CloseWithError(2, "close server")
						return
					}
				}
			}()
			wg.Wait()
			log.Println("Close connection", conn.RemoteAddr())
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

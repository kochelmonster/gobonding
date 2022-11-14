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

func createChannel(ctx context.Context, channelIdx int, cm *gobonding.ConnManager, config *gobonding.Config) {
	laddr, err := gobonding.ToIP(config.Channels[channelIdx])
	if err != nil {
		panic(err)
	}

	tlsCert, err := tls.X509KeyPair([]byte(config.Certificate), []byte(config.PrivateKey))
	if err != nil {
		panic(err)
	}
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"bonding-proxy"},
		Certificates:       []tls.Certificate{tlsCert},
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

	run := func() {
		raddr, err := net.ResolveUDPAddr("udp", config.ProxyIP)
		if err != nil {
			log.Println("Cannot Resolve address", config.ProxyIP, err)
			return
		}

		udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: laddr, Port: 0})
		if err != nil {
			panic(err)
		}

		conn, err := quic.DialContext(ctx, udpConn, raddr, config.ProxyIP, tlsConf, nil)
		if err != nil {
			return
		}

		stream, err := conn.OpenStreamSync(ctx)
		if err != nil {
			return
		}

		_, err = stream.Write([]byte{1})
		if err != nil {
			stream.Close()
			return
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			for {
				select {
				case chunk := <-cm.DispatchChannel:
					log.Println("Send", chunk)
					_, err := stream.Write(chunk.ToSend())
					if err != nil {
						// send chunk via other ProxyChannels
						cm.DispatchChannel <- chunk
						return
					} else {
						cm.FreeChunk(chunk)
					}

				case <-ctx.Done():
					stream.Close()
					return
				}
			}
		}()

		go func() {
			defer wg.Done()

			for {
				chunk := cm.AllocChunk()
				size, err := stream.Read(chunk.Data[0:])
				log.Println("Prereceive", size, err)
				if err != nil {
					cm.FreeChunk(chunk)
					stream.Close()
					return
				}
				if size == 1 {
					// ping message
					cm.FreeChunk(chunk)
					continue
				}
				chunk.Decode(uint16(size))
				log.Println("Receive", chunk)
				select {
				case cm.CollectChannel <- chunk:
				case <-ctx.Done():
					cm.FreeChunk(chunk)
					return
				}
			}
		}()

		wg.Wait()
	}

	for {
		run()
		time.Sleep(20 * time.Second)
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

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	iface := gobonding.IfaceSetup(config.RouterTunName)

	// start routes changes in config monitoring
	log.Println("Interface parameters configured", iface)

	cm := gobonding.NewConnMananger(ctx, config)

	for i := range config.Channels {
		go createChannel(ctx, i, cm, config)
	}

	go gobonding.WriteToIface(ctx, iface, cm)
	go gobonding.ReadFromIface(ctx, iface, cm)

	exitChan := make(chan os.Signal, 1)
	signal.Notify(exitChan, syscall.SIGTERM)
	signal.Notify(exitChan, syscall.SIGINT)

	<-exitChan
	cancel()
}

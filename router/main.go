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

func createChannel(ctx context.Context, link, proxy string, cm *gobonding.ConnManager, config *gobonding.Config) {
	laddr, err := gobonding.ToIP(link)
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
		ctx, cancel := context.WithCancel(ctx)

		qConf := &quic.Config{
			MaxIdleTimeout:  30 * time.Second,
			KeepAlivePeriod: 30 * time.Second}

		raddr, err := net.ResolveIPAddr("udp", proxy)
		if err != nil {
			log.Println("Cannot Resolve address", proxy, err)
			cancel()
			return
		}

		udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: laddr, Port: 0})
		if err != nil {
			panic(err)
		}

		conn, err := quic.DialContext(ctx, udpConn, raddr, proxy, tlsConf, qConf)
		if err != nil {
			cancel()
			return
		}

		stream, err := conn.OpenStreamSync(ctx)
		if err != nil {
			cancel()
			return
		}

		if cm.ActiveChannels == 0 {
			cm.SyncCounter()
		}
		cm.ActiveChannels++
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			for {
				select {
				case msg := <-cm.DispatchChannel:
					log.Println("Send", msg)
					err := msg.Write(stream)
					if err != nil {
						// send chunk via other ProxyChannels
						cm.DispatchChannel <- msg
						log.Println("Close write stream", err)
						stream.Close()
						cancel()
						return
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
				message, err := gobonding.ReadMessage(stream, cm)
				log.Println("Receive", message)
				if err != nil {
					log.Println("Close read stream", err)
					stream.Close()
					cancel()
					return
				}
				message.Action(ctx, cm)
			}
		}()

		wg.Wait()
		cm.ActiveChannels--
	}

	reconnectTime, err := time.ParseDuration(config.ReconnectTime)
	if err != nil {
		reconnectTime = 20 * time.Second
	}
	for {
		run()
		log.Println("Reconnect")
		select {
		case <-time.After(reconnectTime):
		case <-ctx.Done():
			return
		}
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

	iface := gobonding.IfaceSetup(config.TunName)

	// start routes changes in config monitoring
	log.Println("Interface parameters configured", iface)

	cm := gobonding.NewConnMananger(ctx, config)

	for link, proxy := range config.Channels {
		go createChannel(ctx, link, proxy, cm, config)
	}

	go gobonding.WriteToIface(ctx, iface, cm)
	go gobonding.ReadFromIface(ctx, iface, cm)

	exitChan := make(chan os.Signal, 1)
	signal.Notify(exitChan, syscall.SIGTERM)
	signal.Notify(exitChan, syscall.SIGINT)

	<-exitChan
	cancel()
}

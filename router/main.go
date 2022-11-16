package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
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

	serverAddr := fmt.Sprintf("%v:%v", proxy, config.ProxyPort)
	raddr, err := net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		log.Println("Cannot Resolve address", serverAddr, err)
		return
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: laddr, Port: 0})
	if err != nil {
		panic(err)
	}

	tlsConf := gobonding.CreateTlsConf(config)
	run := func() {
		qConf := &quic.Config{
			HandshakeIdleTimeout: 30 * time.Second,
			MaxIdleTimeout:       30 * time.Second,
			KeepAlivePeriod:      30 * time.Second}

		log.Printf("Dialing %v(%v) -> %v\n", laddr, link, serverAddr)
		conn, err := quic.DialContext(ctx, udpConn, raddr, serverAddr, tlsConf, qConf)
		if err != nil {
			log.Printf("Error Dialing %v(%v) -> %v: %v\n", laddr, link, serverAddr, err)
			return
		}

		stream, err := conn.OpenStreamSync(ctx)
		if err != nil {
			return
		}

		if cm.ActiveChannels == 0 {
			cm.SyncCounter()
		}
		gobonding.HandleStream(conn, stream, cm)
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

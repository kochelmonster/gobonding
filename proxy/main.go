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
)

func startDispatcher(ctx context.Context, cm *gobonding.ConnManager, config *gobonding.Config) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: nil, Port: config.ProxyPort})
	if err != nil {
		panic(err)
	}
	log.Println("UDP Server started", conn.LocalAddr())

	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	for {
		msg, err := gobonding.ReadMessage(conn, cm)
		if err != nil {
			return
		}
		// log.Println("Received", msg)
		msg.Action(cm, conn)
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
	cm := gobonding.NewConnMananger(config).Start(ctx)

	go startDispatcher(ctx, cm, config)
	go gobonding.WriteToIface(ctx, iface, cm)
	go gobonding.ReadFromIface(ctx, iface, cm)

	exitChan := make(chan os.Signal, 1)
	signal.Notify(exitChan, syscall.SIGTERM)
	signal.Notify(exitChan, syscall.SIGINT)
	<-exitChan
	cancel()
	time.Sleep(1 * time.Microsecond)
	cm.Close()
}

package main

import (
	"context"
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
	tlsConf := gobonding.CreateTlsConf(config)

	addr := fmt.Sprintf(":%v", config.ProxyPort)
	listener, err := quic.ListenAddr(addr, tlsConf, nil)
	if err != nil {
		panic(err)
	}
	log.Println("Quic Server started", addr)
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
			gobonding.HandleStream(conn, stream, cm)
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

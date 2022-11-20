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

	"github.com/kochelmonster/gobonding"
)

func startDispatcher(ctx context.Context, cm *gobonding.ConnManager, config *gobonding.Config) {
	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%v", config.ProxyPort))
	if err != nil {
		panic(err)
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		panic(err)
	}

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	log.Println("TCP Server started", addr)
	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			log.Println("Error listener", err)
			return
		}

		go func() {
			gobonding.HandleConnection(ctx, conn, cm)
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

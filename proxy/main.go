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

type ProxyIO struct {
	conn *net.UDPConn
	addr net.Addr
}

func (io *ProxyIO) Write(buffer []byte) (int, error) {
	if io.addr != nil {
		_, err := io.conn.WriteTo(buffer, io.addr)
		if err != nil {
			log.Println("error sending", err, io.conn)
		}
	}
	return len(buffer), nil
}

func (io *ProxyIO) Read(buffer []byte) (int, error) {
	size, addr, err := io.conn.ReadFrom(buffer)
	io.addr = addr
	return size, err
}

func (io *ProxyIO) Close() error {
	return io.conn.Close()
}

func createChannels(cm *gobonding.ConnManager) {
	for i := uint16(0); i < uint16(len(cm.Config.Channels)); i++ {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{
			IP: nil, Port: cm.Config.ProxyStartPort + int(i)})
		if err != nil {
			panic(err)
		}
		conn.SetReadBuffer(gobonding.SOCKET_BUFFER)
		conn.SetWriteBuffer(0)
		log.Println("UDP Server started", conn.LocalAddr())
		gobonding.NewChannel(cm, i, &ProxyIO{conn, nil}).Start(true)
	}
	// go cm.SendPings(ctx, &conn)
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
	cm := gobonding.NewConnMananger(ctx, config).Start()

	createChannels(cm)
	go cm.Receiver(iface)
	go cm.Sender(iface)

	exitChan := make(chan os.Signal, 1)
	signal.Notify(exitChan, syscall.SIGTERM)
	signal.Notify(exitChan, syscall.SIGINT)
	<-exitChan
	cancel()
	time.Sleep(1 * time.Microsecond)
	cm.Close()
}

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

type RouterIO struct {
	conn *net.UDPConn
}

func (io *RouterIO) Write(buffer []byte) {
	_, err := io.conn.Write(buffer)
	if err != nil {
		log.Println("error sending", err, io.conn)
	}
}

func (io *RouterIO) Read(buffer []byte) (int, error) {
	return io.conn.Read(buffer)
}

func (io *RouterIO) Close() {
	io.conn.Close()
}

func createChannels(cm *gobonding.ConnManager) {
	i := uint16(0)
	for link, proxy := range cm.Config.Channels {
		laddr, err := gobonding.ToIP(link)
		if err != nil {
			panic(err)
		}

		serverAddr := fmt.Sprintf("%v:%v", proxy, cm.Config.ProxyStartPort+int(i))
		raddr, err := net.ResolveUDPAddr("udp", serverAddr)
		if err != nil {
			log.Println("Cannot Resolve address", serverAddr, err)
			return
		}

		udpConn, err := net.DialUDP("udp", &net.UDPAddr{IP: laddr, Port: 0}, raddr)
		if err != nil {
			panic(err)
		}

		gobonding.NewChannel(cm, i, &RouterIO{udpConn}).Ping().Ping().Start()
		i++
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

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

type ConnectionDispatcher struct {
	conns map[string]*net.UDPConn
}

func (c *ConnectionDispatcher) WriteTo(b []byte, addr net.Addr) (int, error) {
	if conn, ok := c.conns[addr.String()]; ok {
		return conn.Write(b)
	}
	return 0, nil
}

type ReadWrapper struct {
	conn *net.UDPConn
}

func (c *ReadWrapper) ReadFrom(b []byte) (int, net.Addr, error) {
	size, _, err := c.conn.ReadFrom(b)
	return size, c.conn.LocalAddr(), err
}

func createChannels(ctx context.Context, cm *gobonding.ConnManager, config *gobonding.Config) {
	conn := ConnectionDispatcher{conns: map[string]*net.UDPConn{}}

	i := uint16(0)
	for link, proxy := range config.Channels {
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

		udpConn, err := net.DialUDP("udp", &net.UDPAddr{IP: laddr, Port: 0}, raddr)
		if err != nil {
			panic(err)
		}
		addr := udpConn.LocalAddr()
		conn.conns[addr.String()] = udpConn
		cm.AddChannel(i, addr, &conn)
		i++

		go func() {
			log.Println("Initial PingPong")
			cm.PingPong(addr, &conn)
			log.Println("Initial PingPong done")
		}()

		go func() {
			wrapper := ReadWrapper{conn: udpConn}
			for {
				msg, err := gobonding.ReadMessage(&wrapper, cm)
				if err != nil {
					return
				}
				//log.Println("Received", addr, msg)
				msg.Action(cm, &conn)
			}
		}()
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

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	iface := gobonding.IfaceSetup(config.TunName)

	// start routes changes in config monitoring
	log.Println("Interface parameters configured", iface)

	cm := gobonding.NewConnMananger(ctx, config)

	createChannels(ctx, cm, config)
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

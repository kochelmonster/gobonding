package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/songgao/water"
	"golang.org/x/net/ipv4"
	"gopkg.in/yaml.v2"
)

const (
	// AppVersion contains current application version for -version command flag
	AppVersion = "0.1.0a"
)

const (
	// I use TUN interface, so only plain IP packet,
	// no ethernet header + mtu is set to 1300

	// BUFFERSIZE is size of buffer to receive packets
	// (little bit bigger than maximum)
	BUFFERSIZE = 1518
)

type Config struct {
	TunName  string
	LocalIp  string
	ProxyIP  string
	Channels []string
}

/*
func rcvrThread(config *Config, iface *water.Interface) {
	for {
		n, _, err := conn.ReadFrom(encrypted)
		if err != nil {
			log.Println("Error: ", err)
			continue
		}

		// ReadFromUDP can return 0 bytes on timeout
		if 0 == n {
			continue
		}

		conf := config.Load().(VPNState)

		if !conf.Main.main.CheckSize(n) {
			log.Println("invalid packet size ", n)
			continue
		}

		n, err = iface.Write(decrypted[:size])
		if nil != err {
			log.Println("Error writing to local interface: ", err)
		} else if n != size {
			log.Println("Partial package written to local interface")
		}
	}
}*/

func sndrThread(config *Config, iface *water.Interface) {
	// first time fill with random numbers
	var packet = make([]byte, BUFFERSIZE)

	for {
		_, err := iface.Read(packet[:MTU])
		if err != nil {
			break
		}
		header, err := ipv4.ParseHeader(packet)
		if err != nil {
			log.Println("Error parsing package", err)
		}
		log.Println("IPv4 packet", header)
		continue

	}
}

func main() {
	confName := flag.String("config file", "gobonding.yml", "Configuration file name")
	version := flag.Bool("version", false, "print lcvpn version")
	flag.Parse()

	if *version {
		fmt.Println(AppVersion)
		os.Exit(0)
	}

	config := Config{}
	data, err := os.ReadFile(*confName)
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		panic(err)
	}

	iface, localCIDR := ifaceSetup(config.LocalIp)
	addRoutes(localCIDR)
	defer ifaceShutdown()

	// start routes changes in config monitoring
	log.Println("Interface parameters configured", iface)

	// Start listen threads
	// go rcvrThread(&config, iface)

	// Start sender threads
	go sndrThread(&config, iface)

	exitChan := make(chan os.Signal, 1)
	signal.Notify(exitChan, syscall.SIGTERM)
	signal.Notify(exitChan, syscall.SIGINT)

	<-exitChan

	/*err = writeConn.Close()
	if nil != err {
		log.Println("Error closing UDP connection: ", err)
	}*/
}

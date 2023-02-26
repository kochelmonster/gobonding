package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/kochelmonster/gobonding"
	"gopkg.in/yaml.v2"
)

const (
	ROUTER_SETUP = `set -e
if [ ! -f /tmp/org-routing ]; then
	ip route save table all > /tmp/org-routing
fi
sysctl -w net.ipv4.ip_forward=1
sysctl -w net.ipv6.conf.all.forwarding=1
sysctl -w net.core.rmem_max=2500000

[TABLES]

ip tuntap add mode tun dev tun0
ip addr add 10.0.0.2/30 dev tun0
ip link set dev tun0 up

ip route flush 0/0
ip route add default via 10.0.0.1 dev tun0

[WAN-RULES]
[WAN-ROUTES]
`
	CREATE_TABLE = "grep -q 'wan%[1]v' '/etc/iproute2/rt_tables' || echo '%[2]v wan%[1]v' >> /etc/iproute2/rt_tables\n"
	WANRULE      = "ip rule add from %[1]v table wan%[2]v\n"
	WANROUTE     = "ip route add default via %[1]v table wan%[2]v\n"
)

var COMMENTS = [...]string{
	"# Name of the TUN adapter\ntunname",
	"\n# If not empty the path to a monitor file\nmonitorpath",
	"\n# The Tick interval to update the monitor file\nmonitortick",
	"\n# The delay a channel tries to reconnect to the proxy after a connection close\nreconnecttime",
	"\n# The Port the proxy is listening to\nproxyport",
	"\n# A map of wan device to ip address of proxy server\nchannels",
	"\n# Public key for authentication\npublickey",
	"\n# Private key for authentication\nprivatekey",
	"\n# Name of the distribution balancer, possible: prio, relative\nbalancer",
	"\n# Start of port range to proxy\nproxystartport",
}

func main() {
	version := flag.Bool("version", false, "print gobonding version")
	flag.Usage = func() {
		fmt.Printf("Usage: config PROXYIP DEVORIP+\n")
		fmt.Printf("where  PROXYIP is the IP Address of the Proxy Server\n")
		fmt.Printf("       DEVORIP is the IP Address or the Device Name to a wan interface\n\n")
		fmt.Printf("Generates the configuration files and startup code for router and proxy sites\n")
	}
	flag.Parse()
	if *version {
		fmt.Println(gobonding.AppVersion)
		os.Exit(0)
	}

	proxy := flag.Arg(0)

	if proxy == "" {
		flag.Usage()
		os.Exit(0)
	}

	if net.ParseIP(proxy) == nil {
		panic(fmt.Sprintf("PROXY %v is not a valid ip address", proxy))
	}

	devs := map[string]string{}
	i := 1
	for {
		device := flag.Arg(i)
		if device == "" {
			break
		}
		ip, err := gobonding.ToIP(device)
		if err != nil {
			panic(err)
		}
		devs[device] = ip.String()
		i++
	}

	fname, err := filepath.Abs(os.Args[0])
	if err != nil {
		panic(err)
	}
	parent := filepath.Dir(filepath.Dir(fname))
	WriteConfigFile(parent, proxy, devs)
	WriteRouterSetup(parent, devs)
	fmt.Println("Config generated")
}

func WriteConfigFile(parentDir, proxy string, devs map[string]string) {
	channels := make(map[string]string)
	for dev := range devs {
		channels[dev] = proxy
	}

	privatePEM, publicPEM := generateKeys()
	config := gobonding.Config{
		TunName:        "tun0",
		MonitorPath:    "/var/lib/gobonding/monitor.yml",
		MonitorTick:    "5s",
		ProxyStartPort: 41414,
		Balancer:       "relative",
		Channels:       channels,
		PrivateKey:     string(privatePEM),
		PublicKey:      string(publicPEM),
	}

	for _, d := range []string{"router", "proxy"} {
		if d == "proxy" {
			config.PrivateKey = ""
		}

		text, err := yaml.Marshal(config)
		if err != nil {
			log.Panicln("Error marshalling config", err)
		}

		ctext := string(text)
		for _, c := range COMMENTS {
			lines := strings.Split(c, "\n")
			key := lines[len(lines)-1]
			ctext = strings.Replace(ctext, key, c, -1)
		}

		WriteText(parentDir, d, gobonding.CONFFILE, ctext)
	}
}

func WriteRouterSetup(parentDir string, devs map[string]string) {
	devToTable := make(map[string]int)
	tables := ""

	i := 0
	for dev := range devs {
		devToTable[dev] = i
		tables += fmt.Sprintf(CREATE_TABLE, i, 100+i)
		i++
	}
	wrules := ""
	for dev, i := range devToTable {
		wrules += fmt.Sprintf(WANRULE, dev, i)
	}

	wroutes := ""
	for dev, i := range devToTable {
		gw := replaceLast(devs[dev], "1")
		wroutes += fmt.Sprintf(WANROUTE, gw, i)
	}

	setup := strings.Replace(ROUTER_SETUP, "[TABLES]", tables, -1)
	setup = strings.Replace(setup, "[WAN-RULES]", wrules, -1)
	setup = strings.Replace(setup, "[WAN-ROUTES]", wroutes, -1)
	WriteText(parentDir, "router", "router-setup.sh", setup)
}

func replaceLast(ip, subst string) string {
	parts := strings.Split(ip, ".")
	parts[len(parts)-1] = subst
	return strings.Join(parts, ".")
}

func WriteText(parent, directory, name, text string) {
	path := filepath.Join(parent, directory, name)
	err := os.WriteFile(path, []byte(text), 0666)
	if err != nil {
		log.Panicln("Error writing", path, err)
	}
}

func generateKeys() ([]byte, []byte) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Panicln("Error creating RSA key", err)
	}
	privatePEM := pem.EncodeToMemory(
		&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	pubASN1, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		log.Panicln("Error marshaling public key", err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: pubASN1})

	return privatePEM, publicPEM
}

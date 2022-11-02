//go:build linux
// +build linux

package main

import (
	"log"
	"net"

	"github.com/songgao/water"
	"github.com/vishvananda/netlink"
)

const (
	// MTU used for tunneled packets
	MTU = 1300
)

var oldGateways = make([]netlink.Route, 0, 10)
var newGateway netlink.Route

// ifaceSetup returns new interface OR PANIC!
func ifaceSetup(localCIDR string) (*water.Interface, string) {
	if localCIDR == "" {
		localCIDR = "10.10.1.1/24"
	}

	addr, err := netlink.ParseAddr(localCIDR)
	if nil != err {
		log.Fatalln("\nlocal ip is not in ip/cidr format")
		panic("invalid local ip")
	}

	iface, err := water.New(water.Config{DeviceType: water.TUN})
	if nil != err {
		log.Println("Unable to allocate TUN interface:", err)
		panic(err)
	}
	log.Println("Interface allocated:", iface.Name())

	link, err := netlink.LinkByName(iface.Name())
	if nil != err {
		log.Fatalln("Unable to get interface info", err)
	}

	err = netlink.LinkSetMTU(link, MTU)
	if nil != err {
		log.Fatalln("Unable to set MTU to 1300 on interface")
	}

	err = netlink.AddrAdd(link, addr)
	if nil != err {
		log.Fatalln("Unable to set IP to ", addr, " on interface")
	}

	err = netlink.LinkSetUp(link)
	if nil != err {
		log.Fatalln("Unable to UP interface")
	}

	return iface, localCIDR
}

func addRoutes(localCIDR string) {
	routes, _ := netlink.RouteList(nil, netlink.FAMILY_V4)
	for _, r := range routes {
		if r.Dst == nil {
			if err := netlink.RouteDel(&r); err != nil {
				log.Fatalln("Could not remove gateway", r, "failed:", err)
			}
			oldGateways = append(oldGateways, r)
		}
	}

	ip, _, err := net.ParseCIDR(localCIDR)
	newGateway = netlink.Route{
		Scope: netlink.SCOPE_UNIVERSE,
		Dst:   nil,
		Gw:    ip,
	}
	if nil != err {
		log.Fatalln("Error parsing cidr", localCIDR, "failed:", err)
	}

	err = netlink.RouteAdd(&newGateway)
	if nil != err {
		log.Fatalln("Adding gateway", newGateway, "failed:", err)
	}
}

func ifaceShutdown() {
	if err := netlink.RouteDel(&newGateway); err != nil {
		log.Fatalln("Could not remove router gateway", newGateway, "failed:", err)
	}

	for _, r := range oldGateways {
		err := netlink.RouteAdd(&r)
		if nil != err {
			log.Fatalln("Adding gateway", r, "failed:", err)
		}
	}
	log.Println("Replaced to old gateway")
}

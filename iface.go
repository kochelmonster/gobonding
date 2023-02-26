//go:build linux
// +build linux

package gobonding

import (
	"log"

	"github.com/songgao/water"
	"github.com/vishvananda/netlink"
)

// ifaceSetup returns new interface OR PANIC!
func IfaceSetup(name string) *water.Interface {
	config := water.Config{DeviceType: water.TUN}
	config.Name = name
	config.Persist = true
	iface, err := water.New(config)
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

	return iface
}

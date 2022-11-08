//go:build linux
// +build linux

package gobonding

import (
	"context"
	"io"
	"log"

	"github.com/songgao/water"
	"github.com/vishvananda/netlink"
)

// ifaceSetup returns new interface OR PANIC!
func IfaceSetup(localCIDR string) *water.Interface {
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

	return iface
}

func ReadFromIface(ctx context.Context, iface *water.Interface, cm *ConnManager) {
	var counter uint16 = 0
	for {
		chunk := cm.AllocChunk()
		size, err := iface.Read(chunk.Buffer())
		switch err {
		case io.EOF:
			return
		case nil:
		default:
			log.Println("Error reading packet", err)
		}

		chunk.Size = uint16(size)
		chunk.Idx = counter
		counter++

		select {
		case cm.DispatchChannel <- chunk:
		case <-ctx.Done():
			cm.FreeChunk(chunk)
			return
		}
	}
}

func WriteToIface(ctx context.Context, iface *water.Interface, cm *ConnManager) {
	for {
		select {
		case chunk := <-cm.OrderedChannel:
			_, err := iface.Write(chunk.Buffer()[:chunk.Size])
			if err != nil {
				log.Println("Error writing packet", err)
			}
			cm.FreeChunk(chunk)

		case <-ctx.Done():
			return
		}
	}
}

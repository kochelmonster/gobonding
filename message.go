package gobonding

import (
	"fmt"
	"log"
	"net"

	"golang.org/x/net/ipv4"
)

const (
	MTU = 1300

	// BSIZE is size of buffer to receive packets
	// (little bit bigger than maximum)
	BUFFERSIZE = 1518
)

type WriteConnection interface {
	WriteTo(b []byte, addr net.Addr) (int, error)
}

type ReadConnection interface {
	ReadFrom(b []byte) (int, net.Addr, error)
}

type Message interface {
	Write(conn WriteConnection) error
	Action(cm *ConnManager, conn WriteConnection)
	RouterAddr() net.Addr
	SetRouterAddr(addr net.Addr)
	String() string
}

type Chunk struct {
	Message
	Data [BUFFERSIZE]byte
	Size uint16
	Addr net.Addr
}

func (c *Chunk) SetRouterAddr(addr net.Addr) {
	c.Addr = addr
}

func (c *Chunk) RouterAddr() net.Addr {
	return c.Addr
}

func (c *Chunk) Write(conn WriteConnection) error {
	// log.Println("Send", c.Addr)
	_, err := conn.WriteTo(c.Data[:c.Size], c.Addr)
	return err
}

func (c *Chunk) Action(cm *ConnManager, conn WriteConnection) {
	cm.CollectChannel <- c
}

func (c *Chunk) String() string {
	header, err := ipv4.ParseHeader(c.Data[0:])
	if err != nil {
		return "Error parsing buffer"
	} else {
		return fmt.Sprintf("Packet %v: %v", c.Size, header)
	}
}

type PingMessage struct {
	Addr net.Addr
}

func (m *PingMessage) Write(conn WriteConnection) error {
	buffer := []byte{0, 'p'}
	_, err := conn.WriteTo(buffer, m.Addr)
	return err
}

func (m *PingMessage) Action(cm *ConnManager, conn WriteConnection) {
	cm.UpdateChannel(m.Addr, conn)
}

func (m *PingMessage) SetRouterAddr(addr net.Addr) {
	m.Addr = addr
}

func (m *PingMessage) RouterAddr() net.Addr {
	return m.Addr
}

func (m *PingMessage) String() string {
	return fmt.Sprintf("PingMessage: %v", m.Addr)
}

func ReadMessage(conn ReadConnection, cm *ConnManager) (Message, error) {
	chunk := cm.AllocChunk()
	size, addr, err := conn.ReadFrom(chunk.Data[0:])
	if err != nil {
		cm.FreeChunk(chunk)
		return nil, err
	}
	chunk.Size = uint16(size)
	chunk.Addr = addr

	if chunk.Data[0] == 0 {
		switch chunk.Data[1] {
		case 'p':
			cm.FreeChunk(chunk)
			return &PingMessage{Addr: addr}, nil
		}
		// not an IP Packet -> Control Message
		log.Println("control message")
	}

	return chunk, nil
}

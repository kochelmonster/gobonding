package gobonding

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"

	"golang.org/x/net/ipv4"
)

const (
	MTU = 1500

	// BSIZE is size of buffer to receive packets
	// (little bit bigger than maximum)
	BUFFERSIZE = 1718
)

type WriteConnection interface {
	WriteTo(b []byte, addr net.Addr) (int, error)
}

type ReadConnection interface {
	ReadFrom(b []byte) (int, net.Addr, error)
}

type Message interface {
	Write(conn WriteConnection) int
	Action(cm *ConnManager, conn WriteConnection)
	RouterAddr() net.Addr
	SetRouterAddr(addr net.Addr)
	String() string
}

type MsgBase struct {
	Addr net.Addr
}

func (m *MsgBase) SetRouterAddr(addr net.Addr) {
	m.Addr = addr
}

func (m *MsgBase) RouterAddr() net.Addr {
	return m.Addr
}

type Chunk struct {
	MsgBase
	Data [BUFFERSIZE]byte
	Size uint16
}

func (c *Chunk) Write(conn WriteConnection) int {
	// log.Println("Send", c.Addr)
	size, _ := conn.WriteTo(c.Data[:c.Size], c.Addr)
	return size
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

type PongMsg struct {
	MsgBase
	Id uint16
}

func (m *PongMsg) Write(conn WriteConnection) int {
	buffer := []byte{0, 'o', 0, 0}
	binary.BigEndian.PutUint16(buffer[2:], m.Id)
	conn.WriteTo(buffer, m.Addr)
	return 0
}

func (m *PongMsg) Action(cm *ConnManager, conn WriteConnection) {
	cm.GotPong(m.Addr)
}

func (m *PongMsg) String() string {
	return fmt.Sprintf("Pong: %v %v", m.Id, m.Addr)
}

type PingMsg struct {
	MsgBase
	Id uint16
}

func (m *PingMsg) Write(conn WriteConnection) int {
	buffer := []byte{0, 'i', 0, 0}
	binary.BigEndian.PutUint16(buffer[2:], m.Id)
	conn.WriteTo(buffer, m.Addr)
	return 0
}

func (m *PingMsg) Action(cm *ConnManager, conn WriteConnection) {
	cm.SendPong(m.Id, m.Addr, conn)
}

func (m *PingMsg) String() string {
	return fmt.Sprintf("Ping: %v %v", m.Id, m.Addr)
}

type WeightMsg struct {
	MsgBase
	Weight uint16
}

func (m *WeightMsg) Write(conn WriteConnection) int {
	buffer := []byte{0, 'w', 0, 0}
	binary.BigEndian.PutUint16(buffer[2:], m.Weight)
	conn.WriteTo(buffer, m.Addr)
	return 0
}

func (m *WeightMsg) Action(cm *ConnManager, conn WriteConnection) {
	key := toKey(m.Addr)
	if c, ok := cm.Channels[key]; ok {
		log.Println("set Weight", m.Addr, m.Weight)
		c.sendWeight = m.Weight
	}
}

func (m *WeightMsg) String() string {
	return fmt.Sprintf("Weight: %v %v", m.Weight, m.Addr)
}

func ReadMessage(conn ReadConnection, cm *ConnManager) (Message, error) {
	chunk := cm.AllocChunk()
	size, addr, err := conn.ReadFrom(chunk.Data[0:])
	if err != nil {
		cm.FreeChunk(chunk)
		return nil, err
	}
	chunk.Addr = addr

	if chunk.Data[0] == 0 { // the ip header first byte
		id := binary.BigEndian.Uint16(chunk.Data[2:4])
		cm.FreeChunk(chunk)

		switch chunk.Data[1] {
		case 'o':
			return &PongMsg{
				MsgBase: MsgBase{Addr: addr},
				Id:      id,
			}, nil
		case 'i':
			return &PingMsg{
				MsgBase: MsgBase{Addr: addr},
				Id:      id,
			}, nil
		case 'w':
			return &WeightMsg{
				MsgBase: MsgBase{Addr: addr},
				Weight:  id,
			}, nil
		}
		// not an IP Packet -> Control Message
		log.Println("control message")
	}

	chunk.Size = uint16(size)
	cm.ReceivedChunk(addr, chunk)
	return chunk, nil
}

package gobonding

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"time"

	"golang.org/x/net/ipv4"
)

const (
	MTU = 1450

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
	Write(cm *ConnManager, conn WriteConnection) int
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
	Data [BUFFERSIZE + 2]byte
	Size uint16
	Idx  uint16
}

func (c *Chunk) Write(cm *ConnManager, conn WriteConnection) int {
	// log.Println("Send", c.Addr)
	binary.BigEndian.PutUint16(c.Data[:2], c.Idx)
	size, _ := conn.WriteTo(c.Data[:c.Size+2], c.Addr)
	cm.FreeChunk(c)
	return size
}

func (c *Chunk) Action(cm *ConnManager, conn WriteConnection) {
	d := time.Since(cm.rtick)
	if d < cm.minRtick {
		cm.minRtick = d
	}
	if d > cm.maxRtick {
		cm.maxRtick = d
	}

	cm.CollectChannel <- c
}

func (c *Chunk) String() string {
	header, err := ipv4.ParseHeader(c.Data[2:])
	if err != nil {
		return "Error parsing buffer"
	} else {
		return fmt.Sprintf("Packet %v(%v): %v", c.Idx, c.Size, header)
	}
}

type PongMsg struct {
	MsgBase
	Id uint16
}

func (m *PongMsg) Write(cm *ConnManager, conn WriteConnection) int {
	buffer := []byte{0, 0, 0, 'o', 0, 0}
	binary.BigEndian.PutUint16(buffer[4:], m.Id)
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

func (m *PingMsg) Write(cm *ConnManager, conn WriteConnection) int {
	buffer := []byte{0, 0, 0, 'i', 0, 0}
	binary.BigEndian.PutUint16(buffer[4:], m.Id)
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

func (m *WeightMsg) Write(cm *ConnManager, conn WriteConnection) int {
	buffer := []byte{0, 0, 0, 'w', 0, 0}
	binary.BigEndian.PutUint16(buffer[4:], m.Weight)
	conn.WriteTo(buffer, m.Addr)
	return 0
}

func (m *WeightMsg) Action(cm *ConnManager, conn WriteConnection) {
	cm.changeWeights(m.Addr, int(m.Weight))
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

	if chunk.Data[2] == 0 { // the ip header first byte
		id := binary.BigEndian.Uint16(chunk.Data[4:6])
		cm.FreeChunk(chunk)

		switch chunk.Data[3] {
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

	chunk.Size = uint16(size) - 2
	chunk.Idx = binary.BigEndian.Uint16(chunk.Data[:2])
	cm.ReceivedChunk(addr, chunk)
	return chunk, nil
}

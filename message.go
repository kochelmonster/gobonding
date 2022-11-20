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

type RequestAckMsg struct {
	MsgBase
}

func (m *RequestAckMsg) Write(conn WriteConnection) error {
	buffer := []byte{0, 'r'}
	_, err := conn.WriteTo(buffer, m.Addr)
	return err
}

func (m *RequestAckMsg) Action(cm *ConnManager, conn WriteConnection) {
	cm.SendAck(m.Addr, conn)
}

func (m *RequestAckMsg) String() string {
	return fmt.Sprintf("RequestAck: %v", m.Addr)
}

type AckMessage struct {
	MsgBase
}

func (m *AckMessage) Write(conn WriteConnection) error {
	buffer := []byte{0, 'a'}
	_, err := conn.WriteTo(buffer, m.Addr)
	return err
}

func (m *AckMessage) Action(cm *ConnManager, conn WriteConnection) {
	cm.GotAck(m.Addr)
}

func (m *AckMessage) String() string {
	return fmt.Sprintf("AckMessage: %v", m.Addr)
}

type PingMessage struct {
	MsgBase
}

func (m *PingMessage) Write(conn WriteConnection) error {
	buffer := []byte{0, 'p'}
	_, err := conn.WriteTo(buffer, m.Addr)
	return err
}

func (m *PingMessage) Action(cm *ConnManager, conn WriteConnection) {
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
		cm.FreeChunk(chunk)
		switch chunk.Data[1] {
		case 'r':
			return &RequestAckMsg{MsgBase{Addr: addr}}, nil
		case 'a':
			return &AckMessage{MsgBase{Addr: addr}}, nil
		case 'p':
			return &PingMessage{MsgBase{Addr: addr}}, nil
		}
		// not an IP Packet -> Control Message
		log.Println("control message")
	}

	cm.ReceivedChunk(addr, chunk)
	return chunk, nil
}

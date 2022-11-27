package gobonding

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/net/ipv4"
)

const (
	MTU = 1500

	// BSIZE is size of buffer to receive packets
	// (little bit bigger than maximum)
	BUFFERSIZE = 1718
)

type Message interface {
	Buffer() []byte
	String() string
}

type Chunk struct {
	Data [BUFFERSIZE + 2]byte
	Size uint16
}

func (msg *Chunk) Buffer() []byte {
	// log.Println("Send", c.Addr)
	return msg.Data[:msg.Size]
}

func (msg *Chunk) String() string {
	header, err := ipv4.ParseHeader(msg.Data[0:])
	if err != nil {
		return "Error parsing buffer"
	} else {
		return fmt.Sprintf("Packet %v: %v", msg.Size, header)
	}
}

type PongMsg struct {
}

func (msg *PongMsg) Buffer() []byte {
	return []byte{0, 'o'}
}

func (msg *PongMsg) String() string {
	return "Pong"
}

type PingMsg struct {
}

func (msg *PingMsg) Buffer() []byte {
	return []byte{0, 'i'}
}

func (msg *PingMsg) String() string {
	return "Ping"
}

type SpeedMsg struct {
	Speed uint64
}

func (msg *SpeedMsg) Buffer() []byte {
	buffer := []byte{0, 's', 0, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint64(buffer[2:], msg.Speed)
	return buffer
}

func (msg *SpeedMsg) String() string {
	return fmt.Sprintf("Speed: %v", msg.Speed)
}

type Wrapped uint16

func (msg Wrapped) Less(other Wrapped) bool {
	if msg < 0x1000 && other > 0xf000 {
		// Overflow case
		return false
	}
	if other < 0x1000 && msg > 0xf000 {
		// Overflow case
		return true
	}
	return msg < other
}

type ChangeMsg struct {
	NextChannelId uint16
	Age           Wrapped
}

func (m *ChangeMsg) Buffer() []byte {
	buffer := []byte{0, 'c', 0, 0, 0, 0}
	binary.BigEndian.PutUint16(buffer[2:4], m.NextChannelId)
	binary.BigEndian.PutUint16(buffer[4:6], uint16(m.Age))
	return buffer
}

func (msg *ChangeMsg) String() string {
	return fmt.Sprintf("ChangeChannel: %v", msg.NextChannelId)
}

func ChangeChannel(channel uint16, age Wrapped) *ChangeMsg {
	return &ChangeMsg{NextChannelId: channel, Age: age}
}

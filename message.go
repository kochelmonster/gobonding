package gobonding

import (
	"encoding/binary"
	"fmt"
)

const (
	MTU = 1500

	// BSIZE is size of buffer to receive packets
	// (little bit bigger than maximum)
	BUFFERSIZE = 1718

	SOCKET_BUFFER = 1024 * 1024
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
	return fmt.Sprintf("Chunk %v", msg.Size)
	/*
		header, err := ipv4.ParseHeader(msg.Data[0:])
		if err != nil {
			return "Error parsing buffer"
		} else {
			return fmt.Sprintf("Packet %v: %v", msg.Size, header)
		}*/
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

type StartBlockMsg struct {
	Age Wrapped
}

func (m *StartBlockMsg) Buffer() []byte {
	buffer := []byte{0, 'b', 0, 0}
	binary.BigEndian.PutUint16(buffer[2:4], uint16(m.Age))
	return buffer
}

func (msg *StartBlockMsg) String() string {
	return fmt.Sprintf("StartBlock: %v", msg.Age)
}

func StartBlock(age Wrapped) *StartBlockMsg {
	return &StartBlockMsg{Age: age}
}

// A wrapped counter
type Wrapped uint16

func (wrp Wrapped) Less(other Wrapped) bool {
	if wrp < 0x1000 && other > 0xf000 {
		// Overflow case
		return false
	}
	if other < 0x1000 && wrp > 0xf000 {
		// Overflow case
		return true
	}

	if other == 0 && wrp != 0 {
		return true
	}
	return wrp < other && wrp != 0
}

func (wrp *Wrapped) Inc() {
	(*wrp)++
	if *wrp == 0 {
		*wrp++
	}
}

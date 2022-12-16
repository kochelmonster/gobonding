package gobonding

import (
	"encoding/binary"
	"fmt"
	"time"
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

var epoch = time.Date(2022, time.January, 1, 0, 0, 0, 0, time.UTC)
var Epoch = epoch

type PongMsg struct {
	Timestamp time.Duration
}

func Pong() *PongMsg {
	return &PongMsg{Timestamp: time.Since(epoch)}
}

func PongFromChunk(chunk *Chunk) *PongMsg {
	ts := time.Duration(binary.BigEndian.Uint64(chunk.Data[2:10]))
	return &PongMsg{Timestamp: ts}
}

func (msg *PongMsg) Buffer() []byte {
	buffer := []byte{0, 'o', 0, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint64(buffer[2:10], uint64(msg.Timestamp))
	return buffer
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
	Age       Wrapped
	Timestamp time.Duration
	BlockSize uint32
}

func StartBlock(age Wrapped, ts time.Time, bs uint32) *StartBlockMsg {
	return &StartBlockMsg{Age: age, Timestamp: ts.Sub(epoch), BlockSize: bs}
}

func StartBlockFromChunk(chunk *Chunk) *StartBlockMsg {
	age := Wrapped(binary.BigEndian.Uint16(chunk.Data[2:4]))
	ts := time.Duration(binary.BigEndian.Uint64(chunk.Data[4:12]))
	bs := binary.BigEndian.Uint32(chunk.Data[12:16])
	return &StartBlockMsg{Age: age, Timestamp: ts, BlockSize: bs}
}

func (m *StartBlockMsg) Buffer() []byte {
	buffer := []byte{0, 'b', 1, 2, 1, 2, 3, 4, 5, 7, 7, 8, 1, 2, 3, 4}
	binary.BigEndian.PutUint16(buffer[2:4], uint16(m.Age))
	binary.BigEndian.PutUint64(buffer[4:12], uint64(m.Timestamp))
	binary.BigEndian.PutUint32(buffer[12:16], uint32(m.BlockSize))
	return buffer
}

func (msg *StartBlockMsg) String() string {
	return fmt.Sprintf("StartBlock: %v", msg.Age)
}

type StopBlockMsg struct{}

func (m *StopBlockMsg) Buffer() []byte {
	return []byte{}
}

func (msg *StopBlockMsg) String() string {
	return "StopBlock"
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

func (wrp Wrapped) Inc() Wrapped {
	(wrp)++
	if wrp == 0 {
		wrp++
	}
	return wrp
}

package gobonding

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/net/ipv4"
)

const (
	MTU = 1300

	// BUFFERSIZE is size of buffer to receive packets
	// (little bit bigger than maximum)
	BUFFERSIZE = 1518
)

type Chunk struct {
	Data [BUFFERSIZE + 2]byte
	Idx  uint16
	Size uint16
}

func (c *Chunk) Buffer() []byte {
	return c.Data[2 : BUFFERSIZE+2]
}

func (c *Chunk) ToSend() []byte {
	binary.BigEndian.PutUint16(c.Data[0:2], c.Idx)
	return c.Data[0 : c.Size+2]
}

/*
Decodes the transfered message
size is the received message size inclusive the message order
*/
func (c *Chunk) Decode(size uint16) {
	c.Idx = binary.BigEndian.Uint16(c.Data[0:2])
	c.Size = size - 2
}

func (c *Chunk) String() string {
	header, err := ipv4.ParseHeader(c.Buffer())
	if err != nil {
		return "Error parsing buffer"
	} else {
		return fmt.Sprintf("%v: %v", c.Idx, header)
	}
}

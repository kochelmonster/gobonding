package gobonding

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/net/ipv4"
)

const (
	MTU = 1300

	// BSIZE is size of buffer to receive packets
	// (little bit bigger than maximum)
	BUFFERSIZE = 1518
)

type Message interface {
	Write(stream io.Writer) error
	Action(ctx context.Context, cm *ConnManager) error
	String() string
}

func ReadMessage(stream io.Reader, cm *ConnManager) (Message, error) {
	buffer := []byte{0, 0}

	_, err := io.ReadFull(stream, buffer)
	if err != nil {
		return nil, err
	}

	size := binary.BigEndian.Uint16(buffer)
	if size&0x8000 != 0 {
		// control message
		size = size & 0x7fff
		switch size {
		case 0:
			_, err := io.ReadFull(stream, buffer)
			if err != nil {
				return nil, err
			}
			return &SyncOrderMsg{
				Order: binary.BigEndian.Uint16(buffer),
			}, nil
		}
		return nil, errors.New("wrong Control message")
	}

	size = size & 0x7fff
	chunk := cm.AllocChunk()

	// Read Chunk Order
	_, err = io.ReadFull(stream, buffer)
	if err != nil {
		return nil, err
	}
	chunk.Idx = binary.BigEndian.Uint16(buffer[:2])

	chunk.Size = size - 2
	_, err = io.ReadFull(stream, chunk.Data[0:chunk.Size])
	if err != nil {
		cm.FreeChunk(chunk)
		return nil, err
	}
	return chunk, nil
}

type SyncOrderMsg struct {
	Message
	Order uint16
}

func (msg *SyncOrderMsg) Write(stream io.Writer) error {
	buffer := []byte{0, 0, 0, 0}
	binary.BigEndian.PutUint16(buffer[0:2], 0x8000)
	binary.BigEndian.PutUint16(buffer[2:4], msg.Order)
	_, err := stream.Write(buffer)
	return err
}

func (msg *SyncOrderMsg) Action(ctx context.Context, cm *ConnManager) error {
	cm.PeerOrder = msg.Order
	cm.LocalOrder = 0
	cm.Clear()
	return nil
}

func (msg *SyncOrderMsg) String() string {
	return fmt.Sprintf("SyncMessage: %v", msg.Order)
}

type Chunk struct {
	Message
	Data [BUFFERSIZE]byte
	Idx  uint16
	Size uint16
}

func (c *Chunk) Write(stream io.Writer) error {
	buffer := []byte{0, 0, 0, 0}

	binary.BigEndian.PutUint16(buffer[0:2], c.Size+2)
	binary.BigEndian.PutUint16(buffer[2:4], c.Idx)
	_, err := stream.Write(buffer)
	if err != nil {
		return err
	}

	_, err = stream.Write(c.Data[:c.Size])
	return err
}

func (c *Chunk) Action(ctx context.Context, cm *ConnManager) error {
	select {
	case cm.CollectChannel <- c:
	case <-ctx.Done():
		cm.FreeChunk(c)
		return errors.New("cancel writing Chunk")
	}
	return nil
}

func (c *Chunk) String() string {
	header, err := ipv4.ParseHeader(c.Data[0:])
	if err != nil {
		return "Error parsing buffer"
	} else {
		return fmt.Sprintf("Packet %v(%v): %v", c.Idx, c.Size, header)
	}
}

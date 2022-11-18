package gobonding

import (
	"context"
	"errors"
	"fmt"
	"net"

	"golang.org/x/net/ipv4"
)

const (
	MTU = 1300

	// BSIZE is size of buffer to receive packets
	// (little bit bigger than maximum)
	BUFFERSIZE = 1518
)

type Message interface {
	Write(conn *net.UDPConn) error
	Action(ctx context.Context, cm *ConnManager) error
	Src() net.Addr
	String() string
}

type Chunk struct {
	Message
	Data [BUFFERSIZE]byte
	Size uint16
	Addr net.Addr
}

func (c *Chunk) Src() net.Addr {
	return c.Addr
}

func (c *Chunk) Write(conn *net.UDPConn) error {
	_, err := conn.Write(c.Data[:c.Size])
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
		return fmt.Sprintf("Packet %v: %v", c.Size, header)
	}
}

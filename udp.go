package gobonding

import (
	"context"
	"log"
	"net"
	"sync"
)

type UDPConn interface {
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	SendMessage(msg Message) error
	ReadMessage() (Message, error)
	Close()
}

type UDPConnBase struct {
	conn *net.UDPConn
}

type UDPDialConnection struct {
	UDPConnBase
	cm *ConnManager
}

type UDPListenerConnection struct {
	UDPConnBase
	receiver chan Message
}

type UDPListener struct {
	channels map[net.Addr]UDPListenerConnection
	conn     *net.UDPConn
	cm       *ConnManager
}

func NewUDPDialer(conn *net.UDPConn, cm *ConnManager) *UDPDialConnection {
	return &UDPDialConnection{
		UDPConnBase: UDPConnBase{
			conn: conn,
		},
		cm: cm,
	}
}

func NewUDPListener(conn *net.UDPConn, cm *ConnManager) *UDPListener {
	return &UDPListener{
		channels: map[net.Addr]UDPListenerConnection{},
		conn:     conn,
		cm:       cm,
	}
}

func readMessage(conn *net.UDPConn, cm *ConnManager) (Message, error) {
	chunk := cm.AllocChunk()
	size, addr, err := conn.ReadFrom(chunk.Data[0:])
	if err != nil {
		cm.FreeChunk(chunk)
		return nil, err
	}
	chunk.Size = uint16(size)
	chunk.Addr = addr

	if chunk.Data[0] == 0 {
		// not an IP Packet -> Control Message
		log.Println("control message")
	}

	return chunk, nil
}

func (l *UDPListener) Accept() (UDPConn, error) {
	for {
		msg, err := readMessage(l.conn, l.cm)
		if err != nil {
			return nil, err
		}
		if conn, ok := l.channels[msg.Src()]; ok {
			conn.receiver <- msg
		} else {
			result := UDPListenerConnection{
				UDPConnBase: UDPConnBase{conn: l.conn},
				receiver:    make(chan Message, 1),
			}
			l.channels[msg.Src()] = result
			result.receiver <- msg
			return &result, nil
		}
	}
}

func (c *UDPConnBase) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *UDPConnBase) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *UDPConnBase) Close() {
	c.conn.Close()
}

func (c *UDPListenerConnection) SendMessage(msg Message) error {
	return msg.Write(c.UDPConnBase.conn)
}

func (c *UDPListenerConnection) ReadMessage() (Message, error) {
	return <-c.receiver, nil
}

func (c *UDPDialConnection) SendMessage(msg Message) error {
	return msg.Write(c.UDPConnBase.conn)
}

func (c *UDPDialConnection) ReadMessage() (Message, error) {
	return readMessage(c.conn, c.cm)
}

func HandleCommunication(ctx context.Context, conn UDPConn, cm *ConnManager) {
	var wg sync.WaitGroup
	wg.Add(2)

	ctx, cancel := context.WithCancel(ctx)

	go func() {
		defer wg.Done()
		for {
			msg, err := conn.ReadMessage()
			// log.Println("Receive from", conn.LocalAddr(), cm.PeerOrder, msg)
			if err != nil {
				log.Println("Closing receive stream to", conn.RemoteAddr(), err)
				conn.Close()
				cancel()
				return
			}
			msg.Action(ctx, cm)
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case msg := <-cm.DispatchChannel:
				// log.Println("Send to", conn.RemoteAddr(), conn.LocalAddr(), msg)
				err := conn.SendMessage(msg)
				if err != nil {
					cm.DispatchChannel <- msg
					log.Println("Closing send stream to", conn.RemoteAddr(), err)
					conn.Close()
					return
				}
			case <-ctx.Done():
				conn.Close()
				return
			}
		}
	}()

	id := conn.LocalAddr().String() + "->" + conn.RemoteAddr().String()
	cm.ActiveChannels[id] = true
	log.Printf("Connection established %v: %v\n", id, cm.ActiveChannels)
	wg.Wait()
	delete(cm.ActiveChannels, id)
	log.Printf("Close connection %v: %v\n", id, cm.ActiveChannels)
}

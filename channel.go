package gobonding

import (
	"encoding/binary"
	"log"
	"net"
	"time"
)

type Channel struct {
	Id            uint16
	ReceiveChl    chan Message
	conn          *net.UDPConn
	cm            *ConnManager
	lastHeartbeat time.Time
	ReceiveSpeed  uint64 // bytes per second
	SendSpeed     uint64 // bytes per second
}

func NewChannel(cm *ConnManager, id uint16, conn *net.UDPConn) *Channel {
	chl := &Channel{
		Id:            id,
		ReceiveChl:    make(chan Message, 100),
		conn:          conn,
		cm:            cm,
		lastHeartbeat: time.Time{},
		SendSpeed:     0,
		ReceiveSpeed:  0,
	}
	cm.Channels[id] = chl
	return chl
}

func (chl *Channel) Active() bool {
	return time.Since(chl.lastHeartbeat) < chl.cm.heartbeat
}

func (chl *Channel) Start() *Channel {
	go chl.receiver()
	go chl.pinger()
	return chl
}

func (chl *Channel) Send(msg Message) {
	chl.conn.Write(msg.Buffer())
}

func (chl *Channel) Ping() *Channel {
	chl.Send(&PingMsg{})
	return chl
}

func (chl *Channel) pinger() {
	ticker := time.NewTicker(chl.cm.heartbeat)
	for {
		select {
		case <-ticker.C:
			if !chl.Active() {
				chl.Ping()
			}

		case <-chl.cm.ctx.Done():
			return
		}
	}
}

func (chl *Channel) receiver() {
	lastTime := time.Now()
	received := 0

	var msg Message = nil

	for {
		chunk := chl.cm.AllocChunk()

		size, _, err := chl.conn.ReadFrom(chunk.Data[0:])
		if err != nil {
			chl.cm.FreeChunk(chunk)
			log.Println("Error reading from connection", chl, err)
			return
		}

		if chl.cm.needSignal && len(chl.cm.signal) == 0 {
			chl.cm.signal <- true
			chl.cm.needSignal = false
		}

		received += size
		chl.lastHeartbeat = time.Now()
		if duration := time.Since(lastTime); duration >= time.Second {
			lastTime = chl.lastHeartbeat
			chl.ReceiveSpeed = uint64(received) * uint64(time.Second) /
				uint64(duration)
			chl.conn.Write((&SpeedMsg{Speed: chl.ReceiveSpeed}).Buffer())
		}

		if chunk.Data[0] == 0 { // the ip header first byte
			// not an IP Packet -> Control Message
			defer chl.cm.FreeChunk(chunk)

			switch chunk.Data[1] {
			case 'o':
				continue
			case 'i':
				chl.conn.Write((&PongMsg{}).Buffer())
				continue
			case 's':
				chl.SendSpeed = binary.BigEndian.Uint64(chunk.Data[2:10])
				continue
			case 'c':
				msg = ChangeChannel(
					binary.BigEndian.Uint16(chunk.Data[2:4]),
					Wrapped(binary.BigEndian.Uint16(chunk.Data[4:6])))
			}
		} else {
			chunk.Size = uint16(size)
			msg = chunk
		}

		select {
		case chl.ReceiveChl <- msg:
		case <-chl.cm.ctx.Done():
		}
	}
}

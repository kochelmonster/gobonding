package gobonding

import (
	"encoding/binary"
	"log"
	"time"
)

type ChannelIO interface {
	Write(buffer []byte)
	Read(buffer []byte) (int, error)
	Close()
}

type Channel struct {
	Id            uint16
	ReceiveChl    chan Message
	io            ChannelIO
	cm            *ConnManager
	lastHeartbeat time.Time
	ReceiveSpeed  uint64 // bytes per second
	SendSpeed     uint64 // bytes per second
}

func NewChannel(cm *ConnManager, id uint16, io ChannelIO) *Channel {
	chl := &Channel{
		Id:            id,
		ReceiveChl:    make(chan Message, 100),
		io:            io,
		cm:            cm,
		lastHeartbeat: time.Time{},
		ReceiveSpeed:  0,
		SendSpeed:     0,
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
	chl.io.Write(msg.Buffer())
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
	log.Println("start receiver", chl.io)
	for {
		chunk := chl.cm.AllocChunk()

		size, err := chl.io.Read(chunk.Data[0:])
		if err != nil {
			chl.cm.FreeChunk(chunk)
			log.Println("Error reading from connection", chl, err)
			return
		}
		log.Println("received", chunk.Data[:size])

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
			chl.io.Write((&SpeedMsg{Speed: chl.ReceiveSpeed}).Buffer())
		}

		if chunk.Data[0] == 0 { // the ip header first byte
			// not an IP Packet -> Control Message
			defer chl.cm.FreeChunk(chunk)

			switch chunk.Data[1] {
			case 'o':
				continue
			case 'i':
				chl.io.Write((&PongMsg{}).Buffer())
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

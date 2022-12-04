package gobonding

import (
	"encoding/binary"
	"log"
	"time"
)

type ChannelIO interface {
	Write(buffer []byte) (int, error)
	Read(buffer []byte) (int, error)
	Close() error
}

type Channel struct {
	Id            uint16
	ReceiveChl    chan Message
	Io            ChannelIO
	cm            *ConnManager
	lastHeartbeat time.Time
	ReceiveSpeed  uint64 // bytes per second
	SendSpeed     uint64 // bytes per second
}

func NewChannel(cm *ConnManager, id uint16, io ChannelIO) *Channel {
	chl := &Channel{
		Id:            id,
		ReceiveChl:    make(chan Message, 100),
		Io:            io,
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
	// log.Println("Send", chl.Id, msg)
	chl.Io.Write(msg.Buffer())
}

func (chl *Channel) Ping() *Channel {
	chl.Send(&PingMsg{})
	return chl
}

func (chl *Channel) pinger() {
	ticker := time.NewTicker(chl.cm.heartbeat * 2 / 3)
	for {
		select {
		case <-ticker.C:
			chl.Ping()

		case <-chl.cm.ctx.Done():
			log.Println("Stop Pinger", chl.Id)
			return
		}
	}
}

func (chl *Channel) receiver() {
	defer log.Println("done receiver", chl.Id)

	lastTime := time.Now()
	received := 0

	var msg Message = nil
	chl.cm.Log("start channel receiver %v\n", chl.Id)
	chl.Ping().Ping()
	for {
		chunk := chl.cm.AllocChunk()

		size, err := chl.Io.Read(chunk.Data[0:])
		if err != nil {
			chl.cm.FreeChunk(chunk)
			chl.cm.Log("Error reading from connection %v %v", chl.Id, err)
			return
		}

		// chl.cm.Log("receive from channel %v: %v %v\n", chl.Id, size, chunk.Data[:4])

		if !chl.cm.active {
			active := chl.cm.setActive()
			chl.cm.Log("reactivate channel %v %v\n", chl.Id, active)
		}

		received += size
		chl.lastHeartbeat = time.Now()
		if duration := time.Since(lastTime); duration >= time.Second {
			lastTime = chl.lastHeartbeat
			speed := uint64(received) * uint64(time.Second) / uint64(duration)
			if speed > 128*1024/8 { // 128kbps
				chl.cm.Log("Send ReceiveSpeed %v\n", speed)
				chl.ReceiveSpeed = speed
				chl.Io.Write((&SpeedMsg{Speed: chl.ReceiveSpeed}).Buffer())
			}
		}

		if chunk.Data[0] == 0 { // the ip header first byte
			// not an IP Packet -> Control Message
			defer chl.cm.FreeChunk(chunk)

			switch chunk.Data[1] {
			case 'o':
				continue
			case 'i':
				chl.Io.Write((&PongMsg{}).Buffer())
				chl.Io.Write((&PongMsg{}).Buffer())
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

		// chl.cm.Log("receive from channel %v %v: %v\n", chl.Id, size, msg)
		select {
		case chl.ReceiveChl <- msg:
		case <-chl.cm.ctx.Done():
			log.Println("Stop Receiver", chl.Id)
		}
	}
}

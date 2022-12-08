package gobonding

import (
	"encoding/binary"
	"fmt"
	"log"
	"time"
)

type ChannelIO interface {
	Write(buffer []byte) (int, error)
	Read(buffer []byte) (int, error)
	Close() error
}

type Channel struct {
	Id  uint16
	Io  ChannelIO
	Age Wrapped

	startSignal   chan bool
	transmitChl   chan Message
	cm            *ConnManager
	lastHeartbeat time.Time
	ReceiveSpeed  uint64 // bytes per second
	SendSpeed     uint64 // bytes per second
}

func NewChannel(cm *ConnManager, id uint16, io ChannelIO) *Channel {
	chl := &Channel{
		Id:            id,
		Io:            io,
		Age:           Wrapped(0),
		startSignal:   make(chan bool),
		transmitChl:   make(chan Message, 500),
		cm:            cm,
		lastHeartbeat: time.Time{},
		ReceiveSpeed:  0,
		SendSpeed:     0,
	}
	cm.Channels[id] = chl
	return chl
}

func (chl *Channel) String() string {
	return fmt.Sprintf("Chl %v", chl.Id)
}

func (chl *Channel) Active() bool {
	return time.Since(chl.lastHeartbeat) < chl.cm.heartbeat
}

func (chl *Channel) Start() *Channel {
	go chl.receiver()
	go chl.transmitter()
	go chl.pinger()
	return chl
}

func (chl *Channel) Send(msg Message) {
	// chl.cm.Log("channel send %v %v %v", chl.Id, msg, string(msg.Buffer()))
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
			// chl.cm.Log("Send Ping %v\n", chl.Id)
			chl.Ping()

		case <-chl.cm.ctx.Done():
			chl.cm.Log("Stop Pinger %v\n", chl.Id)
			return
		}
	}
}

func (chl *Channel) receiver() {
	defer chl.cm.Log("Stop receiver %v\n", chl.Id)

	var msg Message
	lastTime := time.Now()
	received := 0

	lastAge := Wrapped(0)
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
		// chl.cm.Log("channel receive  %v: %v %v\n", chl.Id, size, string(chunk.Data[:4]))

		received += size
		chl.lastHeartbeat = time.Now()
		if duration := time.Since(lastTime); duration >= time.Second {
			lastTime = chl.lastHeartbeat
			speed := uint64(received) * uint64(time.Second) / uint64(duration)
			if speed > 128*1024/8 { // 128kbps
				// chl.cm.Log("Send ReceiveSpeed %v %v\n", chl.Id, speed)
				chl.ReceiveSpeed = speed
				chl.Io.Write((&SpeedMsg{Speed: chl.ReceiveSpeed}).Buffer())
			}
		}

		if chunk.Data[0] == 0 { // the ip header first byte
			// not an IP Packet -> Control Message
			defer chl.cm.FreeChunk(chunk)

			switch chunk.Data[1] {
			case 'o':
				// do nothing
				continue
			case 'i':
				chl.Io.Write((&PongMsg{}).Buffer())
				chl.Io.Write((&PongMsg{}).Buffer())
				continue
			case 's':
				chl.SendSpeed = binary.BigEndian.Uint64(chunk.Data[2:10])
				// chl.cm.Log("Update Sendspeed %v %v", chl.Id, chl.SendSpeed)
				continue
			case 'b':
				sb := StartBlock(Wrapped(binary.BigEndian.Uint16(chunk.Data[2:4])))
				// chl.cm.Log("receiver %v %v", chl.Id, sb)
				if lastAge == sb.Age {
					continue
				}
				lastAge = sb.Age
				msg = sb
			}
		} else {
			chunk.Size = uint16(size)
			msg = chunk
		}

		select {
		case chl.transmitChl <- msg:
		case <-chl.cm.ctx.Done():
			log.Println("Stop Receiver", chl.Id)
			return
		}
	}
}

func (chl *Channel) transmitter() {
	defer chl.cm.Log("Stop transmitter %v\n", chl.Id)

	chl.cm.Log("start channel transmitter %v\n", chl.Id)
	for {
		select {
		case msg := <-chl.transmitChl:
			switch msg := msg.(type) {
			case *StartBlockMsg:
				chl.cm.Log("received Startblock %v %v -> %v\n", chl.Id, chl.Age, msg.Age)
				chl.Age = msg.Age
				if chl.cm.handleBlockMsg(chl) && msg.Age != 0 {
					select {
					case <-chl.startSignal:
					case <-chl.cm.ctx.Done():
						log.Println("Stop Transmitter", chl.Id)
						return
					}
				}

			case *Chunk:
				chl.cm.Log("received chunk %v %v\n", chl.Id, msg)
				select {
				case chl.cm.receiveChunks <- msg:
				case <-chl.cm.ctx.Done():
					log.Println("Stop Receiver", chl.Id)
					return
				}
			}

		case <-chl.cm.ctx.Done():
			log.Println("Stop Receiver", chl.Id)
			return
		}
	}
}

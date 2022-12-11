package gobonding

import (
	"fmt"
	"log"
	"time"
)

const (
	MIN_SPEED = 128 * 1024 / 8 // 128kbps
	HEARTBEAT = 2 * time.Minute
	INACTIVE  = 3 * time.Minute
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
	waitForSignal bool

	transmitChl   chan Message
	cm            *ConnManager
	lastHeartbeat time.Time
	lastPing      time.Time
	clockDelta    time.Duration

	TransmissionSpeed uint64 // bytes per second
}

func NewChannel(cm *ConnManager, id uint16, io ChannelIO) *Channel {
	chl := &Channel{
		Id:                id,
		Io:                io,
		Age:               Wrapped(0),
		startSignal:       make(chan bool),
		waitForSignal:     false,
		transmitChl:       make(chan Message, 500),
		cm:                cm,
		lastHeartbeat:     time.Time{},
		lastPing:          time.Time{},
		clockDelta:        0,
		TransmissionSpeed: MIN_SPEED,
	}
	cm.Channels[id] = chl
	return chl
}

func (chl *Channel) String() string {
	return fmt.Sprintf("Chl %v", chl.Id)
}

func (chl *Channel) Active() bool {
	return time.Since(chl.lastHeartbeat) < INACTIVE
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
	chl.lastPing = time.Now()
	chl.Send(&PingMsg{})
	return chl
}

func (chl *Channel) pinger() {
	ticker := time.NewTicker(HEARTBEAT)
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
	received := uint64(0)

	lastAge := Wrapped(0)
	chl.cm.Log("start channel receiver %v\n", chl.Id)
	chl.Ping()
	for {
		chunk := chl.cm.AllocChunk()

		size, err := chl.Io.Read(chunk.Data[0:])
		if err != nil {
			chl.cm.FreeChunk(chunk)
			chl.cm.Log("Error reading from connection %v %v", chl.Id, err)
			return
		}
		// chl.cm.Log("channel receive  %v: %v %v\n", chl.Id, size, string(chunk.Data[:4]))

		received += uint64(size)
		chl.lastHeartbeat = time.Now()

		if chunk.Data[0] == 0 { // the ip header first byte
			// not an IP Packet -> Control Message
			defer chl.cm.FreeChunk(chunk)

			switch chunk.Data[1] {
			case 'o':
				pong := PongFromChunk(chunk)
				d1 := chl.lastHeartbeat.Sub(chl.lastPing) / 2
				tl := chl.lastHeartbeat.Add(d1)
				tp := epoch.Add(pong.Timestamp)
				chl.clockDelta = tl.Sub(tp)
				continue
			case 'i':
				chl.Io.Write(Pong().Buffer())
				continue
			case 'b':
				sb := StartBlockFromChunk(chunk)
				// chl.cm.Log("receiver %v %v", chl.Id, sb)
				if lastAge == sb.Age {
					continue
				}
				if sb.Age == 0 {
					tp := epoch.Add(sb.Timestamp + chl.clockDelta)
					d := tp.Sub(chl.lastHeartbeat)
					chl.TransmissionSpeed = received * uint64(time.Second) / uint64(d)
				} else {
					received = 0
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
	count := 0
	bytes := 0
	for {
		select {
		case msg := <-chl.transmitChl:
			switch msg := msg.(type) {
			case *StartBlockMsg:
				if msg.Age == 0 {
					//chl.cm.Log("received StopBlock %v(%v): %v(%v)\n", chl.Id, chl.Age, count, bytes)
				} else {
					//chl.cm.Log("received Startblock %v(%v)\n", chl.Id, msg.Age)
					count = 0
					bytes = 0
				}
				chl.Age = msg.Age
				chl.cm.handleBlockMsg(chl)
				if msg.Age != 0 {
					chl.cm.waitForActivation(chl)
					// chl.cm.Log("receive start block %v(%v) %v b/s\n", chl.Id, chl.Age, chl.ReceiveSpeed)
				}

			case *Chunk:
				if chl.Age != 0 {
					count++
					bytes += int(msg.Size)
				}
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

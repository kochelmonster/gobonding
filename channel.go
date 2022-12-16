package gobonding

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

const (
	MIN_SPEED = 128 * 1024 / 8   // 128kbps
	HEARTBEAT = 20 * time.Second // 2 * time.Minute
	INACTIVE  = 30 * time.Second //3 * time.Minute
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

	sendQueue    chan Message
	queue        chan Message
	pendingChunk *Chunk

	cm            *ConnManager
	lastHeartbeat time.Time
	lastPing      time.Time
	clockDelta    time.Duration

	ReceiveSpeed uint64 // bytes per second
	SendSpeed    uint64
}

func NewChannel(cm *ConnManager, id uint16, io ChannelIO) *Channel {
	chl := &Channel{
		Id:            id,
		Io:            io,
		Age:           Wrapped(0),
		sendQueue:     make(chan Message, 200),
		queue:         make(chan Message, 500),
		pendingChunk:  nil,
		cm:            cm,
		lastHeartbeat: time.Time{},
		lastPing:      time.Time{},
		clockDelta:    0,
		ReceiveSpeed:  MIN_SPEED,
		SendSpeed:     MIN_SPEED,
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
	go chl.sender()
	return chl
}

func (chl *Channel) Ping() *Channel {
	chl.lastPing = time.Now()
	chl.sendQueue <- &PingMsg{}
	return chl
}

func (chl *Channel) sender() {
	ticker := time.NewTicker(HEARTBEAT)
	for {
		select {
		case msg := <-chl.sendQueue:
			//chl.cm.Log("channel send %v %v %v", chl.Id, msg, string(msg.Buffer()[:4]))
			chl.Io.Write(msg.Buffer())
			switch msg := msg.(type) {
			case *Chunk:
				chl.cm.FreeChunk(msg)
			}

		case <-ticker.C:
			// chl.cm.Log("Send Ping %v\n", chl.Id)
			chl.Ping()

		case <-chl.cm.ctx.Done():
			chl.cm.Log("Stop sender %v\n", chl.Id)
			return
		}
	}
}

func (chl *Channel) receiver() {
	defer chl.cm.Log("Stop receiver %v\n", chl.Id)

	lastAge := Wrapped(0)
	chl.cm.Log("start channel receiver %v\n", chl.Id)
	chl.Ping()

	lastSpeed := uint64(0)
	received := uint64(0)
	duration := time.Duration(0)
	chunkBytes := 0
	blockSize := 0
	blockStart := time.Now()

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
				chl.sendQueue <- Pong()
				continue
			case 's':
				chl.SendSpeed = binary.BigEndian.Uint64(chunk.Data[2:10])
				// chl.cm.Log("Update Sendspeed %v %v", chl.Id, chl.SendSpeed)
				continue
			case 'b':
				sb := StartBlockFromChunk(chunk)
				// chl.cm.Log("receiver %v %v", chl.Id, sb)
				if lastAge == sb.Age {
					continue
				}
				/*
					if chunkBytes < blockSize {
						chl.cm.Log("Missing chunks chunk %v(%v->%v): %v >= %v",
							chl.Id, chl.Age, sb.Age, chunkBytes, blockSize)
					}
				*/

				blockSize = int(sb.BlockSize)
				chunkBytes = 0
				blockStart = epoch.Add(sb.Timestamp)
				lastAge = sb.Age
				chl.queue <- &StopBlockMsg{}
				chl.queue <- sb
			}
		} else {
			chunkBytes += size
			chunk.Size = uint16(size)
			// chl.cm.Log("receiver %v %v", chl.Id, chunk)
			chl.queue <- chunk

			if chunkBytes >= blockSize && chunkBytes > 0 {
				// End of block
				duration += chl.lastHeartbeat.Sub(blockStart)
				if duration >= time.Second {
					chl.ReceiveSpeed = received * uint64(time.Second) / uint64(duration)
					duration = 0
					received = 0
					if chl.ReceiveSpeed < MIN_SPEED {
						chl.ReceiveSpeed = MIN_SPEED
					}

					if lastSpeed != chl.ReceiveSpeed {
						//chl.cm.Log("Send ReceiveSpeed %v %v\n", chl.Id, chl.ReceiveSpeed)
						chl.sendQueue <- &SpeedMsg{Speed: chl.ReceiveSpeed}
						lastSpeed = chl.ReceiveSpeed
					}
				}

				chunkBytes = 0
				blockSize = 0
				chl.queue <- &StopBlockMsg{}
			}

			chl.cm.receiveSignal <- true
		}
	}
}

func (chl *Channel) Write(iface io.ReadWriteCloser, age *Wrapped) (bool, error) {
	written := false

	write := func(chunk *Chunk) error {
		defer chl.cm.FreeChunk(chunk)
		written = true
		_, err := iface.Write(chunk.Data[:chunk.Size])
		return err
	}

	if chl.pendingChunk != nil {
		if chl.Age != *age {
			return written, nil
		}
		err := write(chl.pendingChunk)
		chl.pendingChunk = nil
		if err != nil {
			return written, err
		}
	}

	for {
		if len(chl.queue) == 0 {
			return written, nil
		}

		msg := <-chl.queue
		switch msg := msg.(type) {
		case *StartBlockMsg:
			/*chl.cm.Log("StartBlock %v: %v -> %v ?= %v len=%v bs=%v",
			chl.Id, chl.Age, msg.Age, *age, len(chl.queue), msg.BlockSize)*/
			chl.Age = msg.Age

		case *StopBlockMsg:
			chl.cm.Log("Stop Block %v(%v) %v", chl.Id, chl.Age, *age)
			if chl.Age == *age {
				*age = (*age).Inc()
			}
			chl.Age = 0

		case *Chunk:
			if chl.Age == *age || chl.Age == 0 {
				/*if chl.Age == 0 {
					chl.cm.Log("Null Chunk %v(%v): %v", chl.Id, *age, msg)
				}*/

				err := write(msg)
				if err != nil {
					return written, err
				}
			} else {
				chl.pendingChunk = msg
				return written, nil
			}
		}
	}
}

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
	Id      uint16
	Io      ChannelIO
	Latency time.Duration

	sendQueue    chan Message
	queue        chan *Chunk
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
		Latency:       10 * time.Millisecond,
		sendQueue:     make(chan Message, 200),
		queue:         make(chan *Chunk, 500),
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
		chl.Latency = (time.Since(chl.lastHeartbeat) + chl.Latency*9) / 10
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
				continue
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
		if err != nil && *age == chunk.Age {
			*age = (*age).Inc()
		}
		return err
	}

	if chl.pendingChunk != nil {
		if chl.pendingChunk.Age == *age || chl.pendingChunk.Age.Less(*age) {
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

		chunk := <-chl.queue
		if chunk.Age == *age || chunk.Age.Less(*age) {
			err := write(chunk)
			if err != nil {
				return written, err
			}
		} else {
			chl.pendingChunk = chunk
			return written, nil
		}
	}
}

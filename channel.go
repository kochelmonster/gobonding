package gobonding

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"time"
)

const (
	HEARTBEAT    = 20 * time.Second // 2 * time.Minute
	INACTIVE     = 30 * time.Second //3 * time.Minute
	MIN_SPEED    = 128 * 1024 / 8   // 128kbps
	SPEED_WINDOW = 500 * time.Millisecond
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
	pendingChunk *Chunk

	cm            *ConnManager
	lastHeartbeat time.Time
	Authenticated bool

	ReceiveSpeed float32 // bytes per second
	SendSpeed    float32
}

func NewChannel(cm *ConnManager, id uint16, io ChannelIO, isProxy bool) *Channel {
	chl := &Channel{
		Id:            id,
		Io:            io,
		Latency:       10 * time.Millisecond,
		sendQueue:     make(chan Message, 50),
		pendingChunk:  nil,
		cm:            cm,
		lastHeartbeat: time.Time{},
		Authenticated: !isProxy,
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
	chl.sendQueue <- &PingMsg{}
	return chl
}

func (chl *Channel) sender() {
	ticker := time.NewTicker(HEARTBEAT)
	ts := time.Time{}
	sent := uint32(0)
	for {
		select {
		case msg := <-chl.sendQueue:
			chl.cm.Log(DEBUG2, "channel send %v %v", chl.Id, msg)
			switch msg := msg.(type) {
			case *Chunk:
				if ts.IsZero() {
					ts = time.Now()
					sent = uint32(msg.Size)
				} else {
					sent += uint32(msg.Size)
					if time.Since(ts) >= SPEED_WINDOW {
						chl.Io.Write(SpeedTest(msg.Age, ts, sent).Buffer())
						ts = time.Time{}
					}
				}
			}
			chl.Io.Write(msg.Buffer())

		case <-ticker.C:
			chl.Ping()

		case <-chl.cm.ctx.Done():
			chl.cm.Log(INFO, "Stop sender %v\n", chl.Id)
			return
		}
	}
}

func (chl *Channel) receiver() {
	defer chl.cm.Log(INFO, "Stop receiver %v\n", chl.Id)

	chl.cm.Log(INFO, "start channel receiver %v\n", chl.Id)

	chl.Ping()
	chl.lastHeartbeat = time.Now().Add(-20 * time.Hour)

	var test *SpeedTestMsg = nil
	challenge := Challenge()

	for {
		chunk := chl.cm.AllocChunk()
		lat := time.Since(chl.lastHeartbeat)
		if lat > 10*time.Millisecond {
			lat = 10 * time.Millisecond
		}
		chl.Latency = (lat + chl.Latency*19) / 20
		size, err := chl.Io.Read(chunk.Data[0:])
		if err != nil {
			chl.cm.Log(ERROR, "Error reading from connection %v %v", chl.Id, err)
			return
		}
		chl.cm.Log(DEBUG2, "channel receive  %v: %v %v %v\n", chl.Id, size, string(chunk.Data[1]), chunk.Data[:4])

		if !chl.Authenticated {
			if chunk.Data[0] == 0 && chunk.Data[1] == 'r' {
				bPem, _ := pem.Decode([]byte(chl.cm.Config.PublicKey))
				if bPem == nil {
					log.Panicln("Not Public Key defined")
				}
				key, err := x509.ParsePKIXPublicKey(bPem.Bytes)
				if err != nil {
					log.Panicln("Wrong Public Key in config")
				}

				err = challenge.Verify(key.(*rsa.PublicKey), chunk, size)
				if err == nil {
					chl.cm.Log(INFO, "verified!! %v", chl.Id)
					chl.Authenticated = true
				} else {
					chl.cm.Log(ERROR, "Not verified %v: %v", chl.Id, err)
				}
			} else {
				chl.sendQueue <- challenge
			}
			continue
		}

		chl.lastHeartbeat = time.Now()

		if chunk.Data[0] == 0 { // the ip header first byte
			// not an IP Packet -> Control Message
			switch chunk.Data[1] {
			case 'o':
				continue
			case 'i':
				chl.sendQueue <- Pong()
				continue
			case 's':
				chl.SendSpeed = SpeedFromChunk(chunk).Speed
				chl.cm.Log(DEBUG, "Update Sendspeed %v", chl.Id)
				continue
			case 'b':
				test = SpeedTestFromChunk(chunk)
				chl.cm.Log(DEBUG, "Got Start block %v %v", chl.Id, chunk.Age)
			case 'c':
				bPem, _ := pem.Decode([]byte(chl.cm.Config.PrivateKey))
				if bPem == nil {
					log.Panicln("Not Private Key defined")
				}
				key, err := x509.ParsePKCS1PrivateKey(bPem.Bytes)
				if err != nil {
					log.Panicln("Wrong Private Key in config")
				}
				challenge := ChallengeFromChunk(chunk, size)
				chl.sendQueue <- challenge.CreateResponse(key)
				chl.cm.Log(DEBUG, "Send Verification %v", chl.Id)
				continue
			case 'r':
				continue
			}
		} else {
			chunk.Gather(uint16(size))
			if test != nil && test.Age == chunk.Age {
				d := time.Since(epoch.Add(test.Timestamp))
				speed := float32(test.Size) * float32(time.Second) / float32(d)
				if speed < MIN_SPEED {
					speed = MIN_SPEED
				}

				if float32(speed) < 0.9*float32(chl.ReceiveSpeed) ||
					1.1*float32(chl.ReceiveSpeed) < float32(speed) {
					chl.ReceiveSpeed = speed
					chl.sendQueue <- &SpeedMsg{Speed: chl.ReceiveSpeed}
					chl.cm.Log(INFO, "Calc Speed %v(%v-%v): %v = %v / %v Speed=%v",
						chl.Id, test.Age, chunk.Age, speed, test.Size, d,
						chl.cm.ReceiveSpeeds())
				}
				test = nil
			}

			// chl.cm.Log("receiver %v %v", chl.Id, chunk)
			chl.cm.ChunksToWrite <- chunk
		}
	}
}

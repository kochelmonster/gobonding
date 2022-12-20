package gobonding

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/repeale/fp-go"
)

const (
	// AppVersion contains current application version for -version command flag
	AppVersion = "0.2.0"

	MIN_SEND_LIMIT = uint32(2 * MTU)
)

/*
Dispatches Chunks to Channels
*/
type ConnManager struct {
	ChunkSupply chan *Chunk
	Channels    []*Channel
	Config      *Config

	ChunksToWrite chan *Chunk
	pqueue        []*Chunk
	receiveSignal chan bool
	ctx           context.Context
	Logger        func(format string, v ...any)
}

func NewConnMananger(ctx context.Context, config *Config) *ConnManager {
	return &ConnManager{
		ChunkSupply:   make(chan *Chunk, 2000),
		Channels:      make([]*Channel, len(config.Channels)),
		Config:        config,
		ChunksToWrite: make(chan *Chunk, 100),
		pqueue:        []*Chunk{},
		receiveSignal: make(chan bool),
		ctx:           ctx,
		Logger: func(format string, v ...any) {
			log.Printf(format, v...)
		},
	}
}

func (cm *ConnManager) Close() {
	for _, c := range cm.Channels {
		c.Io.Close()
	}
}

func (cm *ConnManager) Start() *ConnManager {
	if cm.Config.MonitorPath != "" {
		go cm.startMonitor()
	}
	return cm
}

func (cm *ConnManager) startMonitor() {
	period, err := time.ParseDuration(cm.Config.MonitorTick)
	if err != nil {
		period = 20 * time.Second
	}

	dirPath := filepath.Dir(cm.Config.MonitorPath)
	err = os.MkdirAll(dirPath, os.ModePerm)
	if err == nil {
		// Write Monitor File
		for {
			select {
			case <-time.After(period):
				channels := ""
				for _, chl := range cm.Channels {
					channels += fmt.Sprintf("  - %v:\n    Transmission: %v\n",
						chl.Id, chl.ReceiveSpeed)
				}

				os.WriteFile(cm.Config.MonitorPath, []byte(channels), 0666)

			case <-cm.ctx.Done():
				return
			}
		}
	}
}

func (cm *ConnManager) calcSendLimit(chl *Channel) uint32 {
	/*speed := 0
	switch chl.Id {
	case 0:
		speed = 1
	case 1:
		speed = 20
	}
	minSpeed := 1
	return uint32(int(MIN_SEND_LIMIT) * speed / minSpeed)*/

	minSpeed := fp.Reduce(func(acc uint64, current *Channel) uint64 {
		if acc < current.SendSpeed {
			return acc
		} else {
			return current.SendSpeed
		}
	}, cm.Channels[0].SendSpeed)(cm.Channels)

	return uint32(MIN_SEND_LIMIT) * uint32(chl.SendSpeed) / uint32(minSpeed)
}

func (cm *ConnManager) Sender(iface io.ReadWriteCloser) {
	defer cm.Log("Shutdown send loop\n")
	cm.Log("Start send loop")

	// wrapped inc
	winc := func(cidx int) int {
		cidx++
		if cidx >= len(cm.Channels) {
			cidx = 0
		}
		return cidx
	}

	age := Wrapped(1)
	active := 0
	sendBytes := uint32(0)
	limit := MIN_SEND_LIMIT
	for {
		chunk := cm.AllocChunk()
		size, err := iface.Read(chunk.Data[:])
		switch err {
		case io.EOF:
			return
		case nil:
		default:
			cm.Log("Error reading packet %v\n", err)
			continue
		}
		chunk.Set(age, uint16(size))

		// cm.Log("Iface Read  %v\n", size)

		for i := 0; i < len(cm.Channels); i++ {
			chl := cm.Channels[active]
			if chl.Active() {
				if sendBytes == 0 {
					limit = cm.calcSendLimit(chl)
					startTime := time.Now()
					for j := 0; j < 3; j++ {
						chl.sendQueue <- StartBlock(age, startTime, limit)
					}
					/*cm.Log("Send Startblock %v(%v) %v b %v bps = %v",
					chl.Id, age, limit, chl.SendSpeed, float32(limit)/float32(chl.SendSpeed))*/
				}
				sendBytes += uint32(size)

				// cm.Log("Send chunk  %v(%v) %v %v < %v\n", chl.Id, age, size, sendBytes, limit)
				chl.sendQueue <- chunk
				if sendBytes >= limit {
					sendBytes = 0
					active = winc(active)
				}
				break
			}
			sendBytes = 0
			active = winc(active)
		}
		// chunk will be skipped if no active channel is available
		age = age.Inc()
	}
}

func (cm *ConnManager) Receiver(iface io.ReadWriteCloser) {
	nextAge := Wrapped(1)
	const TickTime = 5 * time.Millisecond
	timer := time.NewTimer(TickTime)
	timer.Stop()
	timerRunning := false

	for {
		select {
		case chunk := <-cm.ChunksToWrite:
			if len(cm.pqueue) == 0 && timerRunning {
				timer.Stop()
				timerRunning = false
			}
			idx := sort.Search(len(cm.pqueue), func(i int) bool {
				return chunk.Age.Less(cm.pqueue[i].Age)
			})
			cm.pqueue = append(cm.pqueue[:idx+1], cm.pqueue[idx:]...)
			cm.pqueue[idx] = chunk

		case <-timer.C:
			if len(cm.pqueue) > 0 {
				nextAge = cm.pqueue[0].Age
			}

		case <-cm.ctx.Done():
			return
		}

		cut := 0
		for i, c := range cm.pqueue {
			if c.Age != nextAge && !timerRunning {
				timerRunning = true
				timer.Reset(TickTime)
				break
			}
			nextAge = nextAge.Inc()
			cut = i + 1
			_, err := iface.Write(c.IPData())
			cm.FreeChunk(c)
			switch err {
			case io.EOF:
				return
			case nil:
			default:
				cm.Log("Error writing packet %v\n", err)
			}
		}
		cm.pqueue = cm.pqueue[cut:]
	}
}

func (cm *ConnManager) AllocChunk() *Chunk {
	if len(cm.ChunkSupply) == 0 {
		return &Chunk{
			Size: 0,
		}
	}
	return <-cm.ChunkSupply
}

func (cm *ConnManager) FreeChunk(chunk *Chunk) {
	cm.ChunkSupply <- chunk
}

func (cm *ConnManager) Log(format string, v ...any) {
	cm.Logger(format, v...)
}

/*
Parses an ip address or interface name to an ip4 address
*/
func ToIP(address string) (net.IP, error) {
	ip := net.ParseIP(address)
	if ip == nil {
		link, err := net.InterfaceByName(address)
		if err != nil {
			return nil, err
		}

		addrs, err := link.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			if ipv4Addr := addr.(*net.IPNet).IP.To4(); ipv4Addr != nil {
				return ipv4Addr, nil
			}
		}
		return nil, errors.New("network device has not ip4 address")
	}
	return ip, nil
}

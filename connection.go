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

	MIN_SEND_LIMIT = 2 * MTU
)

type Balancer interface {
	CalcSendLimit(chl *Channel, cm *ConnManager) int
}

/*
Distributes the IP packets according to the last measured channel speeds.
*/
type RelativeBalancer struct{}

func (b *RelativeBalancer) CalcSendLimit(chl *Channel, cm *ConnManager) int {
	minSpeed := fp.Reduce(func(speed float32, c *Channel) float32 {
		if speed < c.SendSpeed {
			return speed
		} else {
			return c.SendSpeed
		}
	}, cm.Channels[0].SendSpeed)(cm.Channels)
	return int(MIN_SEND_LIMIT * chl.SendSpeed / minSpeed)
}

/*
The fastest channel gets 90% of the IP packets all other 10%
*/
type PrioBalancer struct{}

func (b *PrioBalancer) CalcSendLimit(chl *Channel, cm *ConnManager) int {
	for _, c := range cm.Channels {
		if c.SendSpeed > chl.SendSpeed {
			return MIN_SEND_LIMIT
		}
	}
	return MIN_SEND_LIMIT * 9
}

/*
ConnManager is responsible for tunneling (sending and receiving) IP packets through
multiple channels.
*/
type ConnManager struct {
	Channels []*Channel
	Config   *Config
	Balancer Balancer

	ChunksToWrite chan *Chunk
	pqueue        []*Chunk
	ctx           context.Context
	Logger        func(format string, v ...any)
}

func NewConnMananger(ctx context.Context, config *Config) *ConnManager {
	var balancer Balancer
	switch config.Balancer {
	case "prio":
		balancer = &PrioBalancer{}

	default:
		balancer = &RelativeBalancer{}
	}

	return &ConnManager{
		Channels:      make([]*Channel, len(config.Channels)),
		Config:        config,
		Balancer:      balancer,
		ChunksToWrite: make(chan *Chunk, 50),
		pqueue:        []*Chunk{},
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
	sendBytes := 0
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
					limit = cm.Balancer.CalcSendLimit(chl, cm)
					//cm.Log("Send Block  %v: %v [%v, %v]\n", chl.Id, limit, cm.Channels[0].SendSpeed, cm.Channels[1].SendSpeed)
				}
				sendBytes += size

				//cm.Log("Send chunk  %v(%v) %v %v < %v\n", chl.Id, age, size, sendBytes, limit)
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

func (cm *ConnManager) Latencies() []time.Duration {
	return fp.Map(func(c *Channel) time.Duration { return c.Latency })(cm.Channels)
}

func (cm *ConnManager) QueueAges() []int {
	return fp.Map(func(c *Chunk) int { return int(c.Age) })(cm.pqueue)
}

func (cm *ConnManager) ReceiveSpeeds() []float32 {
	return fp.Map(func(c *Channel) float32 {
		return c.ReceiveSpeed * 8 / (1024 * 1024)
	})(cm.Channels)
}

func (cm *ConnManager) Receiver(iface io.ReadWriteCloser) {
	nextAge := Wrapped(1)
	const TICK_TIME = 5 * time.Microsecond
	timer := time.NewTimer(TICK_TIME)
	timer.Stop()
	timerRunning := false

	for {
		select {
		case chunk := <-cm.ChunksToWrite:
			if timerRunning && len(cm.pqueue) == 0 {
				timer.Stop()
				timerRunning = false
			}

			//cm.Log("Receive %v ?= %v %v", nextAge, chunk.Age, len(cm.pqueue))
			idx := sort.Search(len(cm.pqueue), func(i int) bool {
				return chunk.Age.Less(cm.pqueue[i].Age)
			})
			if idx < len(cm.pqueue) {
				cm.pqueue = append(cm.pqueue[:idx+1], cm.pqueue[idx:]...)
				cm.pqueue[idx] = chunk
			} else {
				cm.pqueue = append(cm.pqueue, chunk)
			}

		case <-timer.C:
			if len(cm.pqueue) > 0 {
				/*
					cm.Log("Correction Timer %v: %v %v", nextAge, cm.QueueAges(), cm.Latencies())
				*/
				nextAge = cm.pqueue[0].Age
			}
			timerRunning = false

		case <-cm.ctx.Done():
			return
		}
		cut := 0
	Outer:
		for i, c := range cm.pqueue {
			switch {
			case c.Age == nextAge:
				nextAge = nextAge.Inc()

			case c.Age.Less(nextAge):

			case nextAge.Less(c.Age):
				if !timerRunning {
					timerRunning = true

					maxLat := fp.Reduce(func(lat time.Duration, current *Channel) time.Duration {
						if lat > current.Latency {
							return lat
						} else {
							return current.Latency
						}
					}, TICK_TIME)(cm.Channels) * 2

					timer.Reset(maxLat)
					/*
						cm.Log("Start Correction Timer %v | %v != %v(%v): %v %v",
							maxLat, nextAge, c.Age, c.Size, cm.QueueAges(), cm.Latencies())
					*/
				}
				break Outer
			}
			cut = i + 1
			_, err := iface.Write(c.IPData())
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
	return &Chunk{Size: 0}
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

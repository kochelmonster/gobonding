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
	"time"

	"github.com/songgao/water"
)

const (
	// AppVersion contains current application version for -version command flag
	AppVersion = "0.2.0"
)

/*
Dispatches Chunks to Channels
*/
type ConnManager struct {
	ChunkSupply chan *Chunk
	Channels    []*Channel
	Config      *Config

	needSignal bool
	signal     chan bool
	heartbeat  time.Duration

	ctx context.Context
}

func NewConnMananger(ctx context.Context, config *Config) *ConnManager {
	heartbeat, err := time.ParseDuration(config.HeartBeatTime)
	if err != nil {
		heartbeat = 20 * time.Second
	}

	return &ConnManager{
		ChunkSupply: make(chan *Chunk, 2000),
		Channels:    make([]*Channel, len(config.Channels)),
		Config:      config,
		needSignal:  false,
		signal:      make(chan bool),
		heartbeat:   heartbeat,
		ctx:         ctx,
	}
}

func (cm *ConnManager) Close() {
	for _, c := range cm.Channels {
		c.io.Close()
	}
}

func (cm *ConnManager) Start() *ConnManager {
	if cm.Config.MonitorPath != "" {
		go cm.startMonitor()
	}
	return cm
}

func (cm *ConnManager) Sender(iface *water.Interface) {
	active := cm.selectChannel(0)
	chunk := Chunk{}
	changeId := Wrapped(1)

	for {
		chl := cm.Channels[active]
		limit := cm.calcSendLimit(active)
		log.Println("Active Sender", active, limit)

		sendBytes := 0
		for {
			size, err := iface.Read(chunk.Data[0:])
			switch err {
			case io.EOF:
				return
			case nil:
			default:
				log.Println("Error reading packet", err)
				continue
			}
			sendBytes += size
			chunk.Size = uint16(size)
			chl.Send(&chunk)

			if sendBytes <= limit {
				active := cm.selectChannel(active)
				chl.Send(ChangeChannel(uint16(active), changeId))
				chl.Send(ChangeChannel(uint16(active), changeId))
				changeId++
				break
			}
		}
	}
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
					channels += fmt.Sprintf("  - %v:\n    Send: %v\n    %v\n",
						chl.Id, chl.SendSpeed, chl.ReceiveSpeed)
				}

				os.WriteFile(cm.Config.MonitorPath, []byte(channels), 0666)

				// write
			case <-cm.ctx.Done():
				return
			}
		}
	}
}

func (cm *ConnManager) selectChannel(active int) int {
	size := len(cm.Channels)
	for {
		for j := 0; j < size; j++ {
			active++
			if active >= size {
				active = 0
			}
			chl := cm.Channels[active]
			if chl.Active() {
				return active
			}
		}

		cm.needSignal = true
		select {
		case <-cm.signal:
		case <-cm.ctx.Done():
		}
		for len(cm.signal) > 0 {
			<-cm.signal
		}
	}
}

func (cm *ConnManager) calcSendLimit(active int) int {
	speedSum := uint64(0)
	for _, c := range cm.Channels {
		speedSum += c.SendSpeed
	}
	return int(cm.Channels[active].SendSpeed * uint64(len(cm.Channels)) * MTU /
		speedSum)
}

func (cm *ConnManager) Receiver(iface *water.Interface) {
	timer := time.NewTimer(time.Second)
	active := cm.selectChannel(0)
	lastAge := Wrapped(0)
	for {
		chl := cm.Channels[active]
		timer.Stop()
		timer.Reset(time.Second)
		select {
		case msg := <-chl.ReceiveChl:
			switch msg := msg.(type) {
			case *Chunk:
				_, err := iface.Write(msg.Data[:msg.Size])
				if err != nil {
					log.Println("Error writing packet", err)
				}
				cm.FreeChunk(msg)

			case *ChangeMsg:
				if msg.Age == lastAge || msg.Age.Less(lastAge) {
					continue
				}
				lastAge = msg.Age
				log.Println("Active Receiver", active)
				active = int(msg.NextChannelId)
			}
		case <-timer.C:
			if !chl.Active() {
				active = cm.selectChannel(active)
			}
		}
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

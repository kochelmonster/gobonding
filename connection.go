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
	"sync"
	"time"
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

	activeCond *sync.Cond
	// true if at least one channel is active
	active bool

	heartbeat time.Duration

	ctx    context.Context
	Logger func(format string, v ...any)
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
		activeCond:  sync.NewCond(&sync.Mutex{}),
		active:      false,
		heartbeat:   heartbeat,
		ctx:         ctx,
		Logger: func(format string, v ...any) {
			log.Printf(format, v...)
		},
	}
}

func (cm *ConnManager) Close() {
	for _, c := range cm.Channels {
		c.Io.Close()
	}
	cm.setActive()
}

func (cm *ConnManager) setActive() bool {
	cm.activeCond.L.Lock()
	defer cm.activeCond.L.Unlock()
	before := cm.active
	cm.active = true
	cm.activeCond.Broadcast()
	return before
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
					channels += fmt.Sprintf("  - %v:\n    Send: %v\n    %v\n",
						chl.Id, chl.SendSpeed, chl.ReceiveSpeed)
				}

				os.WriteFile(cm.Config.MonitorPath, []byte(channels), 0666)

			case <-cm.ctx.Done():
				return
			}
		}
	}
}

func (cm *ConnManager) selectChannel(active int) (int, bool) {
	size := len(cm.Channels)
	activated := false
	for {
		for j := 0; j < size; j++ {
			active++
			if active >= size {
				active = 0
			}
			chl := cm.Channels[active]
			if chl.Active() {
				return active, activated
			}
		}

		cm.Log("No active sender -> wait for signal\n")
		cm.activeCond.L.Lock()
		if cm.ctx.Err() != nil {
			return 0, activated
		}
		cm.active = false
		defer cm.activeCond.L.Unlock()
		for !cm.active {
			cm.activeCond.Wait()
			if cm.ctx.Err() != nil {
				return 0, activated
			}
			activated = true
		}
		cm.Log("Sender activated\n")
	}
}

func (cm *ConnManager) calcSendLimit(active int) int {
	speedSum := uint64(0)
	for _, c := range cm.Channels {
		speedSum += c.SendSpeed
	}

	if speedSum == 0 {
		return MTU
	}
	sumMTU := uint64(len(cm.Channels) * MTU)
	return int(cm.Channels[active].SendSpeed * sumMTU / speedSum)
}

func (cm *ConnManager) Sender(iface io.ReadWriteCloser) {
	defer cm.Log("done connection sender\n")

	cm.Log("Start sender\n")
	chunk := Chunk{}

	var activated bool
	cm.Log("Send Start Channel")
	active, _ := cm.selectChannel(0)
	age := Wrapped(0)

	sendchange := func() {
		cm.Log("SendChannelChange %v(%v)", active, age)
		for _, chl := range cm.Channels {
			chl.Send(ChangeChannel(uint16(active), age))
		}
		age++
	}

	sendchange()

	cm.Log("Start sender loop %v\n", active)
	for {
		chl := cm.Channels[active]
		limit := cm.calcSendLimit(active)
		cm.Log("Active Sender %v %v %v\n", active, limit, MTU)

		sendBytes := 0
		for {
			size, err := iface.Read(chunk.Data[0:])
			switch err {
			case io.EOF:
				return
			case nil:
			default:
				cm.Log("Error reading packet %v\n", err)
				continue
			}
			sendBytes += size
			chunk.Size = uint16(size)
			cm.Log("Iface Read  %v %v %v %v\n", chl.Id, sendBytes, limit, size)
			chl.Send(&chunk)

			if sendBytes >= limit {
				cm.Log("Send Limit exceeded %v >= %v", sendBytes, limit)
				active, activated = cm.selectChannel(active)
				if activated {
					age = 0
				}
				sendchange()
				sendBytes = 0
				break
			}
		}
	}
}

func (cm *ConnManager) Receiver(iface io.ReadWriteCloser) {
	defer cm.Log("done connection receiver\n")

	timer := time.NewTimer(time.Second)
	active, _ := cm.selectChannel(0)
	lastAge := Wrapped(0)
	var activated bool
	cm.Log("Start receive %v\n", active)
	for {
		cm.Log("Active Receiver %v\n", active)
		chl := cm.Channels[active]
		timer.Stop()
		timer.Reset(time.Second)
		select {
		case msg := <-chl.ReceiveChl:
			switch msg := msg.(type) {
			case *Chunk:
				cm.Log("Iface Write %v %v %v\n", chl.Id, msg.Size, msg)
				_, err := iface.Write(msg.Data[:msg.Size])
				if err != nil {
					cm.Log("Error writing packet %v\n", err)
				}
				cm.FreeChunk(msg)

			case *ChangeMsg:
				cm.Log("Received change Message %v -> %v at (%v %v)\n",
					active, msg.NextChannelId, msg.Age, lastAge)

				if msg.Age == lastAge || msg.Age.Less(lastAge) {
					continue
				}
				lastAge = msg.Age
				active = int(msg.NextChannelId)
			}
		case <-timer.C:
			if !chl.Active() {
				cm.Log("Receiver not active")
				active, activated = cm.selectChannel(active)
				if activated {
					lastAge = Wrapped(0)
				}
			} else {
				size := len(cm.Channels)
				test := active
				for j := 0; j < size; j++ {
					test++
					if test >= size {
						test = 0
					}
					if len(cm.Channels[test].ReceiveChl) > 0 {
						cm.Log("Change Active Recveiver chanel %v -> %v", active, test)
						active = test
						break
					}
				}
			}
		case <-cm.ctx.Done():
			return
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

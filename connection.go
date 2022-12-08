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
)

const (
	// AppVersion contains current application version for -version command flag
	AppVersion = "0.2.0"

	AVG_SEND_LIMIT = 10 * MTU
)

/*
Dispatches Chunks to Channels
*/
type ConnManager struct {
	ChunkSupply chan *Chunk
	Channels    []*Channel
	Config      *Config

	activeReceiver uint16
	receiveChunks  chan *Chunk
	heartbeat      time.Duration
	ctx            context.Context
	Logger         func(format string, v ...any)
}

func NewConnMananger(ctx context.Context, config *Config) *ConnManager {
	heartbeat, err := time.ParseDuration(config.HeartBeatTime)
	if err != nil {
		heartbeat = 20 * time.Second
	}

	return &ConnManager{
		ChunkSupply:    make(chan *Chunk, 2000),
		Channels:       make([]*Channel, len(config.Channels)),
		Config:         config,
		activeReceiver: 0,
		receiveChunks:  make(chan *Chunk),
		heartbeat:      heartbeat,
		ctx:            ctx,
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

func (cm *ConnManager) newStartBlock(chl *Channel) bool {
	oldestChl := cm.Channels[0]
	for _, chl := range cm.Channels[1:] {
		if chl.Age.Less(oldestChl.Age) {
			oldestChl = chl
		}
	}
	cm.activeReceiver = oldestChl.Id
	// cm.Log("Activate receive block %v %v\n", oldestChl.Id, oldestChl.Age)
	if oldestChl.Age == 0 {
		return true
	}

	if cm.activeReceiver != chl.Id {
		select {
		case oldestChl.startSignal <- true:
			return true
		case <-cm.ctx.Done():
		}
	}
	return false
}

func (cm *ConnManager) calcSendLimit(chl *Channel) int {
	speedSum := uint64(0)
	for _, c := range cm.Channels {
		speedSum += c.SendSpeed
	}

	if speedSum == 0 {
		return AVG_SEND_LIMIT
	}
	sumLimit := uint64(len(cm.Channels) * AVG_SEND_LIMIT)
	return int(chl.SendSpeed * sumLimit / speedSum)
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

	chunk := Chunk{}
	age := Wrapped(0)
	active := 0
	sendBytes := 0
	limit := AVG_SEND_LIMIT
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
		chunk.Size = uint16(size)
		// cm.Log("Iface Read  %v\n", size)

		for i := 0; i < len(cm.Channels); i++ {
			chl := cm.Channels[active]
			if chl.Active() {
				if sendBytes == 0 {
					(&age).Inc()
					// cm.Log("Send Startblock %v %v", chl.Id, age)
					for j := 0; j < 3; j++ {
						chl.Send(StartBlock(age))
					}
					limit = cm.calcSendLimit(chl)
				}
				sendBytes += size

				// cm.Log("Transfer chunk  %v %v %v %v\n", chl.Id, size, sendBytes, limit)
				size = 0
				chl.Send(&chunk)
				if sendBytes > limit {
					// cm.Log("Send Stopblock %v", chl.Id)
					for j := 0; j < 3; j++ {
						chl.Send(StartBlock(0))
					}
					sendBytes = 0
					active = winc(active)
				}
				break
			}
			sendBytes = 0
			active = winc(active)
		}
		if size != 0 {
			cm.Log("Skipped message!!!")
		}
		// chunk will be skipped if no active channel is available
	}
}

func (cm *ConnManager) Receiver(iface io.ReadWriteCloser) {
	defer cm.Log("Shutdown receive loop\n")
	cm.Log("Start receive loop\n")

	for {
		select {
		case chunk := <-cm.receiveChunks:
			// cm.Log("Iface Write %v\n", chunk)
			_, err := iface.Write(chunk.Data[:chunk.Size])
			if err != nil {
				cm.Log("Error writing packet %v\n", err)
			}
			cm.FreeChunk(chunk)

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

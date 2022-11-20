package gobonding

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// AppVersion contains current application version for -version command flag
	AppVersion = "0.1.0"

	monitorTemplate = `download_speed: %[1]vBPS
upload_speed: %[2]vBPS
max_download_speed: %[3]vBPS
max_upload_speed: %[4]vBPS
active_channels:
%[5]v
`
)

/*
Dispatches Chunks to Channels
*/
type ConnManager struct {
	mu sync.Mutex

	// Dispatches chunks on Proxy Channels
	DispatchChannel chan *Chunk

	// Receives chunk of Proxy Channels
	CollectChannel chan *Chunk

	ChunkSupply []*Chunk

	ActiveChannels map[string]*channel

	UploadBytes   int
	DownloadBytes int

	heartbeat time.Duration
}

type channel struct {
	lastPing     time.Time
	signal       chan bool
	addr         net.Addr
	sendCount    int
	receiveCount int
}

func NewConnMananger(ctx context.Context, config *Config) *ConnManager {
	heartbeat, err := time.ParseDuration(config.HeartBeatTime)
	if err != nil {
		heartbeat = 2 * time.Second
	}

	result := &ConnManager{
		DispatchChannel: make(chan *Chunk, 10),
		CollectChannel:  make(chan *Chunk, 10),
		ChunkSupply:     make([]*Chunk, 0),
		ActiveChannels:  make(map[string]*channel),
		UploadBytes:     0,
		DownloadBytes:   0,
		heartbeat:       heartbeat,
	}

	if config.MonitorPath != "" {
		period, err := time.ParseDuration(config.MonitorTick)
		if err != nil {
			period = 20 * time.Second
		}

		dirPath := filepath.Dir(config.MonitorPath)
		err = os.MkdirAll(dirPath, os.ModePerm)
		if err == nil {
			go func() {
				ts := time.Now()
				maxDSpeed := float64(0)
				maxUSpeed := float64(0)

				// Write Monitor File
				for {
					select {
					case <-time.After(period):
						duration := float64(time.Since(ts) / time.Second)
						dSpeed := float64(result.DownloadBytes) / duration
						uSpeed := float64(result.UploadBytes) / duration

						maxDSpeed = math.Max(dSpeed, maxDSpeed)
						maxUSpeed = math.Max(uSpeed, maxUSpeed)

						result.DownloadBytes = 0
						result.UploadBytes = 0
						ts = time.Now()

						channels := ""
						for id, chl := range result.ActiveChannels {
							if result.IsActive(chl) {
								channels += fmt.Sprintf("  - %v(%v/%v)\n",
									id, chl.sendCount, chl.receiveCount)
							}
						}

						text := fmt.Sprintf(monitorTemplate, int(dSpeed), int(uSpeed), int(maxDSpeed),
							int(maxUSpeed), channels)

						os.WriteFile(config.MonitorPath, []byte(text), 0666)

						// write
					case <-ctx.Done():
						return
					}
				}
			}()
		}
	}

	return result
}

func (cm *ConnManager) Close() {
	for _, c := range cm.ActiveChannels {
		close(c.signal)
	}
	close(cm.DispatchChannel)
}

// Starts tracking connections
func (cm *ConnManager) SendPings(ctx context.Context, conn WriteConnection) {
	timer := time.NewTicker(cm.heartbeat)
	for {
		select {
		case <-timer.C:
			for _, chl := range cm.ActiveChannels {
				(&PingMessage{MsgBase{chl.addr}}).Write(conn)
			}

		case <-ctx.Done():
			return
		}
	}
}

func (cm *ConnManager) AllocChunk() *Chunk {
	if len(cm.ChunkSupply) > 0 {
		cm.mu.Lock()
		defer cm.mu.Unlock()
		if len(cm.ChunkSupply) > 0 {
			result := cm.ChunkSupply[len(cm.ChunkSupply)-1]
			cm.ChunkSupply = cm.ChunkSupply[:len(cm.ChunkSupply)-1]
			return result
		}
	}
	return &Chunk{
		Size: 0,
	}
}

func (cm *ConnManager) FreeChunk(chunk *Chunk) {
	cm.mu.Lock()
	cm.ChunkSupply = append(cm.ChunkSupply, chunk)
	cm.mu.Unlock()
}

func (cm *ConnManager) IsActive(c *channel) bool {
	return time.Since(c.lastPing) < 2*cm.heartbeat
}

func (cm *ConnManager) AddChannel(addr net.Addr, conn WriteConnection) {
	signal := make(chan bool, 1)
	chl := &channel{
		lastPing:     time.Now(),
		signal:       signal,
		addr:         addr,
		sendCount:    0,
		receiveCount: 0,
	}
	key := addr.(*net.UDPAddr).IP.String()
	{
		cm.mu.Lock()
		defer cm.mu.Unlock()
		cm.ActiveChannels[key] = chl
	}
	log.Println("AddChannel", key)

	go func() {
		sendCount := 0
		for {
			if cm.IsActive(chl) {
				timer := time.NewTimer(cm.heartbeat)
				select {
				case chunk, more := <-cm.DispatchChannel:
					if !more {
						return
					}

					if sendCount >= 10 {
						if !cm.waitForAck(chl, conn, "msg") {
							return
						}
						sendCount = 0
					}

					chunk.SetRouterAddr(addr)
					log.Println("Send", addr, sendCount, chunk)
					chunk.Write(conn)
					cm.UploadBytes += int(chunk.Size)

					chl.sendCount++
					sendCount++
				case <-timer.C:
					cm.waitForAck(chl, conn, "timeout")
				}
			} else if !cm.waitForAck(chl, conn, "inactive") {
				return
			}
		}
	}()
}

func (cm *ConnManager) RequestAck(addr net.Addr, conn WriteConnection) {
	(&RequestAckMsg{MsgBase{Addr: addr}}).Write(conn)
}

func (cm *ConnManager) WaitForAck(addr net.Addr, conn WriteConnection) bool {
	key := addr.(*net.UDPAddr).IP.String()
	if c, ok := cm.ActiveChannels[key]; ok {
		return cm.waitForAck(c, conn, "start")
	}
	return false
}

func (cm *ConnManager) waitForAck(c *channel, conn WriteConnection, cause string) bool {
	log.Println("Wait For Ack", c.addr, cause)
	cm.RequestAck(c.addr, conn)

	ticker := time.NewTicker(2 * cm.heartbeat)
	for {
		select {
		case _, more := <-c.signal:
			return more
		case <-ticker.C:
			cm.RequestAck(c.addr, conn)
		}
	}
}

func (cm *ConnManager) GotAck(addr net.Addr) {
	key := addr.(*net.UDPAddr).IP.String()
	if c, ok := cm.ActiveChannels[key]; ok {
		delay := time.Since(c.lastPing)
		if delay > 2*cm.heartbeat {
			log.Println("Channel Reconnected", key, delay)
		}
		c.lastPing = time.Now()
		if len(c.signal) == 0 {
			// log.Println("GotAck", addr)
			c.signal <- true
		}
	}
}

func (cm *ConnManager) ReceivedChunk(addr net.Addr, chunk *Chunk) {
	key := addr.(*net.UDPAddr).IP.String()
	if c, ok := cm.ActiveChannels[key]; ok {
		c.receiveCount++
	}
	cm.DownloadBytes += int(chunk.Size)
}

func (cm *ConnManager) SendAck(addr net.Addr, conn WriteConnection) {
	// log.Println("SendAck", addr)
	key := addr.(*net.UDPAddr).IP.String()
	if c, ok := cm.ActiveChannels[key]; ok {
		c.addr = addr
	} else {
		cm.AddChannel(addr, conn)
	}

	(&AckMessage{MsgBase{Addr: addr}}).Write(conn)
}

func (cm *ConnManager) PurgeChannels(ctx context.Context) {
	timer := time.NewTicker(20 * time.Minute)
	for {
		select {
		case <-timer.C:
			for key, chl := range cm.ActiveChannels {
				if time.Since(chl.lastPing) > 10*time.Minute {
					delete(cm.ActiveChannels, key)
				}
			}

		case <-ctx.Done():
			return
		}
	}
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

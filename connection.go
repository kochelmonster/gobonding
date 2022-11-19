package gobonding

import (
	"context"
	"errors"
	"fmt"
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
min_download_speed: %[3]vBPS
min_upload_speed: %[4]vBPS
max_download_speed: %[5]vBPS
max_upload_speed: %[6]vBPS
active_channels:
%[7]v
`
)

/*
Dispatches Chunks to Channels
*/
type ConnManager struct {
	mu sync.Mutex

	// Dispatches chunks on Proxy Channels
	DispatchChannel chan Message

	// Receives chunk of Proxy Channels
	CollectChannel chan *Chunk

	ChunkSupply []*Chunk

	ActiveChannels map[net.Addr]*channel

	UploadBytes   int
	DownloadBytes int

	heartbeat time.Duration
}

type channel struct {
	lastPing time.Time
	signal   chan bool
}

func NewConnMananger(ctx context.Context, config *Config) *ConnManager {
	heartbeat, err := time.ParseDuration(config.HeartBeatTime)
	if err != nil {
		heartbeat = 2 * time.Second
	}

	result := &ConnManager{
		DispatchChannel: make(chan Message, 10),
		CollectChannel:  make(chan *Chunk, 10),
		ChunkSupply:     make([]*Chunk, 0),
		ActiveChannels:  make(map[net.Addr]*channel),
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
				minDSpeed := math.MaxFloat64
				minUSpeed := math.MaxFloat64

				// Write Monitor File
				for {
					select {
					case <-time.After(period):
						duration := time.Since(ts)
						dSpeed := float64(result.DownloadBytes) / float64(duration)
						uSpeed := float64(result.UploadBytes) / float64(duration)

						maxDSpeed = math.Max(dSpeed, maxDSpeed)
						maxUSpeed = math.Max(uSpeed, maxUSpeed)

						minDSpeed = math.Min(dSpeed, minDSpeed)
						minUSpeed = math.Min(uSpeed, minUSpeed)

						result.DownloadBytes = 0
						result.UploadBytes = 0

						channels := ""
						for id := range result.ActiveChannels {
							channels += "  - " + id.String()
						}

						text := fmt.Sprintf(monitorTemplate, int(dSpeed), int(uSpeed), int(maxDSpeed),
							int(maxUSpeed), int(minDSpeed), int(minUSpeed), channels)

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

func (cm *ConnManager) CountUpload(chunk *Chunk) *Chunk {
	cm.UploadBytes += int(chunk.Size)
	return chunk
}

func (cm *ConnManager) CountDownload(chunk *Chunk) *Chunk {
	cm.DownloadBytes += int(chunk.Size)
	return chunk
}

func (cm *ConnManager) Close() {
	for _, c := range cm.ActiveChannels {
		close(c.signal)
	}
	close(cm.DispatchChannel)
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

func (cm *ConnManager) AddChannel(addr net.Addr) chan bool {
	signal := make(chan bool)
	chl := channel{
		lastPing: time.Now(),
		signal:   signal,
	}
	cm.ActiveChannels[addr] = &chl
	return signal
}

// Updates the lastPing Time or creates a channel if a message comes in
func (cm *ConnManager) UpdateChannel(addr net.Addr, conn WriteConnection) {
	if c, ok := cm.ActiveChannels[addr]; ok {
		c.lastPing = time.Now()
		go func() { c.signal <- true }()
	} else {
		signal := cm.AddChannel(addr)

		go func() {
			for {
				if cm.IsActive(c) {
					select {
					case msg, more := <-cm.DispatchChannel:
						if !more {
							return
						}
						if cm.IsActive(c) {
							msg.Write(conn)
						} else {
							cm.DispatchChannel <- msg
						}
					case _, more := <-signal:
						if !more {
							return
						}
					}
				} else {
					// wait for next ping
					_, more := <-signal
					if !more {
						return
					}
				}
			}
		}()
	}
}

// Starts tracking connections
func (cm *ConnManager) SendPings(ctx context.Context, conn WriteConnection) {
	timer := time.NewTimer(cm.heartbeat)
	for {
		select {
		case <-timer.C:
			for addr := range cm.ActiveChannels {
				(&PingMessage{Addr: addr}).Write(conn)
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

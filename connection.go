package gobonding

import (
	"container/heap"
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

	// Receives unordered chunk of Proxy Channels
	CollectChannel chan *Chunk

	// Outputs ordered chunks
	OrderedChannel chan *Chunk

	Queue PriorityQueue

	ChunkSupply []*Chunk

	PeerOrder  uint16
	LocalOrder uint16

	ActiveChannels map[string]bool

	UploadBytes   int
	DownloadBytes int
}

func NewConnMananger(ctx context.Context, config *Config) *ConnManager {
	result := &ConnManager{
		DispatchChannel: make(chan Message, 10),
		CollectChannel:  make(chan *Chunk, 10),
		OrderedChannel:  make(chan *Chunk, 10),
		Queue:           NewQueue(),
		ChunkSupply:     make([]*Chunk, 0),
		PeerOrder:       0,
		LocalOrder:      0,
		ActiveChannels:  make(map[string]bool),
		UploadBytes:     0,
		DownloadBytes:   0,
	}

	ticker := time.NewTicker(time.Second)
	ticker.Stop()

	emptyQueue := func() {
		defer func() {
			if len(result.Queue) == 0 {
				ticker.Stop()
			}
		}()
		for len(result.Queue) > 0 {
			peek := result.Queue[0]
			switch {
			case peek.Idx == result.PeerOrder:
				chunk := heap.Pop(&result.Queue).(*Chunk)
				result.OrderedChannel <- chunk
				result.PeerOrder++

			case peek.Idx > result.PeerOrder:
				return
			}
			// peek.Idx < result.PeerOrder: skip
		}
	}

	go func() {
		// Sorts chunks that came not in order
		for {
			select {
			case chunk := <-result.CollectChannel:
				if chunk.Idx == result.PeerOrder {
					result.OrderedChannel <- chunk
					result.PeerOrder++
				} else {
					heap.Push(&result.Queue, chunk)
					if len(result.Queue) == 1 {
						// optimation emptyQueue is not necessary
						ticker.Reset(time.Second)
						continue
					}
				}
				emptyQueue()

			case <-ticker.C:
				// Should never happen: a missing ip package
				if len(result.Queue) > 0 {
					min := result.Queue[0]
					log.Println("Skip Packet", min.Idx, result.PeerOrder, len(result.Queue))
					result.PeerOrder = min.Idx
					emptyQueue()
				}

			case <-ctx.Done():
				return
			}

		}
	}()

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
						duration := float64(time.Since(ts)) / float64(time.Second)
						dSpeed := float64(result.DownloadBytes) / duration
						uSpeed := float64(result.UploadBytes) / duration

						maxDSpeed = math.Max(dSpeed, maxDSpeed)
						maxUSpeed = math.Max(uSpeed, maxUSpeed)

						minDSpeed = math.Min(dSpeed, minDSpeed)
						minUSpeed = math.Min(uSpeed, minUSpeed)

						result.DownloadBytes = 0
						result.UploadBytes = 0
						ts = time.Now()

						channels := ""
						for id := range result.ActiveChannels {
							channels += "  - " + id + "\n"
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

func (cm *ConnManager) Clear() {
	cm.Queue = cm.Queue[:0]
	for len(cm.DispatchChannel) > 0 {
		<-cm.DispatchChannel
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
		Idx:  0,
		Size: 0,
	}
}

func (cm *ConnManager) FreeChunk(chunk *Chunk) {
	cm.mu.Lock()
	cm.ChunkSupply = append(cm.ChunkSupply, chunk)
	cm.mu.Unlock()
}

func (cm *ConnManager) SyncCounter() {
	cm.LocalOrder = 0
	cm.PeerOrder = 0
	cm.Clear()
	cm.DispatchChannel <- &SyncOrderMsg{
		Order: cm.LocalOrder,
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

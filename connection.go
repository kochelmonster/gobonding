package gobonding

import (
	"container/heap"
	"context"
	"errors"
	"net"
	"sync"
)

const (
	// AppVersion contains current application version for -version command flag
	AppVersion = "0.1.0a"
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

	MsgOrder uint16

	ActiveChannels int
}

func NewConnMananger(ctx context.Context, config *Config) *ConnManager {
	queue := make(PriorityQueue, 0)
	heap.Init(&queue)

	result := &ConnManager{
		DispatchChannel: make(chan Message, 10),
		CollectChannel:  make(chan *Chunk, 10),
		OrderedChannel:  make(chan *Chunk, 10),
		ChunkSupply:     make([]*Chunk, 0),
		Queue:           queue,
		MsgOrder:        0,
		ActiveChannels:  0,
	}
	go func() {
		for {
			select {
			case chunk := <-result.CollectChannel:
				if chunk.Idx == result.MsgOrder {
					result.OrderedChannel <- chunk
					result.MsgOrder++
				} else {
					heap.Push(&result.Queue, chunk)
					if len(result.Queue) > config.OrderWindow {
						result.MsgOrder = result.Queue[len(result.Queue)-1].Idx
					}
				}
				for len(result.Queue) > 0 && result.Queue[len(result.Queue)-1].Idx == result.MsgOrder {
					chunk = heap.Pop(&result.Queue).(*Chunk)
					result.OrderedChannel <- chunk
					result.MsgOrder++
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	return result
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
	cm.DispatchChannel <- &SyncOrderMsg{
		Order: cm.MsgOrder,
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

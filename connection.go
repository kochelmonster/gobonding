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

	PeerOrder  uint16
	LocalOrder uint16

	ActiveChannels int
}

func NewConnMananger(ctx context.Context, config *Config) *ConnManager {
	result := &ConnManager{
		DispatchChannel: make(chan Message, 10),
		CollectChannel:  make(chan *Chunk, 10),
		OrderedChannel:  make(chan *Chunk, 10),
		ChunkSupply:     make([]*Chunk, 0),
		Queue:           NewQueue(),
		PeerOrder:       0,
		LocalOrder:      0,
		ActiveChannels:  0,
	}
	go func() {
		for {
			select {
			case chunk := <-result.CollectChannel:
				if chunk.Idx == result.PeerOrder {
					result.OrderedChannel <- chunk
					result.PeerOrder++
				} else {
					heap.Push(&result.Queue, chunk)
					if len(result.Queue) > config.OrderWindow {
						// Should never happen: a missing ip package
						min := heap.Pop(&result.Queue).(*Chunk)
						heap.Push(&result.Queue, min)
						result.PeerOrder = min.Idx
					}
				}
			StopFill:
				for len(result.Queue) > 0 {
					chunk = heap.Pop(&result.Queue).(*Chunk)
					switch {
					case chunk.Idx == result.PeerOrder:
						result.OrderedChannel <- chunk
						result.PeerOrder++

					case chunk.Idx > result.PeerOrder:
						heap.Push(&result.Queue, chunk)
						break StopFill
					}
					// chunk.Idx < result.PeerOrder: skip
				}

			case <-ctx.Done():
				return
			}

		}
	}()

	return result
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

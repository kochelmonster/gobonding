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
	DispatchChannel chan *Chunk

	// Receives unordered chunk of Proxy Channels
	CollectChannel chan *Chunk

	// Outputs ordered chunks
	OrderedChannel chan *Chunk

	Queue PriorityQueue

	ChunkSupply []*Chunk
}

func NewConnMananger(ctx context.Context, config *Config) *ConnManager {
	queue := make(PriorityQueue, 0)
	heap.Init(&queue)

	result := &ConnManager{
		DispatchChannel: make(chan *Chunk, 10),
		CollectChannel:  make(chan *Chunk, 10),
		OrderedChannel:  make(chan *Chunk, 10),
		ChunkSupply:     make([]*Chunk, 10),
		Queue:           queue,
	}
	go func() {
		var order uint16 = 0
		for {
			select {
			case chunk := <-result.CollectChannel:
				if chunk.Idx == order {
					result.OrderedChannel <- chunk
					order++
				} else {
					heap.Push(&result.Queue, chunk)
					if len(result.Queue) > config.OrderWindow {
						order = result.Queue[len(result.Queue)-1].Idx
					}
				}
				for len(result.Queue) > 0 && result.Queue[len(result.Queue)-1].Idx == order {
					chunk = heap.Pop(&result.Queue).(*Chunk)
					result.OrderedChannel <- chunk
					order++
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	return result
}

func (d *ConnManager) AllocChunk() *Chunk {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.ChunkSupply) > 0 {
		result := d.ChunkSupply[len(d.ChunkSupply)-1]
		d.ChunkSupply = d.ChunkSupply[:len(d.ChunkSupply)-1]
		return result
	}
	return &Chunk{
		Idx:  0,
		Size: 0,
	}
}

func (d *ConnManager) FreeChunk(chunk *Chunk) {
	d.mu.Lock()
	d.ChunkSupply = append(d.ChunkSupply, chunk)
	d.mu.Unlock()
}

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

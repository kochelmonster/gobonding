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

	// Outputs ordered chunks
	OrderedChannel chan *Chunk
	Queue          PriorityQueue

	ChunkSupply []*Chunk

	Channels map[string]*channel

	UploadBytes   int
	DownloadBytes int

	rcount    int
	heartbeat time.Duration
	config    *Config
}

type channel struct {
	id            uint16
	signal        chan bool
	SendChl       chan Message
	addr          net.Addr
	sendCount     int
	receiveCount  int
	receiveWeight int
	sendWeight    int
}

func NewConnMananger(config *Config) *ConnManager {
	heartbeat, err := time.ParseDuration(config.HeartBeatTime)
	if err != nil {
		heartbeat = 20 * time.Second
	}

	result := &ConnManager{
		mu:              sync.Mutex{},
		DispatchChannel: make(chan *Chunk, 10),
		CollectChannel:  make(chan *Chunk, 10),
		OrderedChannel:  make(chan *Chunk, 10),
		Queue:           NewQueue(),
		ChunkSupply:     make([]*Chunk, 0),
		Channels:        make(map[string]*channel),
		UploadBytes:     0,
		DownloadBytes:   0,
		rcount:          0,
		heartbeat:       heartbeat,
		config:          config,
	}

	return result
}

func (cm *ConnManager) Start(ctx context.Context) *ConnManager {
	go cm.startBalancer(ctx)
	go cm.startDispatcher(ctx)
	go cm.startSorter(ctx)

	if cm.config.MonitorPath != "" {
		go cm.startMonitor(ctx, cm.config)
	}
	return cm
}

func (cm *ConnManager) Close() {
	for _, c := range cm.Channels {
		close(c.signal)
	}
	close(cm.DispatchChannel)
}

func (cm *ConnManager) startDispatcher(ctx context.Context) {
	for {
		for _, c := range cm.Channels {
			amount := 0
			for amount < c.sendWeight {
				select {
				case chunk := <-cm.DispatchChannel:
					c.SendChl <- chunk
					amount += int(chunk.Size)

				case <-ctx.Done():
					return
				}
			}
		}
	}
}

func (cm *ConnManager) startMonitor(ctx context.Context, config *Config) {
	period, err := time.ParseDuration(config.MonitorTick)
	if err != nil {
		period = 20 * time.Second
	}

	dirPath := filepath.Dir(config.MonitorPath)
	err = os.MkdirAll(dirPath, os.ModePerm)
	if err == nil {
		ts := time.Now()
		maxDSpeed := float64(0)
		maxUSpeed := float64(0)

		// Write Monitor File
		for {
			select {
			case <-time.After(period):
				duration := float64(time.Since(ts) / time.Second)
				dSpeed := float64(cm.DownloadBytes) / duration
				uSpeed := float64(cm.UploadBytes) / duration

				maxDSpeed = math.Max(dSpeed, maxDSpeed)
				maxUSpeed = math.Max(uSpeed, maxUSpeed)

				cm.DownloadBytes = 0
				cm.UploadBytes = 0
				ts = time.Now()

				channels := ""
				for id, chl := range cm.Channels {
					channels += fmt.Sprintf("  - %v(%v/%v)\n",
						id, chl.sendCount, chl.receiveCount)
				}

				text := fmt.Sprintf(monitorTemplate, int(dSpeed), int(uSpeed), int(maxDSpeed),
					int(maxUSpeed), channels)

				os.WriteFile(config.MonitorPath, []byte(text), 0666)

				// write
			case <-ctx.Done():
				return
			}
		}
	}
}

func (cm *ConnManager) sendFirst(ctx context.Context) uint16 {
	chunk := heap.Pop(&cm.Queue).(*Chunk)
	select {
	case cm.OrderedChannel <- chunk:
		return chunk.Idx + 1
	case <-ctx.Done():
		return 0
	}
}

func (cm *ConnManager) sendQueue(ctx context.Context, order uint16) uint16 {
	for len(cm.Queue) > 0 {
		if ctx.Err() != nil {
			return order
		}

		peek := cm.Queue[0]
		switch {
		case peek.Idx <= order:
			order = cm.sendFirst(ctx)

		default: // peek.Idx > order
			return order
		}
	}
	return order
}

func (cm *ConnManager) startBalancer(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-ticker.C:
			if cm.rcount > 100 {
				sum := 0
				for _, c := range cm.Channels {
					sum += int(c.receiveWeight)
				}

				log.Println("Balancer", sum)
				for _, c := range cm.Channels {
					weight := uint16(int(c.receiveWeight*100) / sum)
					if c.receiveWeight != 0 && weight == 0 {
						weight = 1
					}
					log.Println("Send Weight", c.addr, c.receiveWeight, weight)
					c.SendChl <- &WeightMsg{
						MsgBase: MsgBase{Addr: c.addr},
						Weight:  weight,
					}
					c.receiveWeight = 0
				}
			}
			cm.rcount = 0

		case <-ctx.Done():
			return
		}
	}

}

func (cm *ConnManager) startSorter(ctx context.Context) {
	// Sorts chunks that came not in order
	ticker := time.NewTicker(time.Second)
	ticker.Stop()
	tickerActive := false
	order := uint16(0)
	for {
		if ctx.Err() != nil {
			return
		}
		if tickerActive && len(cm.Queue) == 0 {
			ticker.Stop()
			tickerActive = false
		}

		select {
		case chunk := <-cm.CollectChannel:
			//log.Println("Received unordered", chunk.Idx, order)
			if chunk.Idx == order {
				cm.OrderedChannel <- chunk
				order++
				if len(cm.Queue) > 0 {
					order = cm.sendQueue(ctx, order)
					if tickerActive && len(cm.Queue) > 0 {
						ticker.Reset(5 * time.Millisecond)
					}
				}
				continue
			}
			heap.Push(&cm.Queue, chunk)
			if !tickerActive {
				ticker.Reset(5 * time.Millisecond)
				tickerActive = true
				// log.Println("Start Ticker", order, cm.Queue[0].Idx, len(cm.Queue))
			}

		case <-ticker.C:
			if len(cm.Queue) > 0 {
				//log.Println("Flush", order, len(cm.Queue))
				order = cm.sendQueue(ctx, cm.Queue[0].Idx)
				/*log.Println("Flushed", order, len(cm.Queue))
				for _, c := range cm.Queue {
					log.Println("       -", c.Idx)
				}*/
			}

		case <-ctx.Done():
			return
		}
	}
}

func (cm *ConnManager) changeWeights(addr net.Addr, weight int) {
	key := toKey(addr)
	if c, ok := cm.Channels[key]; ok {
		log.Println("set Weight", addr, weight)
		c.sendWeight = int((weight * MTU) / 100)
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

func (cm *ConnManager) AddChannel(id uint16, addr net.Addr, conn WriteConnection) {
	signal := make(chan bool, 1)
	chl := &channel{
		id:            id,
		signal:        signal,
		SendChl:       make(chan Message),
		addr:          addr,
		sendCount:     0,
		receiveCount:  0,
		receiveWeight: 0,
		sendWeight:    MTU,
	}
	key := toKey(addr)
	{
		cm.mu.Lock()
		defer cm.mu.Unlock()
		cm.Channels[key] = chl
	}
	log.Println("AddChannel", key)

	go func() {
		for {
			timer := time.NewTimer(cm.heartbeat)
			select {
			case msg, more := <-chl.SendChl:
				if !more {
					return
				}

				msg.SetRouterAddr(chl.addr) // addr port can change after reconnect
				// log.Println("Send", addr, sendCount, chunk)
				size := msg.Write(cm, conn)
				if size > 0 {
					cm.UploadBytes += size
					chl.sendCount++
				}
			case <-timer.C:
				cm.pingPong(chl, conn)
			}
		}
	}()
}

func (cm *ConnManager) SendPing(id uint16, addr net.Addr, conn WriteConnection) {
	(&PingMsg{
		MsgBase: MsgBase{addr},
		Id:      id,
	}).Write(cm, conn)
}

func (cm *ConnManager) PingPong(addr net.Addr, conn WriteConnection) bool {
	key := toKey(addr)
	if c, ok := cm.Channels[key]; ok {
		return cm.pingPong(c, conn)
	}
	return false
}

func (cm *ConnManager) pingPong(c *channel, conn WriteConnection) bool {
	//log.Println("PingPong", c.addr)
	cm.SendPing(c.id, c.addr, conn)

	ticker := time.NewTicker(time.Second)
	for i := 0; i < 10; i++ {
		select {
		case _, more := <-c.signal:
			return more
		case <-ticker.C:
			log.Println("ResendPing", c.addr)
			cm.SendPing(c.id, c.addr, conn)
		}
	}
	log.Println("Disconnected")
	c.sendWeight = 0
	return true
}

func (cm *ConnManager) GotPong(addr net.Addr) {
	key := toKey(addr)
	if c, ok := cm.Channels[key]; ok {
		if len(c.signal) == 0 {
			// log.Println("GotAck", addr)
			c.signal <- true
		}
		if c.sendWeight == 0 {
			c.sendWeight = MTU
		}
	}
}

func (cm *ConnManager) ReceivedChunk(addr net.Addr, chunk *Chunk) {
	key := toKey(addr)
	if c, ok := cm.Channels[key]; ok {
		c.receiveCount++
		c.receiveWeight += int(chunk.Size)
	}
	cm.rcount += 1
	cm.DownloadBytes += int(chunk.Size)
}

func (cm *ConnManager) SendPong(id uint16, addr net.Addr, conn WriteConnection) {
	// log.Println("SendAck", addr)
	key := toKey(addr)
	if c, ok := cm.Channels[key]; ok {
		c.addr = addr
	} else {
		cm.AddChannel(id, addr, conn)
	}

	(&PongMsg{
		MsgBase: MsgBase{addr},
		Id:      id,
	}).Write(cm, conn)
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

func toKey(addr net.Addr) string {
	return addr.(*net.UDPAddr).IP.String()
}

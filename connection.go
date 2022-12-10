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

	"github.com/repeale/fp-go"
)

const (
	// AppVersion contains current application version for -version command flag
	AppVersion = "0.2.0"

	MIN_SEND_LIMIT = 4 * MTU
)

/*
Dispatches Chunks to Channels
*/
type ConnManager struct {
	ChunkSupply chan *Chunk
	Channels    []*Channel
	Config      *Config

	activeReceiver uint16
	receiverMutex  sync.Mutex
	lastReceiveAge Wrapped

	receiveChunks chan *Chunk
	heartbeat     time.Duration
	ctx           context.Context
	Logger        func(format string, v ...any)
}

func NewConnMananger(ctx context.Context, config *Config) *ConnManager {
	return &ConnManager{
		ChunkSupply:    make(chan *Chunk, 2000),
		Channels:       make([]*Channel, len(config.Channels)),
		Config:         config,
		activeReceiver: 0,
		receiverMutex:  sync.Mutex{},
		lastReceiveAge: 0,
		receiveChunks:  make(chan *Chunk),
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
					channels += fmt.Sprintf("  - %v:\n    Transmission: %v\n",
						chl.Id, chl.TransmissionTime)
				}

				os.WriteFile(cm.Config.MonitorPath, []byte(channels), 0666)

			case <-cm.ctx.Done():
				return
			}
		}
	}
}

func (cm *ConnManager) handleBlockMsg(chl *Channel) {
	for i := 0; true; i++ {
		oldestChl := chl
		for _, c := range cm.Channels {
			if c.Age.Less(oldestChl.Age) {
				oldestChl = c
			}
		}

		if oldestChl.Age != 0 && i < 40 && cm.lastReceiveAge.Inc().Less(oldestChl.Age) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if i != 0 {
			cm.Log("Wait for next age %v(%v) != %v+1| %v ", oldestChl.Id, oldestChl.Age, cm.lastReceiveAge, i)
		}

		cm.receiverMutex.Lock()
		defer cm.receiverMutex.Unlock()

		cm.activeReceiver = oldestChl.Id
		/*cm.Log("Activate receive block %v(%v) %v(%v)\n",
		oldestChl.Id, oldestChl.Age, chl.Id, chl.Age)*/
		if oldestChl.Age == 0 {
			// No receiving channel
			return
		}
		cm.lastReceiveAge = oldestChl.Age
		if oldestChl.waitForSignal {
			select {
			case oldestChl.startSignal <- true:
				oldestChl.waitForSignal = false
			case <-cm.ctx.Done():
			}
		}
		return
	}
}

func (cm *ConnManager) waitForActivation(chl *Channel) {
	for cm.activeReceiver != chl.Id {
		cm.receiverMutex.Lock()
		if cm.activeReceiver != chl.Id {
			chl.waitForSignal = true
			cm.receiverMutex.Unlock()
			select {
			case <-chl.startSignal:
			case <-cm.ctx.Done():
			}
		} else {
			cm.receiverMutex.Unlock()
		}
	}
}

func (cm *ConnManager) calcSendLimit(chl *Channel) int {
	maxTime := fp.Reduce(func(acc time.Duration, current *Channel) time.Duration {
		if acc > current.TransmissionTime {
			return acc
		} else {
			return current.TransmissionTime
		}
	}, 0)(cm.Channels)

	return int(MIN_SEND_LIMIT * maxTime / chl.TransmissionTime)
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
	limit := MIN_SEND_LIMIT
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
					age = age.Inc()
					for j := 0; j < 3; j++ {
						chl.Send(StartBlock(age))
					}
					limit = cm.calcSendLimit(chl)
					/*cm.Log("Send Startblock %v(%v) %v b %v bps = %v",
					chl.Id, age, limit, chl.SendSpeed, float32(limit)/float32(chl.SendSpeed))*/
				}
				sendBytes += size

				// cm.Log("Send chunk  %v(%v) %v %v < %v\n", chl.Id, age, size, sendBytes, limit)
				chl.Send(&chunk)
				if sendBytes > limit {
					// cm.Log("Send Stopblock %v", chl.Id)
					for j := 0; j < 3; j++ {
						chl.Send(StartBlock(0))
					}
					sendBytes = 0
					active = winc(active)
				}
				size = 0
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

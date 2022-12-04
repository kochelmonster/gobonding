package gobonding_test

import (
	"bytes"
	"context"
	"io"
	"log"
	"testing"
	"time"

	"github.com/kochelmonster/gobonding"
)

func createConnManager(ctx context.Context) *gobonding.ConnManager {
	config := gobonding.Config{}
	return gobonding.NewConnMananger(ctx, &config)
}

func TestAllocAndFree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cm := createConnManager(ctx)

	chunk1 := cm.AllocChunk()
	chunk2 := cm.AllocChunk()
	chunk1.Size = 1
	chunk2.Size = 2
	cm.FreeChunk(chunk1)

	chunk3 := cm.AllocChunk()
	if chunk3.Size != 1 {
		t.Fatalf("Chunk not reused")
	}

	cm.FreeChunk(chunk3)
	cm.FreeChunk(chunk2)

	cancel()
}

type MockReader struct {
	Input  chan []byte
	Output chan []byte
	Closed bool
	ctx    context.Context
}

func (mr *MockReader) Read(p []byte) (n int, err error) {
	if mr.Closed {
		return 0, io.EOF
	}
	select {
	case b := <-mr.Input:
		copy(p, b)
		return len(b), nil

	case <-mr.ctx.Done():
		return 0, io.EOF
	}
}

func (mr *MockReader) Write(p []byte) (n int, err error) {
	if mr.Closed {
		return 0, io.EOF
	}
	b := make([]byte, len(p))
	copy(b, p)
	select {
	case mr.Output <- b:
		return len(p), err
	case <-mr.ctx.Done():
		return 0, io.EOF
	}
}

func (mr *MockReader) Close() error {
	mr.Closed = true
	return nil
}

type ClosedIO struct{}

func (cio *ClosedIO) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (cio *ClosedIO) Write(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (cio *ClosedIO) Close() error {
	return nil
}

type RouterCM gobonding.ConnManager

func (cm *RouterCM) Log(format string, v ...any) {
	log.Printf("router: "+format, v...)
}

func TestCommunication(t *testing.T) {
	const TIMEOUT = 3 * time.Second

	config := &gobonding.Config{
		HeartBeatTime:  "100ms",
		ProxyStartPort: 41414,
		Channels:       map[string]string{"1": "1", "2": "2"},
	}

	ctx, cancel := context.WithCancel(context.Background())

	ioProxy := MockReader{
		Input:  make(chan []byte, 1),
		Output: make(chan []byte, 1),
		Closed: false,
		ctx:    ctx,
	}

	pr1 := make(chan []byte, 4)
	rp1 := make(chan []byte, 4)
	pr2 := make(chan []byte, 4)
	rp2 := make(chan []byte, 4)

	proxy := gobonding.NewConnMananger(ctx, config)
	proxy.Logger = func(format string, v ...any) {
		log.Printf("proxy: "+format, v...)
	}
	gobonding.NewChannel(proxy, 0, &MockReader{pr1, rp1, false, ctx}).Start()
	gobonding.NewChannel(proxy, 1, &MockReader{pr2, rp2, false, ctx}).Start()
	go proxy.Receiver(&ioProxy)
	go proxy.Sender(&ioProxy)

	rctx, cancelRouter := context.WithCancel(ctx)

	ioRouter := MockReader{
		Input:  make(chan []byte, 1),
		Output: make(chan []byte, 1),
		Closed: false,
		ctx:    rctx,
	}
	router := gobonding.NewConnMananger(ctx, config)
	router.Logger = func(format string, v ...any) {
		log.Printf("router 1: "+format, v...)
	}
	rc1 := gobonding.NewChannel(router, 0, &MockReader{rp1, pr1, false, rctx}).Start()
	rc2 := gobonding.NewChannel(router, 1, &MockReader{rp2, pr2, false, rctx}).Start()
	go router.Receiver(&ioRouter)
	go router.Sender(&ioRouter)

	ioRouter.Input <- []byte("Test")

	select {
	case cmp := <-ioProxy.Output:
		if string(cmp) != "Test" {
			t.Fatalf("Wrong Firstmessage")
		}
	case <-time.After(TIMEOUT):
		t.Fatalf("Timeout")
	}

	buffer := make([]byte, 1000)
	for j := range buffer {
		buffer[j] = 0x0
	}

	for i := 1; i < 98; i++ {
		log.Println("copy in", i)
		buffer[0] = 1
		buffer[1] = byte(i)
		ioRouter.Input <- buffer
		select {
		case cmp := <-ioProxy.Output:
			if !bytes.Equal(buffer, cmp) {
				t.Fatalf("Wrong message %v", i)
			}
		case <-time.After(TIMEOUT):
			t.Fatalf("Timeout")
		}
	}

	cancelRouter()
	ioRouter.Close()
	rc1.Io = &ClosedIO{}
	rc2.Io = &ClosedIO{}

	ioRouter = MockReader{
		Input:  ioRouter.Input,
		Output: ioRouter.Output,
		Closed: false,
		ctx:    ctx,
	}

	// New Router must synchronize it self
	router = gobonding.NewConnMananger(ctx, config)
	router.Logger = func(format string, v ...any) {
		log.Printf("router 2: "+format, v...)
	}
	gobonding.NewChannel(router, 0, &MockReader{rp1, pr1, false, ctx}).Start()
	gobonding.NewChannel(router, 1, &MockReader{rp2, pr2, false, ctx}).Start()
	go router.Receiver(&ioRouter)
	go router.Sender(&ioRouter)

	log.Println("Synchronized new Router")
	buffer[0] = 1
	buffer[1] = byte(1)
	ioRouter.Input <- buffer
	select {
	case cmp := <-ioProxy.Output:
		if !bytes.Equal(buffer, cmp) {
			t.Fatalf("Wrong synched message")
		}
	case <-time.After(TIMEOUT):
		t.Fatalf("Timeout")
	}

	log.Println("End")
	cancel()
	router.Close()
	proxy.Close()
}

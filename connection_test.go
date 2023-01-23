package gobonding_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/kochelmonster/gobonding"
)

const (
	PUBLIC_KEY = `-----BEGIN RSA PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA5QuUZf2wwmK3eMp5NIge
t0HCLODxJ9VwRsT0b7BHJ5y893EkGlqCc8X6bFSRIa2SVHJWrw0KiIiS3htRS79W
95msjaraJEpHxJgZr6MsANYcQVJ7UJl7NhN3dMC14YdV61fkk8iC6KhqxRtNIHY1
kDgCOmt19QaS7rWt1d5v+alM1xoRF0vHUi5icgMitGuMuYqdc59J/1SBk4jwNx+1
AqI0XkbjPG8ymxYryJNkInLO2qOZ4dEwVPHkjkgg/b3HrG2T94fz51m99pqTZFhB
Ft0/PdvEjPpNI4t5QbKdUHNfSZyqENU1VVw239/oyQ9c/fl0POCRD1PVYts78Ehb
rQIDAQAB
-----END RSA PUBLIC KEY-----
`
	PRIVATE_KEY = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA5QuUZf2wwmK3eMp5NIget0HCLODxJ9VwRsT0b7BHJ5y893Ek
GlqCc8X6bFSRIa2SVHJWrw0KiIiS3htRS79W95msjaraJEpHxJgZr6MsANYcQVJ7
UJl7NhN3dMC14YdV61fkk8iC6KhqxRtNIHY1kDgCOmt19QaS7rWt1d5v+alM1xoR
F0vHUi5icgMitGuMuYqdc59J/1SBk4jwNx+1AqI0XkbjPG8ymxYryJNkInLO2qOZ
4dEwVPHkjkgg/b3HrG2T94fz51m99pqTZFhBFt0/PdvEjPpNI4t5QbKdUHNfSZyq
ENU1VVw239/oyQ9c/fl0POCRD1PVYts78EhbrQIDAQABAoIBACSjFIq36LU/Ox/M
K1UWzOr9TsUE+i4n+vym9n6DEO6qKKPf6il4/tLsASGg6VIcxIJTg8AecufiCLQU
ZI2cPpn+b9Z9VMVnSFGPDtTEYf6EQSLFwcnjswy0UnBPfwhjMIAjoAFvmlkCz4lV
06F0px65hsm3dLfL5GbgkrzaBslFWPBqLptFMQXglm5ywkysiPTqzNbsg7nkCKUi
F7kPZEzdozVKtgRxLgAx3OFhSZU85QAs45wBXyGm2+J/looxSE8rrsu0vw/m/NI5
d9Y2HgVXbpIeu+hgKljhaGlrQimkoBopzPgKEEspXRklmI/l1tYikIkT3iHRbx1M
YygDGgECgYEA60LQyKIiyYdZyxVDQMm4V880GX017cBU9q7Iw6GPKVXpfqWAt7yG
eOHKspLcT3DmNLFNQIQf5XDLd5Rc1JVxzaFYNAn9OdmwQ81BW4qP3hVNtEGQ83RS
VroEVYs3q09w34507Qina5V2byz0eb/1QoKFfE0zORBAH5WLuKX1zy0CgYEA+Tx9
3B4IYgMnUgHaEUHvHoLAjiDSQUBlYyhHnRwxJn6LPLIPeQGsvOnrWkNcs5W4/ilj
ipmrc+SEQrIWQNAjFabcq9E5rkqQxTmUwPrGpa/vtoOGhjIseZLUNBQqUa+Slgg6
ifj9D6Ky9GZ9sfXGiwMyW9Mk5r3ox9Syn+CAjoECgYBLq/osDrrRx8+CGxy+wiOh
WuyPJk8qYiryDdZV1qmNyiyIqAN3FhTK3RWtyr9CbjYdzMnkbpsz2cwYcohJeKha
VANi+bOR4AtqQ6M6Jp+P95o+2LgfFtNFQiASw+zsFWlg/xltBNOVL0YhDHy2jJ/+
/KyjBtHrEOcPQbLnebpPIQKBgQDiSQfEiAf4ZQCYNlI1BPYDb5c/85Cx6bOjuXh7
rpL5bj8gllHx/ZFF2+PxCePqsO9K420a87Z0/G8Q1vvZUJ/qEpub69RA6DZUupjS
NV2SJRCxVu0WfgtfPe4ocn6Rt6SRT1tG1ad9QKzVtRA+OPVQVVCtiiCg1p+4fubG
vWA7AQKBgBXuxEdhie0NNDUnjKgQWUr1ODzZG1BLX5zd5gZG0c1GWSsYtoPWmXHi
bW1Er5ORlnMGX847ZZwSW22p3xXUefcWxCM+4HC03GKdT81dXAopKjjYhzd3SLRT
S2dG0FKqSHGyCeK3z2b1raDptBTXz27ji62kTMXQ1Pmdjg5TpK9v
-----END RSA PRIVATE KEY-----`
)

func createConnManager(ctx context.Context) *gobonding.ConnManager {
	config := gobonding.Config{}
	return gobonding.NewConnMananger(ctx, &config)
}

func TestWrapped(t *testing.T) {
	a := gobonding.Wrapped(0)
	b := gobonding.Wrapped(1)

	if a.Less(b) {
		t.Fatalf("0 is always greater")
	}

	if !b.Less(a) {
		t.Fatalf("0 is always greater")
	}

	c := gobonding.Wrapped(0xFFFF)
	if !c.Less(b) {
		t.Fatalf("wrong wrapped less")
	}
	if b.Less(c) {
		t.Fatalf("wrong wrapped less")
	}

	c = c.Inc()
	if c != 1 {
		t.Fatalf("wrong inc")
	}
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

func waitForChannels(cm *gobonding.ConnManager) {
	for i := 0; i < 10; i++ {
		active := 0
		for _, chl := range cm.Channels {
			if chl.Active() {
				active += 1
			}
		}
		if active == len(cm.Channels) {
			return
		}
		time.Sleep(50 * time.Microsecond)
	}
}

func TestCommunication(t *testing.T) {
	const TIMEOUT = 5 * time.Second

	config := &gobonding.Config{
		ProxyStartPort: 41414,
		Channels:       map[string]string{"1": "1", "2": "2"},
		PrivateKey:     PRIVATE_KEY,
		PublicKey:      PUBLIC_KEY,
	}

	ctx, cancel := context.WithCancel(context.Background())

	ioProxy := MockReader{
		Input:  make(chan []byte, 1),
		Output: make(chan []byte, 1),
		Closed: false,
		ctx:    ctx,
	}

	pr1 := make(chan []byte, 400)
	rp1 := make(chan []byte, 400)
	pr2 := make(chan []byte, 400)
	rp2 := make(chan []byte, 400)

	proxy := gobonding.NewConnMananger(ctx, config)
	proxy.Logger = func(format string, v ...any) {
		r := fmt.Sprintf(format, v...)
		switch {
		case strings.HasPrefix(r, "++Receive 0") == true:
			return
		case strings.HasPrefix(r, "--Receive 0") == true:
			return
		}
		log.Printf("proxy: "+format, v...)
	}
	gobonding.NewChannel(proxy, 0, &MockReader{pr1, rp1, false, ctx}, true).Start()
	gobonding.NewChannel(proxy, 1, &MockReader{pr2, rp2, false, ctx}, true).Start()
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
		switch {
		case strings.HasPrefix(format, "++Receive") == true:
			return
		case strings.HasPrefix(format, "--Receive") == true:
			return
		}

		log.Printf("router 1: "+format, v...)
	}
	rc1 := gobonding.NewChannel(router, 0, &MockReader{rp1, pr1, false, rctx}, false).Start()
	rc2 := gobonding.NewChannel(router, 1, &MockReader{rp2, pr2, false, rctx}, false).Start()
	go router.Receiver(&ioRouter)
	go router.Sender(&ioRouter)

	waitForChannels(router)
	waitForChannels(proxy)

	log.Println("Send Test")
	ioRouter.Input <- []byte("\x01\x01Test")

	select {
	case cmp := <-ioProxy.Output:
		if string(cmp) != "\x01\x01Test" {
			t.Fatalf("Wrong Firstmessage")
		}
	case <-time.After(TIMEOUT):
		log.Println("Timeout")
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
			log.Println("Timeout")
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
	gobonding.NewChannel(router, 0, &MockReader{rp1, pr1, false, ctx}, false).Start()
	gobonding.NewChannel(router, 1, &MockReader{rp2, pr2, false, ctx}, false).Start()
	go router.Receiver(&ioRouter)
	go router.Sender(&ioRouter)

	waitForChannels(router)
	waitForChannels(proxy)

	log.Println("Synchronized new Router")
	log.Println("=======================")
	buffer[0] = 1
	buffer[1] = byte(1)
	ioRouter.Input <- buffer
	select {
	case cmp := <-ioProxy.Output:
		if !bytes.Equal(buffer, cmp) {
			t.Fatalf("Wrong synched message")
		}
	case <-time.After(TIMEOUT):
		log.Println("Timeout")
		t.Fatalf("Timeout")
	}

	log.Println("End")
	cancel()
	router.Close()
	proxy.Close()
	time.Sleep(time.Microsecond)
}

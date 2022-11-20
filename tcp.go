package gobonding

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

func HandleConnection(ctx context.Context, conn *net.TCPConn, cm *ConnManager) {
	var wg sync.WaitGroup
	wg.Add(2)

	ctx, cancel := context.WithCancel(ctx)

	conn.SetKeepAlive(true)
	conn.SetNoDelay(false)

	timeVal := new(unix.Timeval)
	timeVal.Sec = 2
	file, err := conn.File()
	if err != nil {
		err = unix.SetsockoptTimeval(int(file.Fd()), unix.SOL_SOCKET, unix.SO_SNDTIMEO, timeVal)
		if err != nil {
			log.Println("error timeout", err)
		}
	}

	go func() {
		defer wg.Done()
		for {
			msg, err := ReadMessage(conn, cm)
			// log.Println("Receive from", conn.LocalAddr(), cm.PeerOrder, msg)
			if err != nil {
				log.Println("Closing receive stream to", conn.RemoteAddr(), err)
				conn.Close()
				cancel()
				return
			}
			msg.Action(ctx, cm)
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case msg := <-cm.DispatchChannel:
				// log.Println("Send to", conn.RemoteAddr(), conn.LocalAddr(), msg)
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				err := msg.Write(conn)
				if err != nil {
					log.Println("Closing send stream to", conn.RemoteAddr(), err)
					conn.Close()
					return
				}
			case <-ctx.Done():
				conn.Close()
				return
			}
		}
	}()

	id := conn.LocalAddr().String() + "->" + conn.RemoteAddr().String()
	cm.ActiveChannels[id] = true
	log.Printf("Connection established %v: %v\n", id, cm.ActiveChannels)
	wg.Wait()
	delete(cm.ActiveChannels, id)
	log.Printf("Close connection %v: %v\n", id, cm.ActiveChannels)
}

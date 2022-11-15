package gobonding

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"sync"

	"github.com/lucas-clemente/quic-go"
)

func CreateTlsConf(config *Config) *tls.Config {
	tlsCert, err := tls.X509KeyPair([]byte(config.Certificate), []byte(config.PrivateKey))
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"bonding-proxy"},
		Certificates:       []tls.Certificate{tlsCert},
		ClientAuth:         tls.RequireAnyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			for _, c1 := range rawCerts {
				for _, c2 := range tlsCert.Certificate {
					if bytes.Equal(c1, c2) {
						log.Println("Certificates Equal")
						return nil
					}
				}
			}
			return errors.New("certificates not equal")
		},
	}
}

func HandleStream(conn quic.Connection, stream quic.Stream, cm *ConnManager) {
	var wg sync.WaitGroup
	wg.Add(2)

	ctx, cancel := context.WithCancel(conn.Context())

	go func() {
		defer wg.Done()
		for {
			msg, err := ReadMessage(stream, cm)
			// log.Println("Receive from", conn.RemoteAddr(), msg)
			if err != nil {
				log.Println("Closing receive stream to", conn.RemoteAddr(), err)
				conn.CloseWithError(1, "stream error")
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
				// log.Println("Send to", conn.RemoteAddr(), msg)
				err := msg.Write(stream)
				if err != nil {
					cm.DispatchChannel <- msg
					log.Println("Closing send stream to", conn.RemoteAddr(), err)
					conn.CloseWithError(1, "stream error")
					return
				}
			case <-ctx.Done():
				conn.CloseWithError(2, "close server")
				return
			}
		}
	}()

	cm.ActiveChannels++
	log.Println("Connection established", conn.RemoteAddr(), cm.ActiveChannels)
	wg.Wait()
	cm.ActiveChannels--
	log.Println("Close connection", conn.RemoteAddr(), cm.ActiveChannels)
}

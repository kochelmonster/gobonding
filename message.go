package gobonding

import (
	"crypto"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"math/rand"
	"time"
)

const (
	MTU = 1500

	// BSIZE is size of buffer to receive packets
	// (little bit bigger than maximum)
	BUFFERSIZE = MTU + 218 + 2 + 8

	SOCKET_BUFFER = 1024 * 1024
)

type Message interface {
	Buffer() []byte
	String() string
}

type Chunk struct {
	Data [BUFFERSIZE]byte
	Size uint16
	Age  Wrapped
}

func (msg *Chunk) Buffer() []byte {
	return msg.Data[:msg.Size]
}

func (msg *Chunk) IPData() []byte {
	return msg.Data[:msg.Size-2]
}

func (msg *Chunk) Gather(size uint16) {
	msg.Size = size
	msg.Age = Wrapped(binary.BigEndian.Uint16(msg.Data[msg.Size-2 : msg.Size]))
}

func (msg *Chunk) Set(age Wrapped, size uint16) {
	msg.Age = age
	msg.Size = size + 2
	binary.BigEndian.PutUint16(msg.Data[msg.Size-2:msg.Size], uint16(msg.Age))
}

func (msg *Chunk) String() string {
	return fmt.Sprintf("Chunk %v", msg.Size)
	/*
		header, err := ipv4.ParseHeader(msg.Data[0:])
		if err != nil {
			return "Error parsing buffer"
		} else {
			return fmt.Sprintf("Packet %v: %v", msg.Size, header)
		}*/
}

var epoch = time.Date(2022, time.January, 1, 0, 0, 0, 0, time.UTC)
var Epoch = epoch

type PongMsg struct {
}

func Pong() *PongMsg {
	return &PongMsg{}
}

func (msg *PongMsg) Buffer() []byte {
	buffer := []byte{0, 'o'}
	return buffer
}

func (msg *PongMsg) String() string {
	return "Pong"
}

type PingMsg struct {
}

func (msg *PingMsg) Buffer() []byte {
	return []byte{0, 'i'}
}

func (msg *PingMsg) String() string {
	return "Ping"
}

type SpeedMsg struct {
	Speed float32
}

func SpeedFromChunk(chunk *Chunk) *SpeedMsg {
	speed := math.Float32frombits(binary.BigEndian.Uint32(chunk.Data[2:]))
	return &SpeedMsg{Speed: speed}
}

func (msg *SpeedMsg) Buffer() []byte {
	buffer := []byte{0, 's', 1, 2, 3, 4}
	binary.BigEndian.PutUint32(buffer[2:], math.Float32bits(msg.Speed))
	return buffer
}

func (msg *SpeedMsg) String() string {
	return fmt.Sprintf("Speed: %v", msg.Speed)
}

type SpeedTestMsg struct {
	Age       Wrapped
	Timestamp time.Duration
	Size      uint32
}

func SpeedTest(age Wrapped, ts time.Time, size uint32) *SpeedTestMsg {
	return &SpeedTestMsg{Age: age, Timestamp: ts.Sub(epoch), Size: size}
}

func SpeedTestFromChunk(chunk *Chunk) *SpeedTestMsg {
	age := Wrapped(binary.BigEndian.Uint16(chunk.Data[2:4]))
	ts := time.Duration(binary.BigEndian.Uint64(chunk.Data[4:12]))
	size := binary.BigEndian.Uint32(chunk.Data[12:16])
	return &SpeedTestMsg{Age: age, Timestamp: ts, Size: size}
}

func (m *SpeedTestMsg) Buffer() []byte {
	buffer := []byte{0, 'b', 1, 2, 1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4}
	binary.BigEndian.PutUint16(buffer[2:4], uint16(m.Age))
	binary.BigEndian.PutUint64(buffer[4:12], uint64(m.Timestamp))
	binary.BigEndian.PutUint32(buffer[12:16], uint32(m.Size))
	return buffer
}

func (msg *SpeedTestMsg) String() string {
	ts := epoch.Add(msg.Timestamp)
	return fmt.Sprintf("SpeedTest: %v %v %v", msg.Age, ts, msg.Size)
}

type ChallengeMsg struct {
	Challenge string
}

func Challenge() *ChallengeMsg {
	return &ChallengeMsg{Challenge: fmt.Sprintf("HEL%v%v", time.Now(), rand.Int())}
}

func ChallengeFromChunk(chunk *Chunk, size int) *ChallengeMsg {
	return &ChallengeMsg{Challenge: string(chunk.Data[2:size])}
}

func (m *ChallengeMsg) Buffer() []byte {
	return append([]byte{0, 'c'}, []byte(m.Challenge)...)
}

func (m *ChallengeMsg) CreateResponse(privKey *rsa.PrivateKey) *ChallengeResponseMsg {
	h := sha256.New()
	h.Write([]byte(m.Challenge))
	hash := h.Sum(nil)
	response, err := rsa.SignPSS(crand.Reader, privKey, crypto.SHA256, hash, nil)
	if err == nil {
		err = rsa.VerifyPSS(&privKey.PublicKey, crypto.SHA256, hash, response, nil)
		if err != nil {
			log.Printf("!!!Wrong Verify %v\n", err)
		}

		return &ChallengeResponseMsg{Response: string(response)}
	}
	return &ChallengeResponseMsg{Response: ""}
}

func (m *ChallengeMsg) Verify(pubKey *rsa.PublicKey, chunk *Chunk, size int) error {
	h := sha256.New()
	h.Write([]byte(m.Challenge))
	hash := h.Sum(nil)
	response := chunk.Data[2:size]
	return rsa.VerifyPSS(pubKey, crypto.SHA256, hash, response, nil)
}

func (msg *ChallengeMsg) String() string {
	return fmt.Sprintf("Challenge: %v", msg.Challenge)
}

type ChallengeResponseMsg struct {
	Response string
}

func (m *ChallengeResponseMsg) Buffer() []byte {
	return append([]byte{0, 'r'}, []byte(m.Response)...)
}

func (msg *ChallengeResponseMsg) String() string {
	return fmt.Sprintf("ChallengeResponseMsg: %v", []byte(msg.Response))
}

// A wrapped counter
type Wrapped uint16

func (wrp Wrapped) Less(other Wrapped) bool {
	if wrp < 0x1000 && other > 0xf000 {
		// Overflow case
		return false
	}
	if other < 0x1000 && wrp > 0xf000 {
		// Overflow case
		return true
	}

	if other == 0 && wrp != 0 {
		return true
	}
	return wrp < other && wrp != 0
}

func (wrp Wrapped) Inc() Wrapped {
	(wrp)++
	if wrp == 0 {
		wrp++
	}
	return wrp
}

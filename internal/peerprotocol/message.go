package peerprotocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
)

type message interface {
	Read([]byte) (err error)
	Handle(peer *Peer) (err error)
	Send(peer *Peer) (err error)
}

type choke struct{}

func (message *choke) Read(buffer []byte) (err error) { return nil }
func (message *choke) Handle(peer *Peer) (err error) {
	peer.mu.Lock()
	peer.state.am_intrested = false
	peer.state.peer_chocking = true
	peer.mu.Unlock()
	return nil
}
func (message *choke) Send(peer *Peer) (err error) { return nil }

type unchoke struct{}

func (message *unchoke) Read(buffer []byte) (err error) { return nil }
func (message *unchoke) Handle(peer *Peer) (err error) {
	peer.mu.Lock()
	peer.state.peer_chocking = false
	peer.mu.Unlock()
	peer.signalPeerChecker <- true
	return nil
}
func (message *unchoke) Send(peer *Peer) (err error) { return nil }

type intrested struct{}

func (message *intrested) Read(buffer []byte) (err error) { return nil }
func (message *intrested) Handle(peer *Peer) (err error) {
	peer.mu.Lock()
	peer.state.peer_intrested = true
	peer.mu.Unlock()
	return nil
}
func (message *intrested) Send(peer *Peer) error {
	err := peer.SendMessage(IDFromMessage(message), make([]byte, 0))
	if err == nil {
		peer.state.am_intrested = true
	}
	return err
}

type Uninstrested struct{}

func (message *Uninstrested) Read(buffer []byte) (err error) { return nil }
func (message *Uninstrested) Handle(peer *Peer) (err error) {
	peer.mu.Lock()
	peer.state.peer_intrested = false
	peer.mu.Unlock()
	return nil
}
func (message *Uninstrested) Send(peer *Peer) (err error) { return nil }

type have struct {
	pieceIndex uint32
}

func (message *have) Read(buffer []byte) (err error) {
	message.pieceIndex = binary.BigEndian.Uint32(buffer)
	return nil
}

func (message *have) Handle(peer *Peer) (err error) {
	byteIndex := message.pieceIndex / 8
	bitOffset := 7 - (message.pieceIndex % 8)

	if int(byteIndex) >= len(peer.bitfield) {
		return errors.New("index out of range")
	}

	peer.mu.Lock()
	peer.bitfield[byteIndex] |= 1 << bitOffset
	peer.mu.Unlock()
	return nil
}
func (message *have) Send(peer *Peer) (err error) { return nil }

type bitfield struct {
	bitfield []byte
}

func (message *bitfield) Read(buffer []byte) (err error) {
	message.bitfield = make([]byte, len(buffer))
	copy(message.bitfield, buffer)
	return nil
}
func (message *bitfield) Handle(peer *Peer) (err error) {
	peer.mu.Lock()
	peer.bitfield = make([]byte, len(message.bitfield))
	copy(peer.bitfield, message.bitfield)
	peer.mu.Unlock()
	peer.signalPeerChecker <- true
	return nil
}
func (message *bitfield) Send(peer *Peer) (err error) {
	return peer.SendMessage(IDFromMessage(message), message.bitfield)
}

type request struct {
	index  uint32
	begin  uint32
	length uint32
}

func (message *request) Read(buffer []byte) (err error) {
	message.index = binary.BigEndian.Uint32(buffer[:4])
	message.begin = binary.BigEndian.Uint32(buffer[4:8])
	message.length = binary.BigEndian.Uint32(buffer[8:12])
	return nil
}
func (message *request) Handle(peer *Peer) (err error) { return nil }
func (message *request) Send(peer *Peer) (err error) {
	var buf bytes.Buffer

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, message.index)
	buf.Write(lenBuf)
	binary.BigEndian.PutUint32(lenBuf, message.begin)
	buf.Write(lenBuf)
	binary.BigEndian.PutUint32(lenBuf, message.length)
	buf.Write(lenBuf)
	return peer.SendMessage(IDFromMessage(message), buf.Bytes())

}

type piece struct {
	index uint32
	begin uint32
	block []byte
}

func (message *piece) Read(buffer []byte) (err error) {
	message.index = binary.BigEndian.Uint32(buffer[:4])
	message.begin = binary.BigEndian.Uint32(buffer[4:8])
	message.block = buffer[8:]
	return nil
}
func (message *piece) Handle(peer *Peer) (err error) {
	peer.Torrent.WriteBlock(message.index, message.begin, message.block)
	peer.mu.Lock()
	peer.requested--
	peer.mu.Unlock()
	peer.signalPeerChecker <- true
	return nil
}
func (message *piece) Send(peer *Peer) (err error) { return nil }

type cancel struct {
	index  uint32
	begin  uint32
	length uint32
}

func (message *cancel) Read(buffer []byte) (err error) {
	message.index = binary.BigEndian.Uint32(buffer[:4])
	message.begin = binary.BigEndian.Uint32(buffer[4:8])
	message.length = binary.BigEndian.Uint32(buffer[8:12])
	return nil
}
func (message *cancel) Handle(peer *Peer) (err error) { return nil }
func (message *cancel) Send(peer *Peer) (err error)   { return nil }

type port struct {
	listen_port uint16
}

func (message *port) Read(buffer []byte) (err error) {
	message.listen_port = binary.BigEndian.Uint16(buffer)
	return nil
}
func (message *port) Handle(peer *Peer) (err error) { return nil }
func (message *port) Send(peer *Peer) (err error)   { return nil }

func MsgParse(buffer []byte) (msg message, err error) {
	id := buffer[0]

	endstr := ""
	if len(buffer) > 15 {
		endstr = "\b ...]"
	}
	fmt.Printf("Got id %d, Data: %v%s\n", id, buffer[1:min(len(buffer), 15)], endstr)
	msg = MessageFromID(id)
	err = msg.Read(buffer[1:])
	return
}

var idToMessage = map[uint8]func() message{
	0: func() message { return new(choke) },
	1: func() message { return new(unchoke) },
	2: func() message { return new(intrested) },
	3: func() message { return new(Uninstrested) },
	4: func() message { return new(have) },
	5: func() message { return new(bitfield) },
	6: func() message { return new(request) },
	7: func() message { return new(piece) },
	8: func() message { return new(cancel) },
	9: func() message { return new(port) },
}

var messageToID = func() map[reflect.Type]uint8 {
	m := make(map[reflect.Type]uint8)
	for id, constructor := range idToMessage {
		m[reflect.TypeOf(constructor())] = id
	}
	return m
}()

func MessageFromID(id uint8) message {
	if constructor, ok := idToMessage[id]; ok {
		return constructor()
	}
	return nil
}

func IDFromMessage(msg message) uint8 {
	if id, ok := messageToID[reflect.TypeOf(msg)]; ok {
		return id
	}
	return 0 // or error, depending on context
}

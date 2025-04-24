package peerprotocol

import (
	"encoding/binary"
	"io"
)

type message interface {
	Read(len uint32, peer *Peer) (err error)
	Handle(peer *Peer) (err error)
}

type choke struct{}

func (message *choke) Read(len uint32, peer *Peer) (err error) { return nil }
func (message *choke) Handle(peer *Peer) (err error)           { return nil }

type unchoke struct{}

func (message *unchoke) Read(len uint32, peer *Peer) (err error) { return nil }
func (message *unchoke) Handle(peer *Peer) (err error)           { return nil }

type intrested struct{}

func (message *intrested) Read(len uint32, peer *Peer) (err error) { return nil }
func (message *intrested) Handle(peer *Peer) (err error)           { return nil }

type Uninstrested struct{}

func (message *Uninstrested) Read(len uint32, peer *Peer) (err error) { return nil }
func (message *Uninstrested) Handle(peer *Peer) (err error)           { return nil }

type have struct {
	pieceIndex uint32
}

func (message *have) Read(len uint32, peer *Peer) (err error) {
	indexBuf := make([]byte, 4)
	_, err = io.ReadFull(peer.conn, indexBuf)
	if err != nil {
		return err
	}
	message.pieceIndex = binary.BigEndian.Uint32(indexBuf)
	return nil
}

func (message *have) Handle(peer *Peer) (err error) { return nil }

type bitfield struct {
	bitfiled []byte
}

func (message *bitfield) Read(len uint32, peer *Peer) (err error) {
	message.bitfiled = make([]byte, int(len-1))
	_, err = io.ReadFull(peer.conn, message.bitfiled)
	return
}
func (message *bitfield) Handle(peer *Peer) (err error) { return nil }

type request struct {
	index  uint32
	begin  uint32
	length uint32
}

func (message *request) Read(len uint32, peer *Peer) (err error) {
	buf := make([]byte, 12)
	_, err = io.ReadFull(peer.conn, buf)
	if err != nil {
		return
	}
	message.index = binary.BigEndian.Uint32(buf[:4])
	message.begin = binary.BigEndian.Uint32(buf[4:8])
	message.length = binary.BigEndian.Uint32(buf[8:12])
	return nil
}
func (message *request) Handle(peer *Peer) (err error) { return nil }

type piece struct {
	index uint32
	begin uint32
	block []byte
}

func (message *piece) Read(len uint32, peer *Peer) (err error) {
	buf := make([]byte, int(len-1))
	_, err = io.ReadFull(peer.conn, buf)
	if err != nil {
		return
	}
	message.index = binary.BigEndian.Uint32(buf[:4])
	message.begin = binary.BigEndian.Uint32(buf[4:8])
	message.block = buf[8:]
	return nil
}
func (message *piece) Handle(peer *Peer) (err error) { return nil }

type cancel struct {
	index  uint32
	begin  uint32
	length uint32
}

func (message *cancel) Read(len uint32, peer *Peer) (err error) {
	buf := make([]byte, 12)
	_, err = io.ReadFull(peer.conn, buf)
	if err != nil {
		return
	}
	message.index = binary.BigEndian.Uint32(buf[:4])
	message.begin = binary.BigEndian.Uint32(buf[4:8])
	message.length = binary.BigEndian.Uint32(buf[8:12])
	return nil
}
func (message *cancel) Handle(peer *Peer) (err error) { return nil }

type port struct {
	listen_port uint16
}

func (message *port) Read(len uint32, peer *Peer) (err error) {
	buf := make([]byte, 2)
	_, err = io.ReadFull(peer.conn, buf)
	if err != nil {
		return
	}
	message.listen_port = binary.BigEndian.Uint16(buf)
	return nil
}
func (message *port) Handle(peer *Peer) (err error) { return nil }

func MessageFromID(id uint8) message {
	switch id {
	case 1:
		return new(choke)
	case 2:
		return new(unchoke)
	case 3:
		return new(intrested)
	case 4:
		return new(have)
	case 5:
		return new(bitfield)
	case 6:
		return new(request)
	case 7:
		return new(piece)
	case 8:
		return new(cancel)
	case 9:
		return new(port)
	default:
		return nil
	}
}

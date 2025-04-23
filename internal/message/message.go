package message

import (
	"encoding/binary"
	"io"

	"github.com/kapilpokhrel/goTorr/internal/peer"
)

type Message interface {
	Read(len uint32, peer *peer.Peer) (err error)
	Handle(peer *peer.Peer) (err error)
}

type Choke struct{}

func (message *Choke) Read(len uint32, peer *peer.Peer) (err error) { return nil }
func (message *Choke) Handle(peer *peer.Peer) (err error)           { return nil }

type Unchoke struct{}

func (message *Unchoke) Read(len uint32, peer *peer.Peer) (err error) { return nil }
func (message *Unchoke) Handle(peer *peer.Peer) (err error)           { return nil }

type Intrested struct{}

func (message *Intrested) Read(len uint32, peer *peer.Peer) (err error) { return nil }
func (message *Intrested) Handle(peer *peer.Peer) (err error)           { return nil }

type Uninstrested struct{}

func (message *Uninstrested) Read(len uint32, peer *peer.Peer) (err error) { return nil }
func (message *Uninstrested) Handle(peer *peer.Peer) (err error)           { return nil }

type Have struct {
	pieceIndex uint32
}

func (message *Have) Read(len uint32, peer *peer.Peer) (err error) {
	indexBuf := make([]byte, 4)
	_, err = io.ReadFull(peer.Conn, indexBuf)
	if err != nil {
		return err
	}
	message.pieceIndex = binary.BigEndian.Uint32(indexBuf)
	return nil
}

func (message *Have) Handle(peer *peer.Peer) (err error) { return nil }

type Bitfield struct {
	bitfiled []byte
}

func (message *Bitfield) Read(len uint32, peer *peer.Peer) (err error) {
	message.bitfiled = make([]byte, int(len-1))
	_, err = io.ReadFull(peer.Conn, message.bitfiled)
	return
}
func (message *Bitfield) Handle(peer *peer.Peer) (err error) { return nil }

type Request struct {
	index  uint32
	begin  uint32
	length uint32
}

func (message *Request) Read(len uint32, peer *peer.Peer) (err error) {
	buf := make([]byte, 12)
	_, err = io.ReadFull(peer.Conn, buf)
	if err != nil {
		return
	}
	message.index = binary.BigEndian.Uint32(buf[:4])
	message.begin = binary.BigEndian.Uint32(buf[4:8])
	message.length = binary.BigEndian.Uint32(buf[8:12])
	return nil
}
func (message *Request) Handle(peer *peer.Peer) (err error) { return nil }

type Piece struct {
	index uint32
	begin uint32
	block []byte
}

func (message *Piece) Read(len uint32, peer *peer.Peer) (err error) {
	buf := make([]byte, int(len-1))
	_, err = io.ReadFull(peer.Conn, buf)
	if err != nil {
		return
	}
	message.index = binary.BigEndian.Uint32(buf[:4])
	message.begin = binary.BigEndian.Uint32(buf[4:8])
	message.block = buf[8:]
	return nil
}
func (message *Piece) Handle(peer *peer.Peer) (err error) { return nil }

type Cancel struct {
	index  uint32
	begin  uint32
	length uint32
}

func (message *Cancel) Read(len uint32, peer *peer.Peer) (err error) {
	buf := make([]byte, 12)
	_, err = io.ReadFull(peer.Conn, buf)
	if err != nil {
		return
	}
	message.index = binary.BigEndian.Uint32(buf[:4])
	message.begin = binary.BigEndian.Uint32(buf[4:8])
	message.length = binary.BigEndian.Uint32(buf[8:12])
	return nil
}
func (message *Cancel) Handle(peer *peer.Peer) (err error) { return nil }

type Port struct {
	listen_port uint16
}

func (message *Port) Read(len uint32, peer *peer.Peer) (err error) {
	buf := make([]byte, 2)
	_, err = io.ReadFull(peer.Conn, buf)
	if err != nil {
		return
	}
	message.listen_port = binary.BigEndian.Uint16(buf)
	return nil
}
func (message *Port) Handle(peer *peer.Peer) (err error) { return nil }

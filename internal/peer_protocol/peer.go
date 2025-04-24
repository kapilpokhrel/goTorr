package peerprotocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"goTorr/internal/torrent"
)

type state struct {
	am_chocking    bool
	am_intrested   bool
	peer_chocking  bool
	peer_intrested bool
}

type Peer struct {
	conn    net.Conn
	id      [20]byte
	state   state
	Torrent *torrent.Torrent
}

func (peer *Peer) Establish_Conn(ip string, port uint16, clientID []byte) (err error) {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		return
	}
	peer.conn = conn

	peer.state = state{true, false, true, false}

	if peer.Torrent == nil {
		peer.conn.Close()
		return errors.New(("No Torrents specified, can't send handshake"))
	}

	err = peer.sendHandShake(clientID)
	if err != nil {
		defer peer.conn.Close()
		return fmt.Errorf("Handshake Error: %w", err)
	}
	err = peer.waitForHandShake()
	if err != nil {
		defer peer.conn.Close()
		return fmt.Errorf("Handshake Error: %w", err)
	}

	return nil
}

func (peer *Peer) sendHandShake(clientID []byte) error {
	var buf bytes.Buffer
	buf.WriteByte(19)
	buf.WriteString("BitTorrent Protocol")
	buf.Write(make([]byte, 8))          // reserved
	buf.Write(peer.Torrent.InfoHash[:]) // info_hash (20 bytes)
	buf.Write(clientID)                 // peer_id (20 bytes)
	_, err := peer.conn.Write(buf.Bytes())
	return err
}

func (peer *Peer) waitForHandShake() (err error) {
	peer.conn.SetReadDeadline(time.Now().Add(2 * time.Minute)) // Timeout of 2 minutes
	pstrlenBuf := make([]byte, 1)
	_, err = io.ReadFull(peer.conn, pstrlenBuf)
	if err != nil {
		return
	}
	peer.conn.SetReadDeadline(time.Time{}) // Reset deadline

	pstrlen := int(pstrlenBuf[0])
	pstrBuf := make([]byte, pstrlen)
	_, err = io.ReadFull(peer.conn, pstrBuf)
	if err != nil {
		return
	}
	if string(pstrBuf) != "BitTorrent Protocol" {
		return errors.New(fmt.Sprintf("Unidentified Protocol, %s", string(pstrBuf)))
	}

	other := make([]byte, 48) // 8 byte reserved, 20 byte info_hash, 20 byte peer_id
	_, err = io.ReadFull(peer.conn, other)
	if err != nil {
		return
	}
	if !bytes.Equal(peer.Torrent.InfoHash[:], other[8:28]) {
		return errors.New("Unidentified infohash")
	}

	copy(peer.id[:], other[28:48])
	return nil

}

func (peer *Peer) StartCommunication() (err error) {
	defer peer.conn.Close()
	for {
		peer.conn.SetReadDeadline(time.Now().Add(time.Minute * 2))

		lenBuf := make([]byte, 4)
		_, err = io.ReadFull(peer.conn, lenBuf)
		if err != nil {
			return
		}
		peer.conn.SetReadDeadline(time.Time{}) // Reset deadline
		len := binary.BigEndian.Uint32(lenBuf)

		if len == 0 {
			continue
		}
		idBuf := make([]byte, 1)
		_, err = io.ReadFull(peer.conn, idBuf)
		if err != nil {
			return
		}
		id := idBuf[0]
		peer_msg := MessageFromID(id)
		err = peer_msg.Read(len, peer)
		if err != nil {
			return
		}
		err = peer_msg.Handle(peer)
		if err != nil {
			return
		}
	}
}

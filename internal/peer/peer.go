package peer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/kapilpokhrel/goTorr/internal/client"
	"github.com/kapilpokhrel/goTorr/internal/torrent"
)

type state struct {
	am_chocking    bool
	am_intrested   bool
	peer_chocking  bool
	peer_intrested bool
}
type Peer struct {
	Conn    net.Conn
	id      [20]byte
	state   state
	Torrent *torrent.Torrent
	Client  *client.Client
}

func (peer *Peer) setTorrent(torr *torrent.Torrent) {
	peer.Torrent = torr
}

func (peer *Peer) setClient(client *client.Client) {
	peer.Client = client
}

func (peer *Peer) establish_Conn(ip string, port uint16) (err error) {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		return
	}
	peer.Conn = conn

	peer.state = state{true, false, true, false}

	if peer.Torrent == nil {
		peer.Conn.Close()
		return errors.New(("No Torrents specified, can't send handshake"))
	}
	if peer.Client == nil {
		peer.Conn.Close()
		return errors.New("No client specified, can't send peer_id")
	}

	err = peer.sendHandShake()
	if err != nil {
		defer peer.Conn.Close()
		return fmt.Errorf("Handshake Error: %w", err)
	}
	err = peer.waitForHandShake()
	if err != nil {
		defer peer.Conn.Close()
		return fmt.Errorf("Handshake Error: %w", err)
	}

	return nil
}

func (peer *Peer) sendHandShake() error {
	var buf bytes.Buffer
	buf.WriteByte(19)
	buf.WriteString("BitTorrent Protocol")
	buf.Write(make([]byte, 8))          // reserved
	buf.Write(peer.Torrent.InfoHash[:]) // info_hash (20 bytes)
	buf.Write(peer.Client.PeerID[:])    // peer_id (20 bytes)
	_, err := peer.Conn.Write(buf.Bytes())
	return err
}

func (peer *Peer) waitForHandShake() (err error) {
	peer.Conn.SetReadDeadline(time.Now().Add(2 * time.Minute)) // Timeout of 2 minutes
	pstrlenBuf := make([]byte, 1)
	_, err = io.ReadFull(peer.Conn, pstrlenBuf)
	if err != nil {
		return
	}
	peer.Conn.SetReadDeadline(time.Time{}) // Reset deadline

	pstrlen := int(pstrlenBuf[0])
	pstrBuf := make([]byte, pstrlen)
	_, err = io.ReadFull(peer.Conn, pstrBuf)
	if err != nil {
		return
	}
	if string(pstrBuf) != "BitTorrent Protocol" {
		return errors.New(fmt.Sprintf("Unidentified Protocol, %s", string(pstrBuf)))
	}

	other := make([]byte, 48) // 8 byte reserved, 20 byte info_hash, 20 byte peer_id
	_, err = io.ReadFull(peer.Conn, other)
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
	defer peer.Conn.Close()
	for {
		peer.Conn.SetReadDeadline(time.Now().Add(time.Minute * 2))

		lenBuf := make([]byte, 4)
		_, err = io.ReadFull(peer.Conn, lenBuf)
		if err != nil {
			return err
		}
		peer.Conn.SetReadDeadline(time.Time{}) // Reset deadline
		len := binary.BigEndian.Uint32(lenBuf)

		if len == 0 {
			continue
		}
	}
}

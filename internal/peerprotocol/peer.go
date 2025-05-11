package peerprotocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"sync"
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
	mu                       sync.Mutex
	conn                     net.Conn
	id                       [20]byte
	state                    state
	bitfield                 []byte
	signalPeerChecker        chan bool
	stopPeer                 chan bool
	closeChan                chan string
	requested                int // requested data from peer
	lastMessageTime          time.Time
	lastNonKeepAliveResponse time.Time //For closing connection
	Torrent                  *torrent.Torrent
}

func NewPeer(currTorrent *torrent.Torrent, closeChan chan string) *Peer {
	peer := new(Peer)
	peer.Torrent = currTorrent
	peer.state = state{true, false, true, false}

	noOfPieces := math.Ceil(float64(peer.Torrent.TotalSize) / float64(peer.Torrent.PieceLength))
	peer.bitfield = make([]byte, int(math.Ceil(noOfPieces/8.0)))

	peer.signalPeerChecker = make(chan bool, 1)
	peer.stopPeer = make(chan bool, 1)
	peer.closeChan = closeChan

	return peer
}

func (peer *Peer) Establish_Conn(ipport string, clientID []byte) (err error) {
	conn, err := net.DialTimeout("tcp", ipport, 25*time.Second)
	if err != nil {
		return
	}
	peer.conn = conn

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
	buf.WriteString("BitTorrent protocol")
	buf.Write(make([]byte, 8))          // reserved
	buf.Write(peer.Torrent.InfoHash[:]) // info_hash (20 bytes)
	buf.Write(clientID)                 // peer_id (20 bytes)
	_, err := peer.conn.Write(buf.Bytes())

	return err
}

func (peer *Peer) waitForHandShake() (err error) {
	peer.conn.SetReadDeadline(time.Now().Add(25 * time.Second))
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
	if string(pstrBuf) != "BitTorrent protocol" {
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

	// Sending Bitfield ; we begin with empty bitfiled
	bitfield_msg := bitfield{
		bitfield: make([]byte, int(math.Ceil(float64(peer.Torrent.NoOfPieces)/8.0))),
	}
	err = bitfield_msg.Send(peer)
	if err != nil {
		return
	}
	peer.lastMessageTime = time.Now()
	peer.lastNonKeepAliveResponse = time.Now()
	return nil

}

func (peer *Peer) StartListening() {
	var err error
	for {
		err = peer.conn.SetReadDeadline(time.Now().Add(time.Minute * 2))
		if err != nil {
			peer.Close(err)
		}

		lenBuf := make([]byte, 4)
		_, err = io.ReadFull(peer.conn, lenBuf)
		if err != nil {
			// TODO: LOG error (maybe other way of handling error)
			peer.Close(err)
			return
		}
		err = peer.conn.SetReadDeadline(time.Time{}) // Reset deadline
		if err != nil {
			peer.Close(err)
		}

		peer.lastMessageTime = time.Now()

		len := binary.BigEndian.Uint32(lenBuf)

		if len == 0 {
			continue
		}
		buf := make([]byte, len)
		_, err = io.ReadFull(peer.conn, buf)
		if err != nil {
			peer.Close(err)
			return
		}

		peer_msg, err := MsgParse(buf)
		if err != nil {
			peer.Close(err)
			return
		}
		err = peer_msg.Handle(peer)
		if err != nil {
			peer.Close(err)
			return
		}
		peer.lastNonKeepAliveResponse = time.Now()

	}
}

func (peer *Peer) PeerChecker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
		case <-peer.signalPeerChecker:
		case <-peer.stopPeer:
			return
		}

		peer.mu.Lock()

		if peer.lastNonKeepAliveResponse.Add(5 * time.Minute).Before(time.Now()) {
			peer.Close(nil)
			peer.mu.Unlock()
			continue
		}

		index := peer.Torrent.GetRarestPieceIndex(peer.bitfield,
			peer.conn.RemoteAddr().String(),
		)
		if index >= 0 {
			var err error
			if peer.requested != 0 {
				peer.mu.Unlock()
				continue
			}
			if !peer.state.am_intrested {
				intrestedMessage := intrested{}
				err = intrestedMessage.Send(peer)
			} else if peer.state.peer_chocking {
				peer.mu.Unlock()
				continue
			} else {
				fmt.Printf("Requesting piece %d from %s\n", index, peer.conn.RemoteAddr().String())
				blocks_requested := 0
				for _, block := range peer.Torrent.GetRequiredBlocks(index) {
					request_message := request{
						index:  uint32(index),
						begin:  block[0],
						length: block[1],
					}
					err = request_message.Send(peer)
					if err == nil {
						blocks_requested++
					}
				}
				fmt.Printf("Requested %d blocks of piece %d from %s\n",
					blocks_requested, index,
					peer.conn.RemoteAddr().String(),
				)
				peer.requested = blocks_requested
			}
			if err == nil {
				peer.mu.Unlock()
				continue
			}
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				peer.mu.Unlock()
				continue
			} else {
				peer.Close(err)
			}
		}
		peer.mu.Unlock()
		if peer.lastMessageTime.Add(2 * time.Minute).Before(time.Now()) {
			peer.SendKeepAlive()
		}
	}

}

func (peer *Peer) Close(err error) {
	if !errors.Is(err, net.ErrClosed) {
		defer peer.conn.Close()
	}
	peer.stopPeer <- true
	peer.closeChan <- peer.conn.RemoteAddr().String()
}

func (peer *Peer) SendKeepAlive() error {
	_, err := peer.conn.Write([]byte{0})
	return err
}

func (peer *Peer) SendMessage(id uint8, buffer []byte) error {
	var buf bytes.Buffer

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(buffer)+1))
	buf.Write(lenBuf)
	buf.WriteByte(id)
	buf.Write(buffer)

	_, err := peer.conn.Write(buf.Bytes())
	if err == nil {
		peer.lastMessageTime = time.Now()
		//fmt.Printf("Sent id %d, data %v\n", id, buffer)
	}
	return err
}

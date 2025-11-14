// Package peerprotocol handler management of active peers, communication with peer and messae parsing
package peerprotocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"sync"
	"time"

	"goTorr/internal/torrent"
)

type state struct {
	amChoking     bool
	amIntrested   bool
	peerChoking   bool
	peerIntrested bool
}

type Peer struct {
	mu                       sync.Mutex
	addr                     string
	conn                     net.Conn
	id                       [20]byte
	state                    state
	bitfield                 []byte
	signalPeerChecker        chan bool
	stopPeer                 chan bool
	closeChan                chan string
	requested                int // requested data from peer
	lastMessageTime          time.Time
	lastNonKeepAliveResponse time.Time // For closing connection
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

func (peer *Peer) EstablishConn(ipport string, clientID []byte) (err error) {
	conn, err := net.DialTimeout("tcp", ipport, 25*time.Second)
	if err != nil {
		return
	}
	peer.conn = conn
	peer.addr = peer.conn.RemoteAddr().String()

	err = peer.sendHandShake(clientID)
	if err != nil {
		defer peer.conn.Close()
		return fmt.Errorf("handshake Error: %w", err)
	}
	err = peer.waitForHandShake()
	if err != nil {
		defer peer.conn.Close()
		return fmt.Errorf("handshake Error: %w", err)
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
		return fmt.Errorf("unidentified Protocol, %s", string(pstrBuf))
	}

	other := make([]byte, 48) // 8 byte reserved, 20 byte info_hash, 20 byte peer_id
	_, err = io.ReadFull(peer.conn, other)
	if err != nil {
		return
	}
	if !bytes.Equal(peer.Torrent.InfoHash[:], other[8:28]) {
		return errors.New("unidentified infohash")
	}

	copy(peer.id[:], other[28:48])

	// Sending Bitfield ; we begin with empty bitfiled
	bitfieldMsg := bitfield{
		bitfield: make([]byte, int(math.Ceil(float64(peer.Torrent.NoOfPieces)/8.0))),
	}
	err = bitfieldMsg.Send(peer)
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

		peerMsg, err := MsgParse(buf)
		if err != nil {
			peer.Close(err)
			return
		}
		go peerMsg.Handle(peer)
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

		if peer.lastNonKeepAliveResponse.Add(2 * time.Minute).Before(time.Now()) {
			peer.Close(nil)
			peer.mu.Unlock()
			continue
		}

		if peer.requested > 30 {
			peer.mu.Unlock()
			continue
		}

		index := peer.Torrent.GetRarestPieceIndex(peer.bitfield,
			peer.addr,
		)

		if index >= 0 {
			var err error

			if !peer.state.amIntrested {
				intrestedMessage := intrested{}
				err = intrestedMessage.Send(peer)
			} else if peer.state.peerChoking {
				peer.mu.Unlock()
				continue
			} else {
				blocksRequested := 0
				for _, block := range peer.Torrent.GetRequiredBlocks(index) {
					requestMsg := request{
						index:  uint32(index),
						begin:  block[0],
						length: block[1],
					}
					err = requestMsg.Send(peer)
					if err == nil {
						blocksRequested++
					} else {
						slog.Error(err.Error())
					}
				}
				if blocksRequested > 0 {
					slog.Debug(fmt.Sprintf(
						"Requested %d blocks of piece %d from %s\n",
						blocksRequested, index,
						peer.addr,
					))
					peer.requested += blocksRequested
				}
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
		if peer.lastMessageTime.Add(2 * time.Second).Before(time.Now()) {
			peer.SendKeepAlive()
		}
	}
}

func (peer *Peer) Close(err error) {
	if err != nil {
		slog.Error(fmt.Sprintf("Closing peer, %v, error = %v\n", peer.addr, err))
	} else {
		slog.Info(fmt.Sprintf("Closing Peer, %v", peer.addr))
	}
	if peer.conn != nil {
		err := peer.conn.Close()
		if err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Error(fmt.Sprintf("Error in closing peer %v, %v\n", peer.addr, err))
		}
	}
	peer.stopPeer <- true
	select {
	case peer.closeChan <- peer.addr:
	default:
	}
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
	}
	return err
}

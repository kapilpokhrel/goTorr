package peerprotocol

import (
	"fmt"
	"sync"
	"time"

	"goTorr/internal/torrent"
)

type PeerManager struct {
	mu      sync.Mutex
	peers   map[string]*Peer
	torrent *torrent.Torrent
	closeCh chan string
	peerID  [20]byte
}

func NewPeerManager(t *torrent.Torrent, peerID [20]byte, closeCh chan string) *PeerManager {
	return &PeerManager{
		peers:   make(map[string]*Peer),
		torrent: t,
		closeCh: closeCh,
		peerID:  peerID,
	}
}

func (pm *PeerManager) AddPeer(addr string) {
	pm.mu.Lock()
	if _, exists := pm.peers[addr]; exists {
		pm.mu.Unlock()
		return // Already connected
	}
	peer := NewPeer(pm.torrent, pm.closeCh)
	pm.peers[addr] = peer
	pm.mu.Unlock()

	err := peer.Establish_Conn(addr, pm.peerID[:])
	if err != nil {
		pm.mu.Lock()
		delete(pm.peers, addr)
		pm.mu.Unlock()
		fmt.Printf("Error adding %s: %v\n", addr, err)
		return
	}

	go peer.PeerChecker(time.Minute)
	go peer.StartListening()
	fmt.Printf("Peer %s added.\n", addr)
}

func (pm *PeerManager) RemovePeer(addr string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.peers, addr)
}

func (pm *PeerManager) AddPeers(addrs []string) {
	var wg sync.WaitGroup
	for _, addr := range addrs {
		wg.Add(1)
		go func(a string) {
			defer wg.Done()
			pm.AddPeer(a)
		}(addr)
	}
	wg.Wait()
}

func (pm *PeerManager) Count() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return len(pm.peers)
}

func (pm *PeerManager) Clear() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, p := range pm.peers {
		p.Close(nil)
	}
	clear(pm.peers)
}

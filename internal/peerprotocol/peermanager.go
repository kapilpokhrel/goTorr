package peerprotocol

import (
	"fmt"
	"sync"
	"time"

	"goTorr/internal/torrent"
)

type PeerManager struct {
	mu       sync.Mutex
	peers    map[string]*Peer
	peerList map[string]bool
	torrent  *torrent.Torrent
	closeCh  chan string
	exitCh   chan bool
	peerID   [20]byte
}

func NewPeerManager(t *torrent.Torrent, peerID [20]byte, closeCh chan string) *PeerManager {
	return &PeerManager{
		peers:    make(map[string]*Peer),
		peerList: make(map[string]bool, 60),
		torrent:  t,
		closeCh:  closeCh,
		peerID:   peerID,
		exitCh:   make(chan bool),
	}
}

func (pm *PeerManager) UpdatePeerList(peerList []string) {
	pm.mu.Lock()
	for _, peerAddr := range peerList {
		_, exists := pm.peerList[peerAddr]
		if !exists {
			pm.peerList[peerAddr] = true
		}
	}
	pm.mu.Unlock()
}

func (pm *PeerManager) PeerListCount() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return len(pm.peerList)
}

func (pm *PeerManager) addPeer(addr string) {
	pm.mu.Lock()
	if _, exists := pm.peers[addr]; exists {
		pm.mu.Unlock()
		return // Already connected
	}
	fmt.Printf("Adding peer %s\n", addr)
	peer := NewPeer(pm.torrent, pm.closeCh)
	pm.peers[addr] = peer
	pm.mu.Unlock()

	err := peer.Establish_Conn(addr, pm.peerID[:])
	if err != nil {
		pm.RemovePeer(addr)
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
	delete(pm.peerList, addr)

}

func (pm *PeerManager) AddPeers() {
	var wg sync.WaitGroup

	maxAdd := 30
	semaphore := make(chan struct{}, maxAdd)

	pm.mu.Lock()
	// It is just simple to create another peerlist so we don't get
	// concurrent iteration and writes
	peerList := make([]string, 0, len(pm.peerList))
	for addr := range pm.peerList {
		peerList = append(peerList, addr)
	}
	pm.mu.Unlock()
	for _, addr := range peerList {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(a string) {
			defer wg.Done()
			defer func() { <- semaphore }()

			pm.mu.Lock()
			pm.peerList[a] = true
			pm.mu.Unlock()
			pm.addPeer(a)
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

func (pm *PeerManager) Start() {
	pm.AddPeers()
	ticker := time.NewTicker(1 * time.Minute)
	for {
		select {
		case <-pm.exitCh:
			return
		case <-ticker.C:
			if pm.Count() < 30 {
				fmt.Printf("Active peers count = %d (< 30), trying to add new pair\n", pm.Count())
				pm.AddPeers()
			}
		}
	}
}

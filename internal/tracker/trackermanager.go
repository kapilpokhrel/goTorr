package tracker

import (
	"fmt"
	"goTorr/internal/torrent"
	"time"
)

type peerManager interface {
	UpdatePeerList([]string)
	Count() int
	PeerListCount() int
}

type Tracker struct {
	trackerId       string
	interval        time.Duration
	minInterval     time.Duration
	lastRequestTime time.Time
}

type TrackerManager struct {
	announceURLS  []string
	activeTracker map[string]*Tracker
	currTorrent   *torrent.Torrent
	peerID        [20]byte
	pm            peerManager
}

func NewTrackerManager(
	announceURLS []string,
	pm peerManager,
	currTorrent *torrent.Torrent,
	peerID [20]byte,
) *TrackerManager {
	tm := new(TrackerManager)
	tm.announceURLS = make([]string, len(announceURLS))
	copy(tm.announceURLS, announceURLS)
	tm.pm = pm
	tm.activeTracker = make(map[string]*Tracker, len(announceURLS))
	tm.currTorrent = currTorrent
	tm.peerID = peerID
	return tm
}

func (tm *TrackerManager) Start() {
	peers := make([]string, 0)
	for _, url := range tm.announceURLS {
		_, exits := tm.activeTracker[url]
		if exits {
			continue
		}

		fmt.Printf("Announcing on %s\n", url)
		tracker := new(Tracker)
		plist, err := tracker.SendTrackerAnnounce(url, tm.currTorrent, tm.peerID[:])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}
		tm.activeTracker[url] = tracker
		peers = append(peers, plist...)
		if len(peers) > 55 {
			// We have got enough peers for us now
			break
		}
	}
	tm.pm.UpdatePeerList(peers)
}

// TODO: Add tickers in each tracker for regular tracker Update
// Need to add force tracker request when peerListCount drops low than some threshold

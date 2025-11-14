package tracker

import (
	"fmt"
	"log/slog"
	"sync"

	"goTorr/internal/torrent"
)

type peerManager interface {
	UpdatePeerList([]string)
	Count() int
	PeerListCount() int
}

type TrackerManager struct {
	mu            sync.Mutex
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
	for _, url := range tm.announceURLS {
		_, exits := tm.activeTracker[url]
		if exits {
			continue
		}

		slog.Debug(fmt.Sprintf("Announcing on %s\n", url))
		tracker := new(Tracker)
		tracker.url = url
		plist, err := tracker.SendTrackerAnnounce(tm.currTorrent, tm.peerID[:])
		if err != nil {
			slog.Debug(fmt.Sprintf("Announce failed on %s, error = %v", url, err))
			continue
		}
		tm.mu.Lock()
		tm.activeTracker[url] = tracker
		tm.mu.Unlock()

		go tracker.startPeriodicUpdate(tracker.interval, tm.currTorrent, tm.peerID[:], tm.pm)
		slog.Debug(fmt.Sprintf("Started periodic announce for %s in interval %v", url, tracker.interval))
		tm.pm.UpdatePeerList(plist)
	}
}

func (tm *TrackerManager) Stop() {
	for url, tracker := range tm.activeTracker {
		tm.mu.Lock()
		tracker.stopTracker <- true
		delete(tm.activeTracker, url)
		tm.mu.Unlock()
	}
}

// Need to add force tracker request when peerListCount drops low than some threshold

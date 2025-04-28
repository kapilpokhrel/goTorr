package main

import (
	"fmt"
	"goTorr/internal/client"
	"goTorr/internal/metadata"
	"goTorr/internal/peerprotocol"
	"goTorr/internal/torrent"
	"goTorr/internal/tracker"
	"sync"
	"time"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func main() {
	// Metadata .torrent parsing
	var mdata metadata.Metadata
	err := mdata.GetMetadata("/mnt/exthome/dev_tmp/goTorr/thermo.torrent")
	check(err)
	err = mdata.Parse()
	check(err)

	// Torrent
	info_hash, err := mdata.GetInfoHash()
	if err != nil {
		panic(err)
	}

	torrentinfoChan := make(chan int)
	currtorrent, err := torrent.NewTorrent(
		info_hash,
		mdata.TotalSize,
		uint64(mdata.PieceLength),
		mdata.Files,
		mdata.FileOrder,
		torrentinfoChan,
	)
	if err != nil {
		panic(err)
	}

	//CLient
	client := client.NewClient()

	// Announce
	peers, err := tracker.SendTrackerAnnounce(mdata.AnnounceUrls[1], currtorrent, client.PeerID[:])

	/*peers := []string{
		"95.158.11.9:33827",
		"41.251.230.76:40385",
		"47.154.58.147:30766",
		"41.251.230.76:46441",
		"41.251.230.76:40899",
		"80.177.32.15:8999",
	}*/

	peercloseChan := make(chan string)
	exitChan := make(chan int)
	peerMap := make(map[string]*peerprotocol.Peer)
	var wg sync.WaitGroup

	infoListener := func() {
		defer wg.Done()
		for {
			select {
			case <-exitChan:
				return
			case peerAddr := <-peercloseChan:
				fmt.Printf("Peer %s closed.\n", peerAddr)
				delete(peerMap, peerAddr)
			case torrinfo := <-torrentinfoChan:
				switch torrinfo {
				case torrent.InfoCompleted:
					fmt.Println("Completed")
					currtorrent.Close()
					clear(peerMap)
					return
				case torrent.InfoPieceComplete:
					pieceindex := <-torrentinfoChan
					fmt.Printf("Piece %d complete.\n", pieceindex)
				}
			}
		}
	}

	peerAdder := func() {

		for _, peerAddr := range peers {
			peer := peerprotocol.NewPeer(currtorrent, peercloseChan)
			err = peer.Establish_Conn(peerAddr, client.PeerID[:])
			if err != nil {
				fmt.Println(err)
				continue
			}
			go peer.PeerChecker(time.Minute)
			go peer.StartListening()
			peerMap[peerAddr] = peer
			fmt.Printf("Peer %s Added.\n", peerAddr)

		}
		if len(peerMap) == 0 {
			fmt.Println("No Peers found")
			exitChan <- 1
		}
	}
	wg.Add(1)
	go infoListener()
	go peerAdder()

	wg.Wait()
	return
}

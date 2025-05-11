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
	//err := mdata.GetMetadata("/mnt/exthome/dev_tmp/goTorr/cowboyBebop.torrent")
	err := mdata.GetMetadata("/home/kapil/Downloads/medicalimgDL.torrent")
	check(err)
	err = mdata.Parse()
	check(err)
	
	mdata.Print()
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
	peers := make([]string, 0)
	for _, url := range mdata.AnnounceUrls {
		fmt.Printf("Announcing on %s\n", url)
		plist, err := tracker.SendTrackerAnnounce(url, currtorrent, client.PeerID[:])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}
		peers = append(peers, plist...)
		if len(peers) > 20 {
			// We have got enough peers for us now
			break
		}
	}
	
	if err != nil {
		panic(err)
	} 

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
				if len(peerMap) == 0 {
					fmt.Printf("No active peers, exitting for now...")
					return
				}
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
				case torrent.InfoFileComplete:
					fileindex := <-torrentinfoChan
					fmt.Printf("File %s complete.\n", mdata.FileOrder[fileindex])
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

package main

import (
	"fmt"
	"goTorr/internal/client"
	"goTorr/internal/metadata"
	"goTorr/internal/peerprotocol"
	"goTorr/internal/torrent"
	"goTorr/internal/tracker"
	"sync"
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
	err := mdata.GetMetadata("/home/kapil/Downloads/mpeg7Shape1.torrent")
	check(err)
	err = mdata.Parse()
	check(err)

	mdata.Print()
	// Torrent
	info_hash, err := mdata.GetInfoHash()
	if err != nil {
		check(err)
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

	peercloseChan := make(chan string)
	exitChan := make(chan int)
	peerManager := peerprotocol.NewPeerManager(currtorrent, client.PeerID, peercloseChan)
	var wg sync.WaitGroup

	infoListener := func() {
		defer wg.Done()
		for {
			select {
			case <-exitChan:
				return
			case peerAddr := <-peercloseChan:
				fmt.Printf("Peer %s closed.\n", peerAddr)
				peerManager.RemovePeer(peerAddr)

				if peerManager.Count() == 0 {
					fmt.Println("No active peers left, exiting for now...")
					exitChan <- 1
					return
				}
			case torrinfo := <-torrentinfoChan:
				switch torrinfo {
				case torrent.InfoCompleted:
					fmt.Println("Completed")
					currtorrent.Close()
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

	wg.Add(1)
	go infoListener()
	go func() {
		peerManager.AddPeers(peers)
		if peerManager.Count() == 0 {
			fmt.Println("No Peers found, exitting for now...")
			exitChan <- 1
		}
	}()

	wg.Wait()
	peerManager.Clear()
	return
}

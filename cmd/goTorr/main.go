package main

import (
	"fmt"
	"os"
	"sync"

	"goTorr/internal/client"
	"goTorr/internal/metadata"
	"goTorr/internal/peerprotocol"
	"goTorr/internal/torrent"
	"goTorr/internal/tracker"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func main() {
	args := os.Args
	if len(args) <= 1 {
		panic(fmt.Errorf("expected command line argument for torrent file to download"))
	}
	// Metadata .torrent parsing
	var mdata metadata.Metadata
	err := mdata.GetMetadata(args[1])
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

	// CLient
	client := client.NewClient()

	peercloseChan := make(chan string)
	exitChan := make(chan int)
	peerManager := peerprotocol.NewPeerManager(currtorrent, client.PeerID, peercloseChan)
	trackerManager := tracker.NewTrackerManager(
		mdata.AnnounceUrls,
		peerManager,
		currtorrent,
		client.PeerID,
	)
	// Announce
	trackerManager.Start()

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
	go peerManager.Start()

	wg.Wait()
	peerManager.Clear()
	return
}

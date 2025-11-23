package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"goTorr/internal/client"
	"goTorr/internal/metadata"
	"goTorr/internal/peerprotocol"
	"goTorr/internal/torrent"
	"goTorr/internal/tracker"

	"github.com/lmittmann/tint"
)

func check(e error) {
	if e != nil {
		slog.Error(e.Error())
		panic(e)
	}
}

func setupLogger() {
	stdHandler := tint.NewHandler(os.Stdout, &tint.Options{Level: slog.LevelInfo})
	logger := slog.New(stdHandler)
	slog.SetDefault(logger)
}

func main() {
	setupLogger()

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
	infoHash, err := mdata.GetInfoHash()
	check(err)

	torrentinfoChan := make(chan int)
	currtorrent, err := torrent.NewTorrent(
		infoHash,
		mdata.TotalSize,
		uint64(mdata.PieceLength),
		mdata.Files,
		mdata.FileOrder,
		torrentinfoChan,
	)
	check(err)

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
	go trackerManager.Start()

	var wg sync.WaitGroup

	infoListener := func() {
		defer wg.Done()
		for {
			select {
			case <-exitChan:
				return
			case peerAddr := <-peercloseChan:
				slog.Debug(fmt.Sprintf("Peer %s closed.\n", peerAddr))
				peerManager.RemovePeer(peerAddr)

				if peerManager.Count() == 0 {
					slog.Info("No active peers left, exiting for now...")
					peerManager.Close()
					currtorrent.Close()
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

					fmt.Printf(
						"Piece %d complete. (%d/%d) %f %%. Active Peers: %d\n",
						pieceindex, currtorrent.CompletedPieces,
						len(mdata.Pieces),
						float64(currtorrent.Downloaded)/float64(currtorrent.TotalSize)*100.0,
						peerManager.ActiveCount(),
					)
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
}

package torrent

import (
	"math"
	"sync"
)

var InfoNoRequiredPieceInBitfield int = -1
var InfoCompleted int = -2
var InfoPieceComplete int = -3 // Followed by piece index

type Piece struct {
	completed bool
	blocks    []bool
	requested []string
}

type Torrent struct {
	mu              sync.Mutex
	InfoHash        [20]byte
	TotalSize       uint64
	Downloaded      uint64
	Uploaded        uint64
	NoOfPieces      uint64
	pieces          []Piece
	BlockSize       uint16
	PieceLength     uint64
	torrentinfoChan chan int
}

func emptyPiece(piecelength uint64, blocksize uint16) Piece {
	return Piece{
		completed: false,
		blocks:    make([]bool, int(math.Ceil(float64(piecelength)/float64(blocksize)))),
		requested: make([]string, 0),
	}
}
func NewTorrent(
	infohash [20]byte,
	totalsize uint64,
	piecelength uint64,
	torrentinfoChan chan int,
) *Torrent {
	noOfPieces := int(math.Ceil(float64(totalsize) / float64(piecelength)))
	currentTorrent := Torrent{
		InfoHash:        infohash,
		TotalSize:       totalsize,
		Downloaded:      0,
		Uploaded:        0,
		pieces:          make([]Piece, 0, noOfPieces),
		PieceLength:     piecelength,
		BlockSize:       uint16(min(uint64(math.Pow(2, 14)), piecelength)), //Blocksize is 2^14
		NoOfPieces:      uint64(noOfPieces),
		torrentinfoChan: torrentinfoChan,
	}
	for i := 0; i < noOfPieces; i++ {
		currentTorrent.pieces = append(
			currentTorrent.pieces,
			emptyPiece(min(piecelength, totalsize-piecelength*uint64(i)), currentTorrent.BlockSize))
	}
	return &currentTorrent
}

func (currTorrent *Torrent) GetRarestPieceIndex(bitfield []byte) int {
	if currTorrent.Downloaded == currTorrent.TotalSize {
		return InfoCompleted
	}

	lastUncompletePiece := InfoNoRequiredPieceInBitfield
	for index, piece := range currTorrent.pieces {
		if piece.completed {
			continue
		}
		if !isPieceSet(bitfield, index) {
			continue
		}
		if len(piece.requested) > 0 {
			lastUncompletePiece = index
		}
		return index
	}

	// All pieces are requested
	return lastUncompletePiece
}

func (currTorrent *Torrent) GetRequiredBlocks(pieceindex int) (requiredBlocks [][2]uint32) {
	piecelength := min(
		currTorrent.PieceLength,
		currTorrent.TotalSize-currTorrent.PieceLength*uint64(pieceindex),
	)
	noofBlocks := int(math.Ceil(float64(piecelength) / min(float64(currTorrent.BlockSize))))
	requiredBlocks = make([][2]uint32, 0, noofBlocks)

	for index, have := range currTorrent.pieces[pieceindex].blocks {
		if !have {
			requiredBlocks = append(requiredBlocks, [2]uint32{
				uint32(int(currTorrent.BlockSize) * index),
				uint32(min(
					currTorrent.BlockSize,
					uint16(piecelength-uint64(index)*uint64(currTorrent.BlockSize)),
				)),
			})
		}
	}
	return
}

func (currTorrent *Torrent) WriteBlock(index, begin uint32, piecedata []byte) {
	blockIndex := uint64(begin) / uint64(currTorrent.BlockSize)
	currTorrent.mu.Lock()
	if currTorrent.pieces[index].blocks[blockIndex] {
		currTorrent.mu.Unlock()
		return
	}
	currTorrent.pieces[index].blocks[blockIndex] = true
	currTorrent.Downloaded += uint64(len(piecedata))
	currTorrent.mu.Unlock()

	// Check if piece is complete
	if all(currTorrent.pieces[index].blocks) {
		currTorrent.pieces[index].completed = true

		currTorrent.torrentinfoChan <- InfoPieceComplete
		currTorrent.torrentinfoChan <- int(index)
		// Do other things like file writing and other
	}

	if currTorrent.Downloaded == currTorrent.TotalSize {
		// Download Complete
		currTorrent.torrentinfoChan <- InfoCompleted
	}
}

func all(slice []bool) bool {
	for _, v := range slice {
		if !v {
			return false
		}
	}
	return true
}

func isPieceSet(bitfield []byte, index int) bool {
	byteIndex := index / 8
	bitOffset := 7 - (index % 8)

	if byteIndex >= len(bitfield) {
		return false // index out of range
	}

	return bitfield[byteIndex]&(1<<bitOffset) != 0
}

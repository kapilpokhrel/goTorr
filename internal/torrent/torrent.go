package torrent

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
)

var InfoNoRequiredPieceInBitfield int = -1
var InfoCompleted int = -2
var InfoPieceComplete int = -3 // Followed by piece index
var InfoFileComplete int = -4  // Followed by file index

type Piece struct {
	completed bool
	blocks    []bool
	requested []string
}

type file struct {
	size    int64
	written int64
	filep   *os.File
}

type Torrent struct {
	mu              sync.Mutex
	wg              sync.WaitGroup
	InfoHash        [20]byte
	TotalSize       uint64
	Downloaded      uint64
	Uploaded        uint64
	NoOfPieces      uint64
	pieces          []*Piece
	files           map[string]*file // file -> filesize
	fileorder       []string
	BlockSize       uint16
	PieceLength     uint64
	torrentinfoChan chan int
}

// TODO: Make torrent info private and write getter in torrent info.
// Helps block the unnecessary chaning of any info by other package

func emptyPiece(piecelength uint64, blocksize uint16) *Piece {
	return &Piece{
		completed: false,
		blocks:    make([]bool, int(math.Ceil(float64(piecelength)/float64(blocksize)))),
		requested: make([]string, 0),
	}
}

func createFileWithDirectory(path string) (*os.File, error) {
	dir := filepath.Dir(path)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0755)
		if err != nil {
			return nil, fmt.Errorf("error creating directory: %w", err)
		}
	}

	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("error creating file: %w", err)
	}

	return file, nil
}

func NewTorrent(
	infohash [20]byte,
	totalsize uint64,
	piecelength uint64,
	files map[string]int64,
	fileorder []string,
	torrentinfoChan chan int,
) (*Torrent, error) {
	noOfPieces := int(math.Ceil(float64(totalsize) / float64(piecelength)))
	currentTorrent := Torrent{
		InfoHash:        infohash,
		TotalSize:       totalsize,
		Downloaded:      0,
		Uploaded:        0,
		pieces:          make([]*Piece, 0, noOfPieces),
		files:           make(map[string]*file),
		fileorder:       make([]string, len(fileorder)),
		PieceLength:     piecelength,
		BlockSize:       uint16(min(uint64(math.Pow(2, 14)), piecelength)), //Blocksize is 2^14
		NoOfPieces:      uint64(noOfPieces),
		torrentinfoChan: torrentinfoChan,
	}

	// files
	for k, v := range files {
		filep, err := createFileWithDirectory(k)
		if err != nil {
			return nil, err
		}
		err = filep.Truncate(v)
		if err != nil {
			return nil, err
		}

		currentTorrent.files[k] = &file{v, 0, filep}
		// NOTE: We are supposed to close the file after it is completely written
	}
	copy(currentTorrent.fileorder, fileorder)

	// pieces
	for i := 0; i < noOfPieces; i++ {
		currentTorrent.pieces = append(
			currentTorrent.pieces,
			emptyPiece(min(piecelength, totalsize-piecelength*uint64(i)), currentTorrent.BlockSize),
		)
	}
	return &currentTorrent, nil
}

func (currTorrent *Torrent) GetRarestPieceIndex(bitfield []byte, peerAddr string) int {
	currTorrent.mu.Lock()
	defer currTorrent.mu.Unlock()
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
			continue
		}
		currTorrent.pieces[index].requested = append(currTorrent.pieces[index].requested,
			peerAddr,
		)
		return index
	}

	// All pieces are requested
	return lastUncompletePiece
}

func (currTorrent *Torrent) GetRequiredBlocks(pieceindex int) (requiredBlocks [][2]uint32) {
	currTorrent.mu.Lock()
	defer currTorrent.mu.Unlock()
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
					uint64(currTorrent.BlockSize),
					piecelength-uint64(index)*uint64(currTorrent.BlockSize),
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
	currTorrent.wg.Add(1)
	go currTorrent.writeBlocktoFile(index, begin, piecedata)

	// Check if piece is complete
	if all(currTorrent.pieces[index].blocks) {
		currTorrent.pieces[index].completed = true

		currTorrent.mu.Lock()
		currTorrent.torrentinfoChan <- InfoPieceComplete
		currTorrent.torrentinfoChan <- int(index)
		currTorrent.mu.Unlock()
	}

	if currTorrent.Downloaded == currTorrent.TotalSize {
		// Download Complete
		currTorrent.wg.Wait()
		currTorrent.mu.Lock()
		currTorrent.torrentinfoChan <- InfoCompleted
		currTorrent.mu.Unlock()
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

func (currTorrent *Torrent) writeBlocktoFile(index, begin uint32, piecedata []byte) {
	defer currTorrent.wg.Done()
	absolute_offset := int64(index)*int64(currTorrent.PieceLength) + int64(begin)
	infileOffset := absolute_offset

	dataoffset := int64(0)
	remainingPiecedata := int64(len(piecedata))
	for file_index, filepath := range currTorrent.fileorder {
		file := currTorrent.files[filepath]
		if infileOffset >= file.size {
			infileOffset -= file.size
			continue
		}
		// Found the file
		remainingFileSpace := file.size - infileOffset

		toWrite := min(remainingPiecedata, remainingFileSpace)

		file.filep.WriteAt(
			piecedata[dataoffset:dataoffset+toWrite],
			int64(infileOffset),
		)
		file.written += toWrite

		if file.written == file.size {
			file.filep.Sync()
			currTorrent.mu.Lock()
			currTorrent.torrentinfoChan <- InfoFileComplete
			currTorrent.torrentinfoChan <- file_index
			currTorrent.mu.Unlock()
		}

		if toWrite < remainingPiecedata {
			dataoffset += toWrite
			remainingPiecedata -= toWrite
			infileOffset = 0

			continue
		}
		break

	}
	return
}

func (currTorrent *Torrent) Close() {
	currTorrent.wg.Wait()
	for _, v := range currTorrent.files {
		v.filep.Close()
	}
	close(currTorrent.torrentinfoChan)
}

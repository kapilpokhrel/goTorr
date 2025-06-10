package torrent

import (
	b64 "encoding/base64"
	"encoding/json"
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

func (p *Piece) MarshalJSON() ([]byte, error) {
	type pieceAlias struct {
		Completed bool
		Blocks    []bool
		Requested []string
	}

	alias := pieceAlias{
		Completed: p.completed,
		Blocks:    p.blocks,
		Requested: p.requested,
	}
	b, err := json.Marshal(alias)
	return b, err
}

func (p *Piece) UnmarshalJSON(inp []byte) error {
	type pieceAlias struct {
		Completed bool
		Blocks    []bool
		Requested []string
	}

	var alias pieceAlias
	err := json.Unmarshal(inp, &alias)
	if err != nil {
		return nil
	}
	p.completed = alias.Completed
	p.blocks = alias.Blocks
	p.requested = alias.Requested
	return err
}

type file struct {
	size    int64
	written int64
	filep   *os.File
}

func (f *file) MarshalJSON() ([]byte, error) {
	type fileAlias struct {
		Size    int64
		Written int64
	}

	alias := fileAlias{
		Size:    f.size,
		Written: f.written,
	}
	b, err := json.Marshal(alias)
	return b, err
}

func (f *file) UnmarshalJSON(inp []byte) error {
	type fileAlias struct {
		Size    int64
		Written int64
	}

	var alias fileAlias
	err := json.Unmarshal(inp, &alias)
	if err != nil {
		return nil
	}
	f.size = alias.Size
	f.written = alias.Written
	return err
}

type Torrent struct {
	mu              sync.Mutex     `json:"-"`
	wg              sync.WaitGroup `json:"-"`
	InfoHash        [20]byte
	TotalSize       uint64
	Downloaded      uint64
	Uploaded        uint64
	NoOfPieces      uint64
	Pieces          []*Piece
	Files           map[string]*file // file -> filesize
	Fileorder       []string
	BlockSize       uint16
	PieceLength     uint64
	torrentinfoChan chan int `json:"-"`
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

func isSaved(infohash [20]byte) bool {
	fname := fmt.Sprintf(
		"%s.gotorr",
		b64.StdEncoding.EncodeToString(infohash[:]),
	)
	if _, err := os.Stat(fname); os.IsNotExist(err) {
		return false
	} else {
		return true
	}
}

func (currTorrent *Torrent) save() error {
	defer currTorrent.wg.Done()

	fname := fmt.Sprintf(
		"%s.gotorr",
		b64.StdEncoding.EncodeToString(currTorrent.InfoHash[:]),
	)

	b, err := json.Marshal(currTorrent)
	if err != nil {
		return err
	}
	err = os.WriteFile(fname, b, 0644)
	return err
}

func (currTorrent *Torrent) load() error {
	fname := fmt.Sprintf(
		"%s.gotorr",
		b64.StdEncoding.EncodeToString(currTorrent.InfoHash[:]),
	)

	b, err := os.ReadFile(fname)
	if err != nil {
		return err
	}
	err = json.Unmarshal(b, currTorrent)
	if err != nil {
		return err
	}
	for k := range currTorrent.Files {
		filep, err := os.OpenFile(fname, os.O_APPEND, os.ModeAppend)
		if err != nil {
			return err
		}
		currTorrent.Files[k].filep = filep
		// NOTE: We are supposed to close the file after it is completely written
	}
	return nil
}

func (currTorrent *Torrent) deleteSave() error {
	fname := fmt.Sprintf(
		"%s.gotorr",
		b64.StdEncoding.EncodeToString(currTorrent.InfoHash[:]),
	)
	err := os.Remove(fname)
	return err
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
		Pieces:          make([]*Piece, 0, noOfPieces),
		Files:           make(map[string]*file),
		Fileorder:       make([]string, len(fileorder)),
		PieceLength:     piecelength,
		BlockSize:       uint16(min(uint64(math.Pow(2, 14)), piecelength)), //Blocksize is 2^14
		NoOfPieces:      uint64(noOfPieces),
		torrentinfoChan: torrentinfoChan,
	}

	if isSaved(infohash) {
		fmt.Println("Resuming from saved state...")
		currentTorrent.load()
		return &currentTorrent, nil
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

		currentTorrent.Files[k] = &file{v, 0, filep}
		// NOTE: We are supposed to close the file after it is completely written
	}
	copy(currentTorrent.Fileorder, fileorder)

	// pieces
	for i := range noOfPieces {
		currentTorrent.Pieces = append(
			currentTorrent.Pieces,
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
	for index, piece := range currTorrent.Pieces {
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
		currTorrent.Pieces[index].requested = append(currTorrent.Pieces[index].requested,
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

	for index, have := range currTorrent.Pieces[pieceindex].blocks {
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
	if currTorrent.Pieces[index].blocks[blockIndex] {
		currTorrent.mu.Unlock()
		return
	}
	currTorrent.Pieces[index].blocks[blockIndex] = true
	currTorrent.Downloaded += uint64(len(piecedata))

	currTorrent.mu.Unlock()
	currTorrent.wg.Add(1)
	go currTorrent.writeBlocktoFile(index, begin, piecedata)

	// Check if piece is complete
	if all(currTorrent.Pieces[index].blocks) {
		currTorrent.Pieces[index].completed = true

		currTorrent.wg.Add(1)
		go currTorrent.save()
		currTorrent.mu.Lock()
		currTorrent.torrentinfoChan <- InfoPieceComplete
		currTorrent.torrentinfoChan <- int(index)
		currTorrent.mu.Unlock()
	}

	if currTorrent.Downloaded == currTorrent.TotalSize {
		// Download Complete
		currTorrent.wg.Wait()

		currTorrent.deleteSave()
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
	for file_index, filepath := range currTorrent.Fileorder {
		file := currTorrent.Files[filepath]
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
	for _, v := range currTorrent.Files {
		v.filep.Close()
	}
	close(currTorrent.torrentinfoChan)
}

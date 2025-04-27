package metadata

import (
	"crypto/sha1"
	"errors"
	"math"
	"os"
	"path/filepath"

	bencode "github.com/anacrolix/torrent/bencode"
	"github.com/davecgh/go-spew/spew"
)

type Metadata struct {
	raw          map[string]interface{}
	TotalSize    uint64
	Files        map[string]int64
	AnnounceUrls []string
	PieceLength  int64
	Pieces       [][]byte
}

func (m *Metadata) GetMetadata(filepath string) (err error) {
	filedat, err := os.ReadFile(filepath)
	if err != nil {
		return
	}
	err = bencode.Unmarshal([]byte(filedat), &m.raw)
	return
}

func (m *Metadata) GetInfoHash() (infohash [20]byte, err error) {
	if m.raw == nil {
		err = errors.New("Metadat empty")
		return
	}
	bencoded_info, err := bencode.Marshal(m.raw["info"])
	if err != nil {
		return
	}
	return sha1.Sum(bencoded_info), nil

}
func (m *Metadata) Parse() error {
	if m.raw == nil {
		return errors.New("Metadata dict empty.")
	}
	info, in := m.raw["info"]
	if in {
		info := info.(map[string]interface{})

		// Piece Length
		m.PieceLength = info["piece length"].(int64)

		// Total size
		m.TotalSize = 0

		// Files
		m.Files = make(map[string]int64)
		name, _ := info["name"]

		length, in := info["length"]
		if in {
			// Single file mode
			m.Files[name.(string)] = length.(int64)
			m.TotalSize += uint64(length.(int64))
		} else {
			// Multifile mode
			for _, file := range info["files"].([]interface{}) {
				file, _ := file.(map[string]interface{})

				// Converting path list of interface{} to path list of string
				paths := make([]string, len(file["path"].([]interface{})))
				paths = append(paths, name.(string))
				for _, value := range file["path"].([]interface{}) {
					paths = append(paths, value.(string))
				}
				m.Files[filepath.Join(paths...)] = file["length"].(int64)
				m.TotalSize += uint64(file["length"].(int64))
			}
		}

		// Pieces
		m.Pieces = make([][]byte, int(math.Ceil(float64(m.TotalSize)/float64(m.PieceLength))))

		// byte string of concatination of all 20 bytes SHA1 hash value of pieces
		pieces := info["pieces"].(string)
		for i := 0; i < len(pieces); i += 20 {
			end := i + 20
			if end > len(pieces) {
				end = len(pieces)
			}
			m.Pieces[i/20] = []byte(pieces[i:end])
		}

	} else {
		return errors.New("info not present in torrent file")
	}
	announce, in := m.raw["announce"]
	if in {
		m.AnnounceUrls = append(m.AnnounceUrls, announce.(string))
	}
	announce_list, in := m.raw["announce-list"]
	if in {
		for _, urlList := range announce_list.([]interface{}) {
			for _, url := range urlList.([]interface{}) {
				m.AnnounceUrls = append(m.AnnounceUrls, url.(string))
			}
		}
	}

	return nil
}

func (m *Metadata) Print() {
	spew.Printf("Total Size: %v\n", m.TotalSize)
	spew.Printf("Piece Length: %v\n", m.PieceLength)
	spew.Printf("Files:\n")
	spew.Dump(m.Files)
	spew.Printf("AnnounceUrls:\n")
	spew.Dump(m.AnnounceUrls)
}

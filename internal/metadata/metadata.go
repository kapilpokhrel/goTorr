package metadata

import (
	"errors"
	"math"
	"os"
	"path/filepath"

	bencode "github.com/anacrolix/torrent/bencode"
)

type Metadata struct {
	raw           map[string]interface{}
	total_size    int64
	files         map[string]int64
	announce_urls []string
	piece_length  int64
	pieces        [][]byte
}

func (m *Metadata) GetMetadata(filepath string) (err error) {
	filedat, err := os.ReadFile(filepath)
	if err != nil {
		return
	}
	err = bencode.Unmarshal([]byte(filedat), &m.raw)
	return
}

func (m *Metadata) Parse() error {
	if m.raw == nil {
		return errors.New("Metadata dict empty.")
	}
	info, in := m.raw["info"]
	if in {
		info := info.(map[string]interface{})

		// Piece Length
		m.piece_length = info["piece length"].(int64)

		// Total size
		m.total_size = 0

		// Files
		m.files = make(map[string]int64)
		name, _ := info["name"]

		length, in := info["length"]
		if in {
			// Single file mode
			m.files[name.(string)] = length.(int64)
			m.total_size += length.(int64)
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
				m.files[filepath.Join(paths...)] = file["length"].(int64)
				m.total_size += file["length"].(int64)
			}
		}

		// Pieces
		m.pieces = make([][]byte, int(math.Ceil(float64(m.total_size)/float64(m.piece_length))))

		// byte string of concatination of all 20 bytes SHA1 hash value of pieces
		pieces := info["pieces"].(string)
		for i := 0; i < len(pieces); i += 20 {
			end := i + 20
			if end > len(pieces) {
				end = len(pieces)
			}
			m.pieces[i/20] = []byte(pieces[i:end])
		}

	} else {
		return errors.New("info not present in torrent file")
	}
	announce, in := m.raw["announce"]
	if in {
		m.announce_urls = append(m.announce_urls, announce.(string))
	}
	announce_list, in := m.raw["announce-list"]
	if in {
		for _, urlList := range announce_list.([]interface{}) {
			for _, url := range urlList.([]interface{}) {
				m.announce_urls = append(m.announce_urls, url.(string))
			}
		}
	}

	return nil
}

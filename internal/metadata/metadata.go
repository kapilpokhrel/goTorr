package metadata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	bencode "github.com/anacrolix/torrent/bencode"
)

type Metadata struct {
	dict map[string]interface{}
}

func (m *Metadata) GetMetadata(filepath string) (err error) {
	filedat, err := os.ReadFile(filepath)
	if err != nil {
		return
	}
	err = bencode.Unmarshal([]byte(filedat), &m.dict)
	return
}

func (m *Metadata) Print() error {
	info, in := m.dict["info"]
	if in {
		info_map, _ := info.(map[string]interface{})
		name, in := info_map["name"]
		if in {
			if str, ok := name.(string); ok {
				fmt.Println("Name: ", str)
			}
		}
		length, in := info_map["length"]
		if in {
			// Single file mode
			fmt.Printf("Length: %v\n", length)
		}
		files, in := info_map["files"]
		if in {
			fmt.Println("FILES: ")
			// Multifile mode
			files, _ := files.([]interface{})
			for _, file := range files {
				file, _ := file.(map[string]interface{})
				fmt.Printf("\tLength: %v\n", file["length"])

				paths := make([]string, len(file["path"].([]interface{})))
				for _, value := range file["path"].([]interface{}) {
					paths = append(paths, value.(string))
				}
				fmt.Printf("\tPath: %s\n", filepath.Join(paths...))
			}
		}
	} else {
		return errors.New("info not present in torrent file")
	}
	announce, in := m.dict["announce"]
	if in {
		if str, ok := announce.(string); ok {
			fmt.Println("Announce: ", str)
		}
	}

	return nil
}

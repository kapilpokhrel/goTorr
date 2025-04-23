package torrent

type Torrent struct {
	InfoHash   [20]byte
	Downloaded uint64
	Uploaded   uint64
}

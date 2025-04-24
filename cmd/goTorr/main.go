package main

import (
	"goTorr/internal/metadata"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func main() {
	var mdata metadata.Metadata
	err := mdata.GetMetadata("/mnt/exthome/dev_tmp/goTorr/cowboyBebop.torrent")
	check(err)
	err = mdata.Parse()
	check(err)
	mdata.Print()
}

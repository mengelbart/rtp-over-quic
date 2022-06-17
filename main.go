package main

import (
	"log"

	"github.com/mengelbart/rtp-over-quic/cmd"
)

func main() {
	log.SetFlags(log.Lshortfile)
	cmd.Execute()
}

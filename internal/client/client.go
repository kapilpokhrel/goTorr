package client

import (
	"github.com/google/uuid"
)

type Client struct {
	peerID [20]byte
}

func NewClient() (client *Client) {
	client = new(Client)
	copy(client.peerID[:], "-GT0001-"+uuid.NewString()[:12])
	return
}

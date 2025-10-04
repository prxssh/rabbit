package torrent

import (
	"context"
	"crypto/sha1"
)

type Client struct {
	ctx      context.Context
	torrents map[[sha1.Size]byte]*Torrent
}

func NewClient() *Client {
	return &Client{torrents: make(map[[sha1.Size]byte]*Torrent)}
}

func (c *Client) Startup(ctx context.Context) {
	c.ctx = ctx
}

func (c *Client) AddTorrent(data []byte) (*Torrent, error) {
	torrent, err := NewTorrent(data)
	if err != nil {
		return nil, err
	}

	go func() {
		torrent.Run(c.ctx)
	}()

	return torrent, nil
}

func (c *Client) RemoveTorrent(infoHash [sha1.Size]byte) {
	torrent, ok := c.torrents[infoHash]
	if !ok {
		return
	}

	torrent.Stop()
	delete(c.torrents, infoHash)
}

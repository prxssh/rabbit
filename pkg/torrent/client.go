package torrent

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"sync"
)

type Client struct {
	ctx      context.Context
	mu       sync.RWMutex
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

	c.mu.Lock()
	c.torrents[torrent.Metainfo.Info.Hash] = torrent
	c.mu.Unlock()

	go func() {
		torrent.Run(c.ctx)
	}()

	return torrent, nil
}

func (c *Client) RemoveTorrent(infoHashHex string) error {
	var infoHash [sha1.Size]byte

	bytes, err := hex.DecodeString(infoHashHex)
	if err != nil || len(bytes) != sha1.Size {
		return err
	}
	copy(infoHash[:], bytes)

	c.mu.Lock()
	defer c.mu.Unlock()

	torrent, ok := c.torrents[infoHash]
	if !ok {
		return nil
	}

	torrent.Stop()
	delete(c.torrents, infoHash)
	return nil
}

func (c *Client) GetTorrentStats(infoHashHex string) *Stats {
	var infoHash [sha1.Size]byte

	bytes, err := hex.DecodeString(infoHashHex)
	if err != nil || len(bytes) != sha1.Size {
		return nil
	}
	copy(infoHash[:], bytes)

	c.mu.RLock()
	torrent, ok := c.torrents[infoHash]
	c.mu.RUnlock()
	if !ok {
		return nil
	}

	return torrent.GetStats()
}

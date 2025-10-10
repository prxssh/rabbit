package torrent

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"log/slog"
	"sync"

	"github.com/prxssh/rabbit/internal/config"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Client struct {
	log      *slog.Logger
	ctx      context.Context
	mu       sync.RWMutex
	torrents map[[sha1.Size]byte]*Torrent
}

func NewClient() *Client {
	return &Client{
		torrents: make(map[[sha1.Size]byte]*Torrent),
		log:      slog.Default(),
	}
}

func (c *Client) Startup(ctx context.Context) {
	c.ctx = ctx
}

func (c *Client) AddTorrent(data []byte) (*Torrent, error) {
	torrent, err := NewTorrent(data)
	if err != nil {
		c.log.Error("failed to parse torrent", "error", err, "size", len(data))
		return nil, err
	}

	infoHashHex := hex.EncodeToString(torrent.Metainfo.Info.Hash[:])

	c.log.Debug("adding torrent",
		"name", torrent.Metainfo.Info.Name,
		"info_hash", infoHashHex,
		"size", torrent.Size,
		"pieces", len(torrent.Metainfo.Info.Pieces),
	)

	c.mu.Lock()
	c.torrents[torrent.Metainfo.Info.Hash] = torrent
	c.mu.Unlock()

	go func() { torrent.Run(c.ctx) }()
	return torrent, nil
}

func (c *Client) RemoveTorrent(infoHashHex string) error {
	var infoHash [sha1.Size]byte

	bytes, err := hex.DecodeString(infoHashHex)
	if err != nil || len(bytes) != sha1.Size {
		c.log.Error("invalid info hash", "hash", infoHashHex, "error", err)
		return err
	}
	copy(infoHash[:], bytes)

	c.mu.Lock()
	defer c.mu.Unlock()

	torrent, ok := c.torrents[infoHash]
	if !ok {
		c.log.Warn("torrent not found", "info_hash", infoHashHex)
		return nil
	}

	c.log.Debug("removing torrent",
		"name", torrent.Metainfo.Info.Name,
		"info_hash", infoHashHex,
	)

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

func (c *Client) SelectDownloadDirectory() (string, error) {
	path, err := runtime.OpenDirectoryDialog(c.ctx, runtime.OpenDialogOptions{
		Title: "Select Download Directory",
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

func (c *Client) GetConfig() *config.Config {
	return config.Load()
}

func (c *Client) UpdateConfig(cfg *config.Config) {
	config.Swap(*cfg)
}

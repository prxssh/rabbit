package torrent

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"log/slog"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Client struct {
	log      *slog.Logger
	ctx      context.Context
	mu       sync.RWMutex
	clientID [sha1.Size]byte
	torrents map[[sha1.Size]byte]*Torrent
}

func NewClient() (*Client, error) {
	clientID, err := generateClientID()
	if err != nil {
		return nil, err
	}

	return &Client{
		log:      slog.Default(),
		ctx:      context.Background(),
		clientID: clientID,
		torrents: make(map[[sha1.Size]byte]*Torrent),
	}, nil
}

func (c *Client) Startup(ctx context.Context) {
	c.ctx = ctx
}

func (c *Client) AddTorrent(data []byte, cfg *Config) (*Torrent, error) {
	if cfg == nil {
		cfg = WithDefaultConfig()
	}

	torrent, err := NewTorrent(c.clientID, data, cfg)
	if err != nil {
		c.log.Error("failed to parse torrent", "error", err, "size", len(data))
		return nil, err
	}

	infoHashHex := hex.EncodeToString(torrent.Metainfo.InfoHash[:])

	c.log.Debug("adding torrent",
		"name", torrent.Metainfo.Info.Name,
		"info_hash", infoHashHex,
		"size", torrent.Size,
		"pieces", len(torrent.Metainfo.Info.Pieces),
	)

	c.mu.Lock()
	c.torrents[torrent.Metainfo.InfoHash] = torrent
	c.mu.Unlock()

	go func() { torrent.Run(c.ctx) }()
	return torrent, nil
}

func (c *Client) GetDefaultConfig() *Config {
	return WithDefaultConfig()
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

	c.log.Debug(
		"removing torrent",
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

func generateClientID() ([sha1.Size]byte, error) {
	var peerID [sha1.Size]byte

	prefix := []byte("-RBBT-")
	copy(peerID[:], prefix)

	if _, err := rand.Read(peerID[len(prefix):]); err != nil {
		return [sha1.Size]byte{}, err
	}

	return peerID, nil
}

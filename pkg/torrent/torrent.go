package torrent

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prxssh/rabbit/pkg/peer"
	"github.com/prxssh/rabbit/pkg/piece"
	"github.com/prxssh/rabbit/pkg/tracker"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sync/errgroup"
)

// Config defines behavior and resource limits for a torrent download.
type Config struct {
	// DefaultDownloadDir is the default directory where NEW torrent files
	// are saved. Changing this only affects new torrents; existing torrents
	// continue downloading to their original location.
	DefaultDownloadDir string

	// Port is the TCP port this client listens on for incoming peer
	// connections.
	Port uint16

	// NumWant is the maximum number of peers to request the tracker.
	NumWant uint32

	// MaxUploadRate limits upload speed in bytes/second. 0 = unlimited.
	MaxUploadRate int64

	// MaxDownloadRate limits download speed in bytes/second. 0 = unlimited.
	MaxDownloadRate int64

	// AnnounceInterval overrides tracker's suggested interval.
	// 0 uses tracker default.
	AnnounceInterval time.Duration

	// MinAnnounceInterval enforces a minimum time between announces.
	MinAnnounceInterval time.Duration

	// MaxAnnounceBackoff caps exponential backoff for failed announces.
	MaxAnnounceBackoff time.Duration

	// EnableIPv6 allows connections to IPv6 peers.
	EnableIPv6 bool

	// EnableDHT enables DHT for peer discovery (future).
	EnableDHT bool

	// EnablePEX enables peer exchange protocol (future).
	EnablePEX bool

	// PieceManagerConfig configures piece selection and storage.
	PieceManagerConfig *piece.Config

	// PeerManagerConfig configures peer download & broadcast.
	PeerManagerConfig *peer.Config

	// ClientIDPrefix customizes the peer ID prefix (e.g., "-EC0001-").
	// Must be exactly 8 bytes. Empty uses default.
	ClientIDPrefix string
}

// DefaultConfig returns sensible defaults for most use cases.
func DefaultConfig() Config {
	downloadDir := getDefaultDownloadDir()
	pieceManagerCfg := piece.DefaultConfig()
	peerManagerCfg := peer.DefaultConfig()

	return Config{
		DefaultDownloadDir:  downloadDir,
		PieceManagerConfig:  &pieceManagerCfg,
		PeerManagerConfig:   &peerManagerCfg,
		Port:                6969,
		NumWant:             50,
		MaxUploadRate:       0, // unlimited
		MaxDownloadRate:     0, // unlimited
		AnnounceInterval:    0, // use tracker default
		MinAnnounceInterval: 2 * time.Minute,
		MaxAnnounceBackoff:  5 * time.Minute,
		EnableIPv6:          true,
		EnableDHT:           false,
		EnablePEX:           false,
		ClientIDPrefix:      "-EC0001-",
	}
}

type Client struct {
	ctx      context.Context
	clientID [sha1.Size]byte
	mu       sync.RWMutex
	cfg      Config
	torrents map[[sha1.Size]byte]*Torrent
}

func NewClient() (*Client, error) {
	clientID, err := generatePeerID()
	if err != nil {
		return nil, err
	}

	return &Client{
		torrents: make(map[[sha1.Size]byte]*Torrent),
		clientID: clientID,
		cfg:      DefaultConfig(),
	}, nil
}

func (c *Client) Startup(ctx context.Context) {
	c.ctx = ctx
}

func (c *Client) AddTorrent(data []byte) (*Torrent, error) {
	torrent, err := NewTorrent(c.clientID, data, c.cfg)
	if err != nil {
		return nil, err
	}

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

func (c *Client) SelectDownloadDirectory() (string, error) {
	path, err := runtime.OpenDirectoryDialog(
		c.ctx,
		runtime.OpenDialogOptions{
			Title: "Select Download Directory",
		},
	)
	if err != nil {
		return "", err
	}
	return path, nil
}

func (c *Client) GetConfig() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

func (c *Client) UpdateConfig(cfg Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = cfg
}

// Torrent represents a single BitTorrent download session.
//
// It coordinates the tracker announce loop, peer management, and piece
// selection for downloading a torrent. Call Run to start the download and Stop
// to gracefully terminate it.
type Torrent struct {
	// size is the total byte size of the torrent content.
	Size int64 `json:"size"`

	// clientID is this client's unique 20-byte peer ID.
	ClientID [sha1.Size]byte `json:"clientId"`

	// metainfo contains the parsed torrent metadata.
	Metainfo *Metainfo `json:"metainfo"`

	// tracker handles communication with the torrent tracker.
	tracker *tracker.Tracker `json:"-"`

	// peerManager coordinates all peer connections and downloads.
	peerManager *peer.Manager `json:"-"`

	// internal lifecycle management.
	cancel   context.CancelFunc
	stopOnce sync.Once

	// config for this torrent.
	cfg Config

	// log is the default logger for this torrent.
	log *slog.Logger
}

func NewTorrent(
	clientID [sha1.Size]byte,
	data []byte,
	cfg Config,
) (*Torrent, error) {
	metainfo, err := ParseMetainfo(data)
	if err != nil {
		return nil, err
	}
	size := metainfo.Size()

	log := slog.Default().With("torrent", metainfo.Info.Name)

	tracker, err := tracker.NewTracker(
		metainfo.Announce,
		metainfo.AnnounceList,
		log,
	)
	if err != nil {
		return nil, err
	}

	pieceManager, err := newPieceManager(metainfo, cfg.PieceManagerConfig)
	if err != nil {
		return nil, err
	}

	peerManager := peer.NewManager(
		clientID,
		metainfo.Info.Hash,
		len(metainfo.Info.Pieces),
		metainfo.Info.PieceLength,
		size,
		pieceManager,
		log,
		cfg.PeerManagerConfig,
	)

	return &Torrent{
		Size:        size,
		ClientID:    clientID,
		Metainfo:    metainfo,
		cfg:         cfg,
		log:         log,
		tracker:     tracker,
		peerManager: peerManager,
	}, nil
}

func (t *Torrent) Run(ctx context.Context) error {
	ctx, t.cancel = context.WithCancel(ctx)
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error { return t.announceLoop(ctx) })
	eg.Go(func() error { return t.peerManager.Run(ctx) })

	return eg.Wait()
}

func (t *Torrent) Stop() {
	t.stopOnce.Do(func() {
		if t.cancel != nil {
			t.cancel()
		}
	})
}

// Stats represents download progress and statistics for a torrent
type Stats struct {
	Downloaded   int64            `json:"downloaded"`
	Uploaded     int64            `json:"uploaded"`
	DownloadRate int64            `json:"downloadRate"`
	UploadRate   int64            `json:"uploadRate"`
	Progress     float64          `json:"progress"`
	Peers        []peer.PeerStats `json:"peers"`
	PieceStates  []int            `json:"pieceStates"`
}

func (t *Torrent) GetStats() *Stats {
	stats := t.peerManager.Stats()

	progress := 0.0
	if t.Size > 0 {
		progress = (float64(stats.TotalDownloaded) / float64(t.Size)) * 100.0
	}

	return &Stats{
		Progress:     progress,
		Downloaded:   stats.TotalDownloaded,
		Uploaded:     stats.TotalUploaded,
		DownloadRate: stats.DownloadRate,
		UploadRate:   stats.UploadRate,
		Peers:        stats.Peers,
		PieceStates:  stats.PieceStates,
	}
}

func (t *Torrent) announceLoop(ctx context.Context) error {
	const maxBackoffShift = 4
	consecutiveFailures := 0

	ticker := time.NewTicker(0)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			stopCtx, cancel := context.WithTimeout(
				context.Background(),
				10*time.Second,
			)
			defer cancel()

			announceParams := t.buildAnnounceParams()
			announceParams.Event = tracker.EventStopped
			_, _ = t.tracker.Announce(stopCtx, announceParams)

			return ctx.Err()
		case <-ticker.C:
			resp, err := t.tracker.Announce(
				ctx,
				t.buildAnnounceParams(),
			)
			if err != nil {
				consecutiveFailures++
				backoff := t.calculateBackoff(
					consecutiveFailures,
					maxBackoffShift,
				)
				t.log.Error(
					"announce failed",
					"error",
					err,
					"failures",
					consecutiveFailures,
					"retry_in",
					backoff,
				)

				ticker.Reset(backoff)
				continue
			}

			consecutiveFailures = 0
			t.peerManager.AdmitPeers(resp.Peers)
			interval := t.getNextAnnounceInterval(resp)

			ticker.Reset(interval)
		}
	}
}

func (t *Torrent) buildAnnounceParams() *tracker.AnnounceParams {
	stats := t.peerManager.Stats()

	event := tracker.EventStarted
	if t.Size-stats.TotalDownloaded == 0 {
		event = tracker.EventCompleted
	}

	return &tracker.AnnounceParams{
		InfoHash:   t.Metainfo.Info.Hash,
		PeerID:     t.ClientID,
		Port:       t.cfg.Port,
		Uploaded:   uint64(stats.TotalUploaded),
		Downloaded: uint64(stats.TotalDownloaded),
		Left:       uint64(t.Size - stats.TotalUploaded),
		Event:      event,
		NumWant:    t.cfg.NumWant,
	}
}

func (t *Torrent) getNextAnnounceInterval(
	resp *tracker.AnnounceResponse,
) time.Duration {
	interval := t.cfg.AnnounceInterval
	if interval == 0 {
		interval = 2 * time.Minute
	}

	if resp.Interval > 0 {
		interval = resp.Interval
	}
	if resp.MinInterval > 0 && resp.MinInterval > interval {
		interval = resp.MinInterval
	}

	if t.cfg.MinAnnounceInterval > 0 &&
		interval < t.cfg.MinAnnounceInterval {
		interval = t.cfg.MinAnnounceInterval
	}

	return interval
}

func (t *Torrent) calculateBackoff(failures int, maxShift int) time.Duration {
	const baseDelay = 15 * time.Second

	shift := failures - 1
	if shift > maxShift {
		shift = maxShift
	}

	delay := baseDelay * (1 << uint(shift))

	if delay > t.cfg.MaxAnnounceBackoff {
		delay = t.cfg.MaxAnnounceBackoff
	}

	jitter := time.Duration(rand.Int63n(int64(delay) / 2))
	return delay - (delay / 4) + jitter
}

func getDefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		if cwd, err := os.Getwd(); err == nil {
			return filepath.Join(cwd, "downloads")
		}
		return "./downloads"
	}

	switch runtime.Environment(context.Background()).Platform {
	case "windows":
		return filepath.Join(home, "Downloads", "rabbit")
	case "darwin":
		return filepath.Join(home, "Downloads", "rabbit")
	default: // linux, bsd, etc.
		return filepath.Join(
			home,
			".local",
			"share",
			"rabbit",
			"downloads",
		)
	}
}

func generatePeerID() ([sha1.Size]byte, error) {
	var peerID [sha1.Size]byte

	prefix := []byte("-EC0001-")
	copy(peerID[:], prefix)

	if _, err := rand.Read(peerID[len(prefix):]); err != nil {
		return [sha1.Size]byte{}, err
	}

	return peerID, nil
}

func newPieceManager(
	metainfo *Metainfo,
	cfg *piece.Config,
) (*piece.Manager, error) {
	size := metainfo.Size()

	var (
		paths [][]string
		lens  []int64
	)

	for _, file := range metainfo.Info.Files {
		paths = append(paths, file.Path)
		lens = append(lens, file.Length)
	}

	if metainfo.Info.Length == 0 {
		paths = append(paths, []string{metainfo.Info.Name})
		lens = append(lens, size)
	}

	return piece.NewPieceManager(
		metainfo.Info.Name,
		size,
		metainfo.Info.PieceLength,
		metainfo.Info.Pieces,
		paths,
		lens,
		cfg,
	)
}

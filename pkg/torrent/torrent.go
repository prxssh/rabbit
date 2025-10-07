package torrent

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"log/slog"
	mr "math/rand"
	"sync"
	"time"

	"github.com/prxssh/rabbit/pkg/config"
	"github.com/prxssh/rabbit/pkg/peer"
	"github.com/prxssh/rabbit/pkg/piece"
	"github.com/prxssh/rabbit/pkg/tracker"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sync/errgroup"
)

type Client struct {
	ctx      context.Context
	clientID [sha1.Size]byte
	mu       sync.RWMutex
	torrents map[[sha1.Size]byte]*Torrent
	log      *slog.Logger
}

func NewClient() (*Client, error) {
	config.Init()

	log := slog.Default().With("src", "torrent_client")

	clientID, err := generateClientID()
	if err != nil {
		log.Error("failed to generate client ID", "error", err)
		return nil, err
	}

	log.Info(
		"client initialized",
		"client_id",
		hex.EncodeToString(clientID[:8]),
	)

	return &Client{
		torrents: make(map[[sha1.Size]byte]*Torrent),
		clientID: clientID,
		log:      log,
	}, nil
}

func (c *Client) Startup(ctx context.Context) {
	c.ctx = ctx
}

func (c *Client) AddTorrent(data []byte) (*Torrent, error) {
	torrent, err := NewTorrent(c.clientID, data)
	if err != nil {
		c.log.Error(
			"failed to parse torrent",
			"error",
			err,
			"size",
			len(data),
		)
		return nil, err
	}

	infoHashHex := hex.EncodeToString(torrent.Metainfo.Info.Hash[:])
	c.log.Info(
		"adding torrent",
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
		c.log.Error(
			"invalid info hash",
			"hash",
			infoHashHex,
			"error",
			err,
		)
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

	c.log.Info(
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

func (c *Client) GetConfig() *config.Config {
	return config.Load()
}

func (c *Client) UpdateConfig(cfg *config.Config) {
	config.Swap(*cfg)
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

	// log is the default logger for this torrent.
	log *slog.Logger

	refillPeerQ chan struct{}
}

func NewTorrent(
	clientID [sha1.Size]byte,
	data []byte,
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

	pieceManager, err := newPieceManager(metainfo, log)
	if err != nil {
		return nil, err
	}

	refillPeerQ := make(chan struct{}, 1)

	peerManager := peer.NewManager(
		clientID,
		metainfo.Info.Hash,
		len(metainfo.Info.Pieces),
		metainfo.Info.PieceLength,
		size,
		pieceManager,
		refillPeerQ,
		log,
	)

	return &Torrent{
		Size:        size,
		ClientID:    clientID,
		Metainfo:    metainfo,
		log:         log,
		tracker:     tracker,
		peerManager: peerManager,
		refillPeerQ: refillPeerQ,
	}, nil
}

func (t *Torrent) Run(ctx context.Context) error {
	t.log.Info("torrent starting")
	ctx, t.cancel = context.WithCancel(ctx)
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error { return t.announceLoop(ctx) })

	eg.Go(func() error { return t.peerManager.Run(ctx) })

	eg.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case _, ok := <-t.refillPeerQ:
				if !ok {
					return nil
				}
				resp, err := t.tracker.Announce(
					ctx,
					t.buildAnnounceParams(),
				)
				if err != nil {
					t.log.Error(
						"failed refill peer",
						"error",
						err,
					)
					continue
				}
				t.log.Debug(
					"refilled peers",
					"count", len(resp.Peers),
				)
				t.peerManager.AdmitPeers(resp.Peers)
			}
		}
	})

	err := eg.Wait()
	t.log.Info("torrent stopped", "error", err)
	return err
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

	ticker := time.NewTicker(5 * time.Second)
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
			t.log.Debug(
				"announce successful",
				"peers", len(resp.Peers),
				"interval", resp.Interval,
				"seeders", resp.Seeders,
				"leechers", resp.Leechers,
			)
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
		Port:       config.Load().Port,
		Uploaded:   uint64(stats.TotalUploaded),
		Downloaded: uint64(stats.TotalDownloaded),
		Left:       uint64(t.Size - stats.TotalDownloaded),
		Event:      event,
		NumWant:    config.Load().NumWant,
	}
}

func (t *Torrent) getNextAnnounceInterval(
	resp *tracker.AnnounceResponse,
) time.Duration {
	interval := config.Load().AnnounceInterval
	if interval == 0 {
		interval = 2 * time.Minute
	}

	if resp.Interval > 0 {
		interval = resp.Interval
	}
	if resp.MinInterval > 0 && resp.MinInterval > interval {
		interval = resp.MinInterval
	}

	if config.Load().MinAnnounceInterval > 0 &&
		interval < config.Load().MinAnnounceInterval {
		interval = config.Load().MinAnnounceInterval
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

	if delay > config.Load().MaxAnnounceBackoff {
		delay = config.Load().MaxAnnounceBackoff
	}

	jitter := time.Duration(mr.Int63n(int64(delay) / 2))
	return delay - (delay / 4) + jitter
}

func generateClientID() ([sha1.Size]byte, error) {
	var peerID [sha1.Size]byte

	prefix := []byte(config.Load().ClientIDPrefix)
	copy(peerID[:], prefix)

	if _, err := rand.Read(peerID[len(prefix):]); err != nil {
		return [sha1.Size]byte{}, err
	}

	return peerID, nil
}

func newPieceManager(
	metainfo *Metainfo,
	log *slog.Logger,
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
		log,
	)
}

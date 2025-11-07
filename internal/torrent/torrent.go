package torrent

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log/slog"
	"net/netip"

	"github.com/prxssh/rabbit/internal/meta"
	"github.com/prxssh/rabbit/internal/peer"
	"github.com/prxssh/rabbit/internal/piece"
	"github.com/prxssh/rabbit/internal/scheduler"
	"github.com/prxssh/rabbit/internal/storage"
	"github.com/prxssh/rabbit/internal/tracker"
	"golang.org/x/sync/errgroup"
)

type Torrent struct {
	Metainfo *meta.Metainfo

	clientID     [sha1.Size]byte
	cfg          *Config
	logger       *slog.Logger
	tracker      *tracker.Tracker
	peerManager  *peer.Swarm
	storage      *storage.Store
	scheduler    *scheduler.Scheduler
	pieceManager *piece.Manager
	cancel       context.CancelFunc
}

func NewTorrent(clientID [sha1.Size]byte, data []byte, cfg *Config) (*Torrent, error) {
	if cfg == nil {
		cfg = WithDefaultConfig()
	}

	metainfo, err := meta.ParseMetainfo(data)
	if err != nil {
		return nil, err
	}

	logger := slog.Default().With("torrent", metainfo.Info.Name)

	storage, err := storage.NewStorage(metainfo, cfg.Storage, logger)
	if err != nil {
		return nil, err
	}

	pieceManager, err := piece.NewManager(
		metainfo.Info.Pieces,
		metainfo.Info.PieceLength,
		metainfo.Size,
	)
	if err != nil {
		return nil, err
	}

	scheduler := scheduler.NewScheduler(
		pieceManager,
		storage.PieceQueue,
		storage.PieceResultQueue,
		&scheduler.Opts{
			Config:   cfg.Scheduler,
			Logger:   logger,
			MaxPeers: cfg.Peer.MaxPeers,
		},
	)

	peerManager, err := peer.NewSwarm(&peer.SwarmOpts{
		Config:    cfg.Peer,
		Logger:    logger,
		Scheduler: scheduler,
		InfoHash:  metainfo.InfoHash,
	})
	if err != nil {
		return nil, err
	}

	tracker, err := tracker.NewTracker(
		metainfo.Announce,
		metainfo.AnnounceList,
		&tracker.TrackerOpts{
			Config: cfg.Tracker,
			Logger: logger,
		},
	)
	if err != nil {
		return nil, err
	}

	return &Torrent{
		clientID:     clientID,
		cfg:          cfg,
		logger:       logger,
		tracker:      tracker,
		pieceManager: pieceManager,
		scheduler:    scheduler,
		peerManager:  peerManager,
	}, nil
}

func (t *Torrent) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error { return t.tracker.Run(gctx) })
	g.Go(func() error { return t.peerManager.Run(gctx) })
	g.Go(func() error { return t.scheduler.Run(gctx) })
	g.Go(func() error { return t.storage.Run(gctx) })

	return g.Wait()
}

func (t *Torrent) Stop() {
	t.cancel()
}

type Stats struct {
	peer.SwarmMetrics
	tracker.TrackerMetrics
	Progress    float64            `json:"progress"`
	Peers       []peer.PeerMetrics `json:"peers"`
	PieceStates []piece.Status     `json:"pieceStates"`
}

func (t *Torrent) GetStats() *Stats {
	swarmStats := t.peerManager.Stats()
	trackerStats := t.tracker.Stats()

	s := &Stats{
		Progress:    0.0,
		Peers:       t.peerManager.PeerMetrics(),
		PieceStates: t.pieceManager.PieceStatus(),
	}
	s.SwarmMetrics = swarmStats
	s.TrackerMetrics = trackerStats

	if total := len(s.PieceStates); total > 0 {
		completed := 0
		for _, st := range s.PieceStates {
			if st == piece.StatusDone {
				completed++
			}
		}
		s.Progress = (float64(completed) / float64(total)) * 100.0
	}
	return s
}

func (t *Torrent) GetConfig() *Config {
	return t.cfg
}

func (t *Torrent) UpdateConfig(cfg *Config) {
	if cfg == nil {
		return
	}

	t.cfg = cfg
	t.logger.Info("torrent configuration updated")
}

func (t *Torrent) GetPeerMessageHistory(peerAddr string, limit int) ([]*peer.Event, error) {
	addr, err := netip.ParseAddrPort(peerAddr)
	if err != nil {
		return nil, err
	}

	p, ok := t.peerManager.GetPeer(addr)
	if !ok {
		return nil, fmt.Errorf("peer not found: %s", peerAddr)
	}

	return p.GetMessageHistory(limit)
}

func (t *Torrent) buildAnnounceParams() *tracker.AnnounceParams {
	stats := t.peerManager.Stats()
	downloaded := stats.TotalDownloaded
	left := t.Metainfo.Size - downloaded

	event := tracker.EventNone
	if left == 0 {
		event = tracker.EventCompleted
	} else if left > 0 {
		event = tracker.EventStarted
	}

	return &tracker.AnnounceParams{
		Event:      event,
		InfoHash:   t.Metainfo.InfoHash,
		PeerID:     t.clientID,
		Uploaded:   stats.TotalUploaded,
		Downloaded: stats.TotalDownloaded,
		Left:       t.Metainfo.Size - stats.TotalDownloaded,
	}
}

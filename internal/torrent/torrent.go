package torrent

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"

	"github.com/prxssh/rabbit/internal/meta"
	"github.com/prxssh/rabbit/internal/peer"
	"github.com/prxssh/rabbit/internal/scheduler"
	"github.com/prxssh/rabbit/internal/storage"
	"github.com/prxssh/rabbit/internal/tracker"
	"golang.org/x/sync/errgroup"
)

type Torrent struct {
	clientID    [sha1.Size]byte
	cfg         *Config
	Size        int64          `json:"size"`
	Metainfo    *meta.Metainfo `json:"metainfo"`
	tracker     *tracker.Tracker
	peerManager *peer.Swarm
	storage     *storage.Store
	scheduler   *scheduler.PieceScheduler
	cancel      context.CancelFunc
	stopOnce    sync.Once
	log         *slog.Logger
	refillPeerQ chan struct{}
}

func NewTorrent(clientID [sha1.Size]byte, data []byte, cfg *Config) (*Torrent, error) {
	if cfg == nil {
		cfg = WithDefaultConfig()
	}

	metainfo, err := meta.ParseMetainfo(data)
	if err != nil {
		return nil, err
	}

	torrent := &Torrent{
		cfg:      cfg,
		Metainfo: metainfo,
		Size:     metainfo.Size(),
		log:      slog.Default().With("torrent", metainfo.Info.Name),
	}

	storage, err := storage.NewStorage(metainfo, cfg.Storage, torrent.log)
	if err != nil {
		return nil, err
	}
	torrent.storage = storage

	scheduler, err := scheduler.NewPieceScheduler(&scheduler.Opts{
		Config:           cfg.Scheduler,
		Log:              torrent.log,
		TotalSize:        torrent.Size,
		PieceHashes:      metainfo.Info.Pieces,
		PieceLength:      metainfo.Info.PieceLength,
		PieceQueue:       storage.PieceQueue,
		PieceResultQueue: storage.PieceResultQueue,
	})
	torrent.scheduler = scheduler

	peerManager, err := peer.NewSwarm(&peer.SwarmOpts{
		Config:     cfg.Peer,
		Log:        torrent.log,
		Scheduler:  scheduler,
		InfoHash:   metainfo.InfoHash,
		PieceCount: len(metainfo.Info.Pieces),
	})
	if err != nil {
		return nil, err
	}
	torrent.peerManager = peerManager

	tracker, err := tracker.NewTracker(
		metainfo.Announce,
		metainfo.AnnounceList,
		&tracker.TrackerOpts{
			Config:            cfg.Tracker,
			Log:               torrent.log,
			OnAnnounceStart:   torrent.buildAnnounceParams,
			OnAnnounceSuccess: torrent.peerManager.AdmitPeers,
		},
	)
	if err != nil {
		return nil, err
	}
	torrent.tracker = tracker
	peerManager.RegisterRefillPeerHook(tracker.RefillPeers)

	return torrent, nil
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
	t.stopOnce.Do(func() {
		if t.cancel != nil {
			t.cancel()
		}

		t.log.Debug("stopped")
	})
}

type Stats struct {
	peer.SwarmMetrics
	tracker.TrackerMetrics
	Progress    float64                `json:"progress"`
	Peers       []peer.PeerMetrics     `json:"peers"`
	PieceStates []scheduler.PieceState `json:"pieceStates"`
}

func (t *Torrent) GetStats() *Stats {
	swarmStats := t.peerManager.Stats()
	trackerStats := t.tracker.Stats()

	s := &Stats{
		Progress:    0.0,
		Peers:       t.peerManager.PeerMetrics(),
		PieceStates: t.scheduler.PieceStates(),
	}
	s.SwarmMetrics = swarmStats
	s.TrackerMetrics = trackerStats

	if total := len(s.PieceStates); total > 0 {
		completed := 0
		for _, st := range s.PieceStates {
			if st == scheduler.PieceStateCompleted {
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
	t.log.Info("torrent configuration updated")

	// Note: Some config changes may require restart to take effect
	// For now we just update the stored config
	// TODO: Apply runtime config changes where possible
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
	left := t.Size - int64(downloaded)

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
		Left:       uint64(t.Size) - stats.TotalDownloaded,
	}
}

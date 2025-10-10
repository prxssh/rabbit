package torrent

import (
	"context"
	"log/slog"
	"sync"

	"github.com/prxssh/rabbit/internal/config"
	"github.com/prxssh/rabbit/internal/meta"
	"github.com/prxssh/rabbit/internal/peer"
	"github.com/prxssh/rabbit/internal/tracker"
	"golang.org/x/sync/errgroup"
)

type Torrent struct {
	Size        uint64         `json:"size"`
	Metainfo    *meta.Metainfo `json:"metainfo"`
	tracker     *tracker.Tracker
	peerManager *peer.Swarm
	cancel      context.CancelFunc
	stopOnce    sync.Once
	log         *slog.Logger
	refillPeerQ chan struct{}
}

func NewTorrent(data []byte) (*Torrent, error) {
	metainfo, err := meta.ParseMetainfo(data)
	if err != nil {
		return nil, err
	}

	torrent := &Torrent{
		Metainfo: metainfo,
		Size:     metainfo.Size(),
		log:      slog.Default().With("torrent", metainfo.Info.Name),
	}

	peerManager, err := peer.NewSwarm(&peer.SwarmOpts{
		Log:        torrent.log,
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
	Progress    float64            `json:"progress"`
	Peers       []peer.PeerMetrics `json:"peers"`
	PieceStates []int              `json:"pieceStates"`
}

func (t *Torrent) GetStats() *Stats {
	swarmStats := t.peerManager.Stats()
	trackerStats := t.tracker.Stats()

	s := &Stats{
		Progress:    0.0, // TODO: Calculate based on piece completion
		Peers:       t.peerManager.PeerMetrics(),
		PieceStates: []int{}, // TODO: Implement piece state tracking
	}
	s.SwarmMetrics = swarmStats
	s.TrackerMetrics = trackerStats
	return s
}

func (t *Torrent) buildAnnounceParams() *tracker.AnnounceParams {
	stats := t.peerManager.Stats()
	downloaded := stats.TotalDownloaded
	left := t.Size - downloaded

	event := tracker.EventNone
	if left == 0 {
		event = tracker.EventCompleted
	} else if left > 0 {
		event = tracker.EventStarted
	}

	return &tracker.AnnounceParams{
		NumWant:    50,
		Event:      event,
		Port:       config.Load().Port,
		InfoHash:   t.Metainfo.InfoHash,
		PeerID:     config.Load().ClientID,
		Uploaded:   stats.TotalUploaded,
		Downloaded: stats.TotalDownloaded,
		Left:       uint64(t.Size) - stats.TotalDownloaded,
	}
}

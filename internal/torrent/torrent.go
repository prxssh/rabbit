package torrent

import (
	"context"
	"log/slog"
	"sync"

	"github.com/prxssh/rabbit/internal/config"
	"github.com/prxssh/rabbit/internal/peer"
	"github.com/prxssh/rabbit/internal/tracker"
	"golang.org/x/sync/errgroup"
)

type Torrent struct {
	Size        uint64    `json:"size"`
	Metainfo    *Metainfo `json:"metainfo"`
	tracker     *tracker.Tracker
	peerManager *peer.Swarm
	cancel      context.CancelFunc
	stopOnce    sync.Once
	log         *slog.Logger
	refillPeerQ chan struct{}
}

func NewTorrent(data []byte) (*Torrent, error) {
	metainfo, err := ParseMetainfo(data)
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
		InfoHash:   metainfo.Info.Hash,
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

		t.log.Info("stopped")
	})
}

// SwarmMetrics represents aggregated peer swarm statistics
type SwarmMetrics struct {
	TotalPeers       uint32 `json:"totalPeers"`
	ConnectingPeers  uint32 `json:"connectingPeers"`
	FailedConnection uint32 `json:"failedConnection"`
	UnchokedPeers    uint32 `json:"unchokedPeers"`
	InterestedPeers  uint32 `json:"interestedPeers"`
	UploadingTo      uint32 `json:"uploadingTo"`
	DownloadingFrom  uint32 `json:"downloadingFrom"`
	TotalDownloaded  uint64 `json:"totalDownloaded"`
	TotalUploaded    uint64 `json:"totalUploaded"`
	DownloadRate     uint64 `json:"downloadRate"`
	UploadRate       uint64 `json:"uploadRate"`
}

// TrackerMetrics represents tracker statistics
type TrackerMetrics struct {
	TotalAnnounces      uint64 `json:"totalAnnounces"`
	SuccessfulAnnounces uint64 `json:"successfulAnnounces"`
	FailedAnnounces     uint64 `json:"failedAnnounces"`
	TotalPeersReceived  uint64 `json:"totalPeersReceived"`
	CurrentSeeders      int64  `json:"currentSeeders"`
	CurrentLeechers     int64  `json:"currentLeechers"`
	LastAnnounce        string `json:"lastAnnounce"`
	LastSuccess         string `json:"lastSuccess"`
}

// Stats represents download progress and statistics for a torrent
type Stats struct {
	Downloaded   int64          `json:"downloaded"`
	Uploaded     int64          `json:"uploaded"`
	DownloadRate int64          `json:"downloadRate"`
	UploadRate   int64          `json:"uploadRate"`
	Progress     float64        `json:"progress"`
	PieceStates  []int          `json:"pieceStates"`
	Swarm        SwarmMetrics   `json:"swarm"`
	Tracker      TrackerMetrics `json:"tracker"`
}

func (t *Torrent) GetStats() *Stats {
	if t.peerManager == nil || t.tracker == nil {
		return nil
	}

	// Get swarm stats
	swarmStats := t.peerManager.Stats()

	// Get tracker stats
	trackerStats := t.tracker.Stats()

	// Format timestamps
	lastAnnounce := ""
	if !trackerStats.LastAnnounce.IsZero() {
		lastAnnounce = trackerStats.LastAnnounce.Format("2006-01-02 15:04:05")
	}
	lastSuccess := ""
	if !trackerStats.LastSuccess.IsZero() {
		lastSuccess = trackerStats.LastSuccess.Format("2006-01-02 15:04:05")
	}

	return &Stats{
		Downloaded:   int64(swarmStats.TotalDownloaded),
		Uploaded:     int64(swarmStats.TotalUploaded),
		DownloadRate: int64(swarmStats.DownloadRate),
		UploadRate:   int64(swarmStats.UploadRate),
		Progress:     0.0, // TODO: Calculate based on piece completion
		PieceStates:  []int{}, // TODO: Implement piece state tracking
		Swarm: SwarmMetrics{
			TotalPeers:       swarmStats.TotalPeers,
			ConnectingPeers:  swarmStats.ConnectingPeers,
			FailedConnection: swarmStats.FailedConnection,
			UnchokedPeers:    swarmStats.UnchokedPeers,
			InterestedPeers:  swarmStats.InterestedPeers,
			UploadingTo:      swarmStats.UploadingTo,
			DownloadingFrom:  swarmStats.DownloadingFrom,
			TotalDownloaded:  swarmStats.TotalDownloaded,
			TotalUploaded:    swarmStats.TotalUploaded,
			DownloadRate:     swarmStats.DownloadRate,
			UploadRate:       swarmStats.UploadRate,
		},
		Tracker: TrackerMetrics{
			TotalAnnounces:      trackerStats.TotalAnnounces,
			SuccessfulAnnounces: trackerStats.SuccessfulAnnounces,
			FailedAnnounces:     trackerStats.FailedAnnounces,
			TotalPeersReceived:  trackerStats.TotalPeersReceived,
			CurrentSeeders:      trackerStats.CurrentSeeders,
			CurrentLeechers:     trackerStats.CurrentLeechers,
			LastAnnounce:        lastAnnounce,
			LastSuccess:         lastSuccess,
		},
	}
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
		InfoHash:   t.Metainfo.Info.Hash,
		PeerID:     config.Load().ClientID,
		Uploaded:   stats.TotalUploaded,
		Downloaded: stats.TotalDownloaded,
		Left:       uint64(t.Size) - stats.TotalDownloaded,
	}
}

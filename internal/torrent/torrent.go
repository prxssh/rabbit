package torrent

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"time"

	"github.com/prxssh/rabbit/internal/dht"
	"github.com/prxssh/rabbit/internal/meta"
	"github.com/prxssh/rabbit/internal/peer"
	"github.com/prxssh/rabbit/internal/piece"
	"github.com/prxssh/rabbit/internal/scheduler"
	"github.com/prxssh/rabbit/internal/storage"
	"github.com/prxssh/rabbit/internal/tracker"
	"golang.org/x/sync/errgroup"
)

type Torrent struct {
	Metainfo *meta.Metainfo `json:"metainfo"`

	clientID     [sha1.Size]byte
	cfg          *Config
	logger       *slog.Logger
	tracker      *tracker.Tracker
	dht          *dht.DHT
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
		logger,
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
		ClientID:  clientID,
	})
	if err != nil {
		return nil, err
	}

	torrent := &Torrent{
		Metainfo:     metainfo,
		clientID:     clientID,
		cfg:          cfg,
		logger:       logger,
		pieceManager: pieceManager,
		scheduler:    scheduler,
		peerManager:  peerManager,
		storage:      storage,
	}

	tracker, err := tracker.NewTracker(
		metainfo.Announce,
		metainfo.AnnounceList,
		&tracker.TrackerOpts{
			Config:        cfg.Tracker,
			Logger:        logger,
			PeerAddrQueue: peerManager.GetPeerConnectQueue(),
			GetState:      torrent.buildAnnounceParams,
		},
	)
	if err != nil {
		return nil, err
	}
	torrent.tracker = tracker

	if cfg.DHT != nil {
		dhtConfig := &dht.Config{
			Logger:         logger,
			LocalID:        clientID,
			ListenAddr:     cfg.DHT.ListenAddr,
			BootstrapNodes: cfg.DHT.BootstrapNodes,
		}
		dhtInstance, err := dht.NewDHT(dhtConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create DHT: %w", err)
		}
		torrent.dht = dhtInstance
	}

	return torrent, nil
}

func (t *Torrent) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	if t.dht != nil {
		if err := t.dht.Start(); err != nil {
			return fmt.Errorf("failed to start DHT: %w", err)
		}
		defer t.dht.Stop()
	}

	g, gctx := errgroup.WithContext(ctx)

	t.logger.Warn("Tracker disabled - testing DHT only")

	g.Go(func() error { return t.peerManager.Run(gctx) })
	g.Go(func() error { return t.scheduler.Run(gctx) })
	g.Go(func() error { return t.storage.Run(gctx) })

	if t.dht != nil {
		g.Go(func() error { return t.dhtPeerDiscoveryLoop(gctx) })
	}

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
	PieceStates []int              `json:"pieceStates"`
}

func (t *Torrent) GetStats() *Stats {
	swarmStats := t.peerManager.Stats()
	trackerStats := t.tracker.Stats()

	// Get piece statuses and convert to []int for JSON marshaling
	rawStates := t.pieceManager.PieceStatus()
	pieceStates := make([]int, len(rawStates))
	for i, status := range rawStates {
		pieceStates[i] = int(status)
	}

	s := &Stats{
		Progress:    0.0,
		Peers:       t.peerManager.PeerMetrics(),
		PieceStates: pieceStates,
	}
	s.SwarmMetrics = swarmStats
	s.TrackerMetrics = trackerStats

	if total := len(s.PieceStates); total > 0 {
		completed := 0
		for _, st := range s.PieceStates {
			if st == int(piece.StatusDone) {
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

	if cfg.Scheduler != nil {
		t.scheduler.UpdateConfig(cfg.Scheduler)
	}

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

func (t *Torrent) dhtPeerDiscoveryLoop(ctx context.Context) error {
	interval := 15 * time.Minute
	if t.cfg.Tracker != nil && t.cfg.Tracker.AnnounceInterval > 0 {
		interval = t.cfg.Tracker.AnnounceInterval
	}

	t.logger.Info("Waiting for DHT to bootstrap...")
	time.Sleep(10 * time.Second)

	t.queryDHTForPeers()
	t.announceToDHT()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			t.queryDHTForPeers()
			t.announceToDHT()
		}
	}
}

func (t *Torrent) queryDHTForPeers() {
	peers, err := t.dht.GetPeers(t.Metainfo.InfoHash)
	if err != nil {
		t.logger.Warn("DHT peer lookup failed", "error", err.Error())
		return
	}

	if len(peers) == 0 {
		t.logger.Debug("No peers found in DHT")
		return
	}

	peerAddrs := make([]peer.PeerAddr, 0, len(peers))
	for _, peerNet := range peers {
		var addr netip.AddrPort
		switch p := peerNet.(type) {
		case *net.UDPAddr:
			ip, ok := netip.AddrFromSlice(p.IP)
			if !ok {
				continue
			}
			addr = netip.AddrPortFrom(ip, uint16(p.Port))
		case *net.TCPAddr:
			ip, ok := netip.AddrFromSlice(p.IP)
			if !ok {
				continue
			}
			addr = netip.AddrPortFrom(ip, uint16(p.Port))
		default:
			t.logger.Warn("Unknown peer address type from DHT", "type", fmt.Sprintf("%T", peerNet))
			continue
		}

		peerAddrs = append(peerAddrs, peer.PeerAddr{Addr: addr, Source: peer.PeerSourceDHT})
	}

	if len(peerAddrs) > 0 {
		t.logger.Info("Found peers via DHT", "count", len(peerAddrs))
		t.peerManager.AdmitPeersWithSource(peerAddrs)
	}
}

func (t *Torrent) announceToDHT() {
	port := 6969
	if t.cfg.Tracker != nil && t.cfg.Tracker.Port > 0 {
		port = int(t.cfg.Tracker.Port)
	}

	err := t.dht.AnnouncePeer(t.Metainfo.InfoHash, port)
	if err != nil {
		t.logger.Warn("DHT announce failed", "error", err.Error())
		return
	}

	t.logger.Debug("Announced to DHT", "port", port)
}

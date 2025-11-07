package peer

import (
	"context"
	"crypto/sha1"
	"log/slog"
	"math/rand"
	"net/netip"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prxssh/rabbit/internal/scheduler"
)

type Config struct {
	MaxPeers                  uint8
	UploadSlots               uint8
	PeerOutboxBacklog         uint8
	ReadTimeout               time.Duration
	WriteTimeout              time.Duration
	DialTimeout               time.Duration
	RechokeInterval           time.Duration
	OptimisticUnchokeInterval time.Duration
	PeerHeartbeatInterval     time.Duration
	PeerInactivityDuration    time.Duration
}

func WithDefaultConfig() *Config {
	return &Config{
		UploadSlots:               4,
		MaxPeers:                  50,
		ReadTimeout:               45 * time.Second,
		WriteTimeout:              30 * time.Second,
		DialTimeout:               45 * time.Second,
		RechokeInterval:           10 * time.Second,
		OptimisticUnchokeInterval: 30 * time.Second,
		PeerHeartbeatInterval:     45 * time.Second,
		PeerInactivityDuration:    2 * time.Minute,
		PeerOutboxBacklog:         50,
	}
}

type Swarm struct {
	cfg                        *Config
	logger                     *slog.Logger
	peerMut                    sync.RWMutex
	peers                      map[netip.AddrPort]*Peer
	infoHash                   [sha1.Size]byte
	clientID                   [sha1.Size]byte
	isSeeder                   bool
	stats                      *SwarmStats
	cancel                     context.CancelFunc
	scheduler                  *scheduler.Scheduler
	optimisticUnchokedPeerAddr netip.AddrPort
	peerConnectCh              chan netip.AddrPort
}

type SwarmStats struct {
	TotalPeers       atomic.Uint32
	ConnectingPeers  atomic.Uint32
	FailedConnection atomic.Uint32
	UnchokedPeers    atomic.Uint32
	InterestedPeers  atomic.Uint32
	UploadingTo      atomic.Uint32
	DownloadingFrom  atomic.Uint32
	TotalDownloaded  atomic.Uint64
	TotalUploaded    atomic.Uint64
	DownloadRate     atomic.Uint64
	UploadRate       atomic.Uint64
}

type SwarmOpts struct {
	Config    *Config
	Logger    *slog.Logger
	InfoHash  [sha1.Size]byte
	ClientID  [sha1.Size]byte
	Scheduler *scheduler.Scheduler
	IsSeeder  bool
}

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

func NewSwarm(opts *SwarmOpts) (*Swarm, error) {
	return &Swarm{
		cfg:           opts.Config,
		infoHash:      opts.InfoHash,
		clientID:      opts.ClientID,
		stats:         &SwarmStats{},
		scheduler:     opts.Scheduler,
		peers:         make(map[netip.AddrPort]*Peer),
		peerConnectCh: make(chan netip.AddrPort, opts.Config.MaxPeers),
		logger:        opts.Logger.With("source", "peer_swarm"),
		isSeeder:      opts.IsSeeder,
	}, nil
}

// TODO: errgroup
func (s *Swarm) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	wg.Go(func() { s.maintenanceLoop(ctx) })
	wg.Go(func() { s.statsLoop(ctx) })
	wg.Go(func() { s.chokeLoop(ctx) })

	for dialWorker := 0; dialWorker < 10; dialWorker++ {
		wg.Go(func() { s.peerDialerLoop(ctx) })
	}

	wg.Wait()

	return nil
}

func (s *Swarm) Stats() SwarmMetrics {
	ps := s.stats
	return SwarmMetrics{
		TotalPeers:       ps.TotalPeers.Load(),
		ConnectingPeers:  ps.ConnectingPeers.Load(),
		FailedConnection: ps.FailedConnection.Load(),
		UnchokedPeers:    ps.UnchokedPeers.Load(),
		InterestedPeers:  ps.InterestedPeers.Load(),
		UploadingTo:      ps.UploadingTo.Load(),
		DownloadingFrom:  ps.DownloadingFrom.Load(),
		TotalDownloaded:  ps.TotalDownloaded.Load(),
		TotalUploaded:    ps.TotalUploaded.Load(),
		DownloadRate:     ps.DownloadRate.Load(),
		UploadRate:       ps.UploadRate.Load(),
	}
}

func (s *Swarm) PeerMetrics() []PeerMetrics {
	s.peerMut.RLock()
	defer s.peerMut.RUnlock()

	metrics := make([]PeerMetrics, 0, len(s.peers))
	for _, peer := range s.peers {
		metrics = append(metrics, peer.Stats())
	}

	return metrics
}

func (s *Swarm) AdmitPeers(addrs []netip.AddrPort) {
	for _, addr := range addrs {
		select {
		case s.peerConnectCh <- addr:
		default:
			s.logger.Warn("admit peer queue full; dropping", "addr", addr)
		}
	}
}

func (s *Swarm) addPeer(ctx context.Context, addr netip.AddrPort) (*Peer, error) {
	s.peerMut.RLock()
	_, dup := s.peers[addr]
	totalPeers := len(s.peers)
	s.peerMut.RUnlock()

	if dup {
		return nil, nil
	}

	if totalPeers >= int(s.cfg.MaxPeers) {
		return nil, nil
	}

	s.stats.ConnectingPeers.Add(1)

	peer, err := newPeer(ctx, addr, &peerOpts{
		infoHash:   s.infoHash,
		clientID:   s.clientID,
		config:     s.cfg,
		logger:     s.logger,
		eventQueue: s.scheduler.GetPeerEventQueue(),
		workQueue:  s.scheduler.GetPeerWorkQueue(addr),
	})
	s.stats.ConnectingPeers.Add(^uint32(0))

	if err != nil {
		s.stats.FailedConnection.Add(1)
		return nil, err
	}

	s.peerMut.Lock()
	s.peers[peer.addr] = peer
	s.peerMut.Unlock()

	s.stats.TotalPeers.Add(1)

	return peer, nil
}

func (s *Swarm) removePeer(addr netip.AddrPort) {
	s.peerMut.Lock()
	if _, exists := s.peers[addr]; !exists {
		s.peerMut.Unlock()
		return
	}
	delete(s.peers, addr)
	s.peerMut.Unlock()

	s.stats.TotalPeers.Add(^uint32(0))
}

func (s *Swarm) GetPeer(addr netip.AddrPort) (*Peer, bool) {
	s.peerMut.RLock()
	defer s.peerMut.RUnlock()

	peer, ok := s.peers[addr]
	return peer, ok
}

func (s *Swarm) maintenanceLoop(ctx context.Context) error {
	l := s.logger.With("component", "maintenance loop")
	l.Debug("started")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-ticker.C:
			maxIdle := s.cfg.PeerInactivityDuration
			var inactivePeerAddrs []netip.AddrPort

			s.peerMut.RLock()
			for addr, peer := range s.peers {
				if peer.Idleness() > maxIdle {
					inactivePeerAddrs = append(inactivePeerAddrs, addr)
				}
			}
			s.peerMut.RUnlock()

			for _, addr := range inactivePeerAddrs {
				s.removePeer(addr)
			}

			n := len(inactivePeerAddrs)
			if n > 0 {
				l.Info("removed inactive peers", "count", n)
			}
		}
	}
}

func (s *Swarm) peerDialerLoop(ctx context.Context) {
	l := s.logger.With("component", "peer dialer loop")
	l.Debug("started")

	for {
		select {
		case <-ctx.Done():
			return

		case peerAddr, ok := <-s.peerConnectCh:
			if !ok {
				return
			}

			peer, err := s.addPeer(ctx, peerAddr)
			if err != nil {
				l.Debug("peer connection failed", "addr", peerAddr, "error", err.Error())
				continue
			}
			if peer == nil { // duplicate
				continue
			}

			go func(p *Peer) {
				defer s.removePeer(p.addr)
				p.Run(ctx)
			}(peer)
		}
	}
}

func (s *Swarm) statsLoop(ctx context.Context) error {
	l := s.logger.With("component", "stats loop")
	l.Debug("started")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Warn("context done, exiting", "error", ctx.Err())
			return nil

		case <-ticker.C:
			var totUp, totDown, upRate, downRate uint64
			var unchoked, interested, uploadingTo, downloadingFrom uint32

			s.peerMut.RLock()
			for _, peer := range s.peers {
				totUp += peer.stats.Uploaded.Load()
				totDown += peer.stats.Downloaded.Load()
				ru := peer.stats.UploadRate.Load()
				rd := peer.stats.DownloadRate.Load()
				upRate += ru
				downRate += rd

				if !peer.AmChoking() {
					unchoked++
				}
				if peer.AmInterested() {
					interested++
				}
				if ru > 0 {
					uploadingTo++
				}
				if rd > 0 {
					downloadingFrom++
				}
			}
			s.peerMut.RUnlock()

			s.stats.TotalUploaded.Store(totUp)
			s.stats.TotalDownloaded.Store(totDown)
			s.stats.UploadRate.Store(upRate)
			s.stats.DownloadRate.Store(downRate)
			s.stats.UnchokedPeers.Store(unchoked)
			s.stats.InterestedPeers.Store(interested)
			s.stats.UploadingTo.Store(uploadingTo)
			s.stats.DownloadingFrom.Store(downloadingFrom)
		}
	}
}

func (s *Swarm) chokeLoop(ctx context.Context) {
	l := s.logger.With("source", "leecher choke loop")
	l.Debug("started")

	normalChokeTicker := time.NewTicker(s.cfg.RechokeInterval)
	defer normalChokeTicker.Stop()

	optimisticChokeTicker := time.NewTicker(s.cfg.OptimisticUnchokeInterval)
	defer optimisticChokeTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-normalChokeTicker.C:
			s.recalculateRegularUnchokes(ctx)

		case <-optimisticChokeTicker.C:
			s.recalculateOptimisticUnchoke(ctx)
		}
	}
}

func (s *Swarm) recalculateRegularUnchokes(ctx context.Context) {
	var candidates []*Peer

	s.peerMut.RLock()
	for _, peer := range s.peers {
		if peer.AmInterested() {
			candidates = append(candidates, peer)
		}
	}
	s.peerMut.RUnlock()

	sort.Slice(candidates, func(i, j int) bool {
		if s.isSeeder {
			return candidates[i].stats.UploadRate.Load() > candidates[j].stats.UploadRate.Load()
		}

		return candidates[i].stats.DownloadRate.Load() > candidates[j].stats.DownloadRate.Load()
	})

	newUnchokes := make(map[netip.AddrPort]struct{})
	for i := 0; i < len(candidates) && i < int(s.cfg.UploadSlots); i++ {
		newUnchokes[candidates[i].addr] = struct{}{}
	}

	s.peerMut.Lock()
	for _, peer := range s.peers {
		_, isTopPeer := newUnchokes[peer.addr]
		isOptimistic := (peer.addr == s.optimisticUnchokedPeerAddr)

		if isTopPeer || isOptimistic {
			if peer.AmChoking() {
				peer.Unchoke()
			}
		} else {
			if !peer.AmChoking() {
				peer.Choke()
			}
		}
	}
	s.peerMut.Unlock()
}

func (s *Swarm) recalculateOptimisticUnchoke(ctx context.Context) {
	var candidates []*Peer

	s.peerMut.RLock()
	for _, peer := range s.peers {
		if peer.PeerInterested() && peer.AmChoking() {
			candidates = append(candidates, peer)
		}
	}
	s.peerMut.RUnlock()

	if len(candidates) == 0 {
		s.optimisticUnchokedPeerAddr = netip.AddrPort{}
		return
	}

	newOptimistic := candidates[rand.Intn(len(candidates))]
	s.optimisticUnchokedPeerAddr = newOptimistic.addr
	newOptimistic.Unchoke()
}

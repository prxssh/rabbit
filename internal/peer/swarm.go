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
	ReadTimeout               time.Duration
	WriteTimeout              time.Duration
	DialTimeout               time.Duration
	MaxPeers                  int
	UploadSlots               int
	RechokeInterval           time.Duration
	OptimisticUnchokeInterval time.Duration
	PeerHeartbeatInterval     time.Duration
	PeerInactivityDuration    time.Duration
	PeerOutboxBacklog         int
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
	log                        *slog.Logger
	peerMut                    sync.RWMutex
	peers                      map[netip.AddrPort]*Peer
	infoHash                   [sha1.Size]byte
	clientID                   [sha1.Size]byte
	stats                      *SwarmStats
	cancel                     context.CancelFunc
	scheduler                  *scheduler.PieceScheduler
	optimisticUnchokedPeerAddr netip.AddrPort
	peerConnectCh              chan netip.AddrPort
	requestMorePeerCh          chan struct{}
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
	Log       *slog.Logger
	InfoHash  [sha1.Size]byte
	ClientID  [sha1.Size]byte
	Scheduler *scheduler.PieceScheduler
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
		log:           opts.Log.With("src", "peer_swarm"),
	}, nil
}

func (s *Swarm) RegisterRefillPeerHook(hook func()) {
	s.refillPeersHook = hook
}

func (s *Swarm) Run(ctx context.Context) error {
	defer s.Close()

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	var wg sync.WaitGroup

	wg.Go(func() { s.maintenanceLoop(ctx) })
	wg.Go(func() { s.admitPeersLoop(ctx) })
	wg.Go(func() { s.statsLoop(ctx) })
	wg.Go(func() { s.leecherChokeLoop(ctx) })
	wg.Go(func() { s.seederChokeLoop(ctx) })
	wg.Wait()

	return nil
}

func (s *Swarm) Close() {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.cancel()

		close(s.admitPeerCh)

		s.log.Debug("stopped")
	})
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
	out := make([]PeerMetrics, 0, s.peers.Count())
	s.peers.Range(func(addr netip.AddrPort, peer *Peer) bool {
		out = append(out, peer.Stats())
		return true
	})

	return out
}

func (s *Swarm) AdmitPeers(addrs []netip.AddrPort) {
	for _, addr := range addrs {
		select {
		case s.admitPeerCh <- addr:
		default:
			s.log.Warn("admit peer queue full; dropping", "addr", addr)
		}
	}
}

func (s *Swarm) AddPeer(ctx context.Context, addr netip.AddrPort) (*Peer, error) {
	if s.closed.Load() {
		return nil, nil
	}

	s.peerMu.RLock()
	_, dup := s.peers[addr]
	totalPeers := len(s.peers)
	s.peerMu.RUnlock()

	if dup {
		return nil, nil
	}

	if totalPeers >= s.cfg.MaxPeers {
		return nil, nil
	}

	s.stats.ConnectingPeers.Add(1)

	// TODO: cleanup queue when peer connection fails
	peer, err := NewPeer(ctx, addr, &peerOpts{
		infoHash:   s.infoHash,
		clientID:   s.clientID,
		config:     s.cfg,
		log:        s.log,
		eventQueue: s.scheduler.GetEventQueue(),
		workQueue:  s.scheduler.GetPeerWorkQueue(addr),
	})

	s.stats.ConnectingPeers.Add(^uint32(0))

	if err != nil {
		s.stats.FailedConnection.Add(1)
		return nil, err
	}

	s.peerMu.Lock()
	s.peers[peer.addr] = peer
	s.peerMu.Unlock()
	s.stats.TotalPeers.Add(1)

	return peer, nil
}

func (s *Swarm) RemovePeer(addr netip.AddrPort) {
	s.peerMu.Lock()
	peer, exists := s.peers[addr]
	if !exists {
		s.peerMu.Unlock()
		return
	}
	delete(s.peers, addr)
	s.peerMu.Unlock()

	peer.Close()
	s.stats.TotalPeers.Add(^uint32(0))
}

func (s *Swarm) GetPeer(addr netip.AddrPort) (*Peer, bool) {
	s.peerMu.RLock()
	defer s.peerMu.RUnlock()

	peer, ok := s.peers[addr]
	return peer, ok
}

func (s *Swarm) maintenanceLoop(ctx context.Context) error {
	l := s.log.With("component", "purge inactive peers loop")
	l.Debug("started")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Warn("context done, exiting", "error", ctx.Err())
			return nil

		case <-ticker.C:
			maxIdle := s.cfg.PeerInactivityDuration
			var inactivePeerAddrs []netip.AddrPort

			s.peerMu.RLock()
			for addr, peer := range s.peers {
				if peer.Idleness() > maxIdle {
					inactivePeerAddrs = append(inactivePeerAddrs, addr)
				}
			}
			s.peerMu.RUnlock()

			for _, addr := range inactivePeerAddrs {
				s.RemovePeer(addr)
			}

			if s.stats.TotalPeers.Load() < 6 && s.refillPeersHook != nil {
				l.Debug("peers low; requesting more peers")
				s.refillPeersHook()
			}

			l.Debug("purged inactive peers", "removed", len(inactivePeerAddrs))
		}
	}
}

func (s *Swarm) admitPeersLoop(ctx context.Context) error {
	l := s.log.With("component", "admit peers loop")
	l.Debug("started")

	for {
		select {
		case <-ctx.Done():
			l.Warn("context done, exiting", "error", ctx.Err().Error())
			return nil

		case peerAddr, ok := <-s.admitPeerCh:
			if !ok {
				l.Error("admit peers queue closed, existing")
				return nil
			}

			go func(addr netip.AddrPort) {
				p, err := s.AddPeer(ctx, addr)
				if err != nil {
					l.Debug(
						"peer connection failed",
						"addr", addr,
						"error", err.Error(),
					)
					return
				}

				if p == nil {
					return
				}

				defer s.RemovePeer(p.addr)

				if err := p.Run(ctx); err != nil {
					l.Debug(
						"peer failed to run",
						"addr", p.addr,
						"error", err.Error(),
					)
					return
				}
			}(peerAddr)
		}
	}
}

func (s *Swarm) statsLoop(ctx context.Context) error {
	l := s.log.With("component", "stats loop")
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

			s.peerMu.RLock()
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
			s.peerMu.RUnlock()

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

func (s *Swarm) leecherChokeLoop(ctx context.Context) {
	l := s.log.With("component", "leecher_choke_loop")
	l.Debug("started")

	normalChokeTicker := time.NewTicker(10 * time.Second)
	defer normalChokeTicker.Stop()

	optimisticChokeTicker := time.NewTicker(30 * time.Second)
	defer optimisticChokeTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-normalChokeTicker.C:
			s.recalculateRegularUnchokes(ctx, false)
		case <-optimisticChokeTicker.C:
			s.recalculateOptimisticUnchoke(ctx)
		}
	}
}

func (s *Swarm) seederChokeLoop(ctx context.Context) {
	l := s.log.With("component", "seeder_choke_loop")
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
			s.recalculateRegularUnchokes(ctx, true)
		case <-optimisticChokeTicker.C:
			s.recalculateOptimisticUnchoke(ctx)
		}
	}
}

func (s *Swarm) recalculateRegularUnchokes(ctx context.Context, isSeeder bool) {
	var candidates []*Peer

	s.peerMu.RLock()
	for _, peer := range s.peers {
		if peer.AmInterested() {
			candidates = append(candidates, peer)
		}
	}
	s.peerMu.RUnlock()

	sort.Slice(candidates, func(i, j int) bool {
		if isSeeder {
			return candidates[i].stats.UploadRate.Load() > candidates[j].stats.UploadRate.Load()
		}

		return candidates[i].stats.DownloadRate.Load() > candidates[j].stats.DownloadRate.Load()
	})

	newUnchokes := make(map[netip.AddrPort]struct{})
	for i := 0; i < len(candidates) && i < s.cfg.UploadSlots; i++ {
		newUnchokes[candidates[i].addr] = struct{}{}
	}

	s.peerMu.Lock()
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
	s.peerMu.Unlock()
}

func (s *Swarm) recalculateOptimisticUnchoke(ctx context.Context) {
	var candidates []*Peer

	s.peerMu.RLock()
	for _, peer := range s.peers {
		if peer.PeerInterested() && peer.AmChoking() {
			candidates = append(candidates, peer)
		}
	}
	s.peerMu.RUnlock()

	if len(candidates) == 0 {
		s.optimisticUnchokedPeerAddr = netip.AddrPort{}
		return
	}

	newOptimistic := candidates[rand.Intn(len(candidates))]
	s.optimisticUnchokedPeerAddr = newOptimistic.addr
	newOptimistic.Unchoke()
}

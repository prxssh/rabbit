package peer

import (
	"context"
	"crypto/sha1"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prxssh/rabbit/internal/scheduler"
	"github.com/prxssh/rabbit/internal/storage"
)

type Config struct {
	// ReadTimeout is the maximum time to wait for data from a peer before
	// considering the connection stalled.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum time to wait when sending data to a peer
	// before considering the connection stalled.
	WriteTimeout time.Duration

	// DialTimeout is the maximum time to wait when establishing a new
	// connection to a peer.
	DialTimeout time.Duration

	// MaxPeers is the maximum number of concurrent peer connections
	// allowed.
	MaxPeers int

	// UploadSlots is the number of regular unchoke slots.
	UploadSlots int

	// RechokeInterval is the duration of how often to reevalute choke/unchoke
	// decisions.
	RechokeInterval time.Duration

	// OptimisticUnchokeInterval is the duration of how often to rotate the
	// optimistic unchoke.
	OptimisticUnchokeInterval time.Duration

	// PeerHeartbeatInterval is how often to send keep-alive messages to
	// peer to maintain the connection.
	PeerHeartbeatInterval time.Duration

	// PeerInactivityDuration is the minimum interval after which a peer connection
	// is considered inactive.
	PeerInactivityDuration time.Duration

	// PeerOutboxBacklog is the maximum messages that peer can have in its buffer to write.
	PeerOutboxBacklog int
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
	cfg             *Config
	log             *slog.Logger
	pieceCount      int
	peerMu          sync.RWMutex
	peers           map[netip.AddrPort]*Peer
	admitPeerCh     chan netip.AddrPort
	infoHash        [sha1.Size]byte
	clientID        [sha1.Size]byte
	stats           *SwarmStats
	cancel          context.CancelFunc
	closeOnce       sync.Once
	closed          atomic.Bool
	storage         *storage.Store
	scheduler       *scheduler.PieceScheduler
	refillPeersHook func()
}

type SwarmStats struct {
	TotalPeers       atomic.Uint32 // currently active peers in the map
	ConnectingPeers  atomic.Uint32 // dial/handshake in progress
	FailedConnection atomic.Uint32 // failed connection attempts
	UnchokedPeers    atomic.Uint32 // peers we are not choking
	InterestedPeers  atomic.Uint32 // peers we are interested in
	UploadingTo      atomic.Uint32 // peer with >0 upload rate
	DownloadingFrom  atomic.Uint32 // peer with >0 download rate

	TotalDownloaded atomic.Uint64 // sum of all peer's download
	TotalUploaded   atomic.Uint64 // sum of all peer's upload
	DownloadRate    atomic.Uint64 // B/s aggregate across peers
	UploadRate      atomic.Uint64 // B/s aggregate across peeres
}

type SwarmOpts struct {
	Config     *Config
	PieceCount int
	Log        *slog.Logger
	InfoHash   [sha1.Size]byte
	ClientID   [sha1.Size]byte
	Scheduler  *scheduler.PieceScheduler
}

type SwarmMetrics struct {
	TotalPeers       uint32 `json:"totalPeers"`
	ConnectingPeers  uint32 `json:"connectingPeers"`
	FailedConnection uint32 `json:"failedConnection"`
	UnchokedPeers    uint32 `json:"unchokedPeers"`
	InterestedPeers  uint32 `json:"interestedPeers"`
	UploadingTo      uint32 `json:"uploadingTo"`
	DownloadingFrom  uint32 `json:"downloadingFrom"`

	TotalDownloaded uint64 `json:"totalDownloaded"`
	TotalUploaded   uint64 `json:"totalUploaded"`
	DownloadRate    uint64 `json:"downloadRate"`
	UploadRate      uint64 `json:"uploadRate"`
}

func NewSwarm(opts *SwarmOpts) (*Swarm, error) {
	return &Swarm{
		cfg:         opts.Config,
		infoHash:    opts.InfoHash,
		clientID:    opts.ClientID,
		pieceCount:  opts.PieceCount,
		stats:       &SwarmStats{},
		scheduler:   opts.Scheduler,
		peers:       make(map[netip.AddrPort]*Peer),
		admitPeerCh: make(chan netip.AddrPort, opts.Config.MaxPeers),
		log:         opts.Log.With("src", "peer_swarm"),
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
	s.peerMu.RLock()
	defer s.peerMu.RUnlock()

	out := make([]PeerMetrics, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p.Stats())
	}

	return out
}

func (s *Swarm) Size() int {
	s.peerMu.RLock()
	defer s.peerMu.RUnlock()

	return len(s.peers)
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

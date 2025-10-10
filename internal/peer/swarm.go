package peer

import (
	"context"
	"crypto/sha1"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prxssh/rabbit/internal/config"
)

type Swarm struct {
	peerMu sync.RWMutex
	peers  map[netip.AddrPort]*Peer

	connecting   map[netip.AddrPort]struct{}
	connectingMu sync.RWMutex

	admitPeerCh chan netip.AddrPort

	log        *slog.Logger
	infoHash   [sha1.Size]byte
	pieceCount int
	stats      *SwarmStats

	cancel    context.CancelFunc
	closeOnce sync.Once
	closed    atomic.Bool
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
	Log        *slog.Logger
	PieceCount int
	InfoHash   [sha1.Size]byte
}

type SwarmMetrics struct {
	TotalPeers       uint32
	ConnectingPeers  uint32
	FailedConnection uint32
	UnchokedPeers    uint32
	InterestedPeers  uint32
	UploadingTo      uint32
	DownloadingFrom  uint32

	TotalDownloaded uint64
	TotalUploaded   uint64
	DownloadRate    uint64
	UploadRate      uint64
}

func NewSwarm(opts *SwarmOpts) (*Swarm, error) {
	cfg := config.Load()

	return &Swarm{
		infoHash:    opts.InfoHash,
		pieceCount:  opts.PieceCount,
		stats:       &SwarmStats{},
		peers:       make(map[netip.AddrPort]*Peer),
		admitPeerCh: make(chan netip.AddrPort, cfg.MaxPeers),
		log:         opts.Log.With("src", "swarm"),
	}, nil
}

func (s *Swarm) Run(ctx context.Context) error {
	defer s.Stop()

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	var wg sync.WaitGroup
	wg.Go(func() { s.maintenanceLoop(ctx) })
	wg.Go(func() { s.admitPeersLoop(ctx) })
	wg.Go(func() { s.statsLoop(ctx) })
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

func (s *Swarm) Stop() {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.cancel()

		close(s.admitPeerCh)

		s.log.Debug("stopped")
	})
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

func (s *Swarm) BroadcastHAVE(piece uint32) {
	s.peerMu.RLock()
	for _, peer := range s.peers {
		peer.SendHave(piece)
	}
	s.peerMu.RUnlock()
}

func (s *Swarm) AddPeer(ctx context.Context, addr netip.AddrPort) error {
	if s.closed.Load() {
		return nil
	}

	maxAllowedPeers := uint32(config.Load().MaxPeers)

	s.peerMu.RLock()
	_, exists := s.peers[addr]
	s.peerMu.RUnlock()

	if exists || s.stats.TotalPeers.Load() >= maxAllowedPeers {
		return nil
	}

	s.stats.ConnectingPeers.Add(1)
	peer, err := NewPeer(ctx, addr, &PeerOpts{
		InfoHash:   s.infoHash,
		Log:        s.log,
		PieceCount: s.pieceCount,
	})
	s.stats.ConnectingPeers.Add(^uint32(0))
	if err != nil {
		s.stats.FailedConnection.Add(1)
		return err
	}

	if s.stats.TotalPeers.Load() >= maxAllowedPeers {
		return nil
	}

	s.peerMu.Lock()
	s.peers[addr] = peer
	s.peerMu.Unlock()

	s.stats.TotalPeers.Add(1)

	go func(peer *Peer) {
		defer func() {
			peer.Stop()

			s.peerMu.Lock()
			delete(s.peers, peer.addr)
			s.peerMu.Unlock()

			s.stats.TotalPeers.Add(^uint32(0))
		}()

		peer.Run(ctx)
	}(peer)

	return nil
}

func (s *Swarm) RemovePeer(addr netip.AddrPort) {
	s.peerMu.Lock()
	peer := s.peers[addr]
	delete(s.peers, addr)
	s.peerMu.Unlock()

	peer.Stop()
	s.stats.TotalPeers.Add(^uint32(0))
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
			maxIdle := config.Load().PeerInactivityDuration
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
			l.Warn("context done, exiting", "error", ctx.Err())
			return nil

		case peerAddr, ok := <-s.admitPeerCh:
			if !ok {
				l.Error("admit peers queue closed, existing")
				return nil
			}

			if err := s.AddPeer(ctx, peerAddr); err != nil {
				l.Debug("failed to add peer", "addr", peerAddr, "error", err)
				continue
			}
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

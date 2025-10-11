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
	"github.com/prxssh/rabbit/internal/piece"
	"github.com/prxssh/rabbit/internal/storage"
	"github.com/prxssh/rabbit/internal/utils/bitfield"
)

type Swarm struct {
	peerMu          sync.RWMutex
	peers           map[netip.AddrPort]*Peer
	admitPeerCh     chan netip.AddrPort
	log             *slog.Logger
	infoHash        [sha1.Size]byte
	pieceCount      int
	stats           *SwarmStats
	cancel          context.CancelFunc
	closeOnce       sync.Once
	closed          atomic.Bool
	storage         *storage.Store
	piecePicker     *piece.Picker
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
	Log         *slog.Logger
	PieceCount  int
	InfoHash    [sha1.Size]byte
	Storage     *storage.Store
	PiecePicker *piece.Picker
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

// PieceStates returns the current state of each piece as integer codes:
// 0 = NotStarted, 1 = InProgress, 2 = Completed.
func (s *Swarm) PieceStates() []int {
	if s.piecePicker == nil {
		return nil
	}
	states := s.piecePicker.PieceStates()
	out := make([]int, len(states))
	for i, st := range states {
		out[i] = int(st)
	}
	return out
}

func NewSwarm(opts *SwarmOpts) (*Swarm, error) {
	cfg := config.Load()

	return &Swarm{
		infoHash:    opts.InfoHash,
		pieceCount:  opts.PieceCount,
		stats:       &SwarmStats{},
		storage:     opts.Storage,
		piecePicker: opts.PiecePicker,
		peers:       make(map[netip.AddrPort]*Peer),
		admitPeerCh: make(chan netip.AddrPort, cfg.MaxPeers),
		log:         opts.Log.With("src", "swarm"),
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
	wg.Go(func() { s.peerRequestTimeoutLoop(ctx) })
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

func (s *Swarm) BroadcastHAVE(piece uint32) {
	s.peerMu.RLock()
	for _, peer := range s.peers {
		peer.SendHave(piece)
	}
	s.peerMu.RUnlock()
}

func (s *Swarm) AddPeer(ctx context.Context, addr netip.AddrPort) (*Peer, error) {
	if s.closed.Load() {
		return nil, nil
	}

	s.peerMu.RLock()
	_, dup := s.peers[addr]
	s.peerMu.RUnlock()
	if dup {
		return nil, nil
	}

	if s.stats.TotalPeers.Load() >= uint32(config.Load().MaxPeers) {
		return nil, nil
	}

	s.stats.ConnectingPeers.Add(1)

	peer, err := NewPeer(ctx, addr, &PeerOpts{
		InfoHash:     s.infoHash,
		Log:          s.log,
		PieceCount:   s.pieceCount,
		OnBitfield:   s.onBitfield,
		OnHave:       s.onHave,
		OnDisconnect: s.onDisconnect,
		OnHandshake:  s.onPeerHandshake,
		OnPiece:      s.onBlockReceived,
		RequestWork:  s.requestWork,
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

func (s *Swarm) peerRequestTimeoutLoop(ctx context.Context) error {
	l := s.log.With("component", "peer request timeout loop")
	l.Debug("started")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Warn("context done, exiting", "error", ctx.Err())
			return nil

		case <-ticker.C:
			timedOutRequests := s.piecePicker.CheckTimeouts()

			for _, req := range timedOutRequests {
				peer, ok := s.GetPeer(req.Peer)
				if !ok {
					continue
				}

				l.Error("piece timed out, cancelling", "piece", req.Piece)
				peer.SendCancel(req.Piece, req.Begin, req.Length)
			}
		}
	}
}

func (s *Swarm) onPeerHandshake(addr netip.AddrPort) {
	peer, ok := s.GetPeer(addr)
	if !ok {
		return
	}

	peer.SendBitfield(s.piecePicker.Bitfield())
}

func (s *Swarm) onBitfield(addr netip.AddrPort, bf bitfield.Bitfield) {
	peer, ok := s.GetPeer(addr)
	if !ok {
		return
	}
	s.piecePicker.OnPeerBitfield(addr, bf)

	// Signal interest based on availability vs our bitfield
	if s.piecePicker.InterestedInPeer(addr) {
		peer.SendInterested()
	} else {
		peer.SendNotInterested()
	}

	// Only attempt requests if the peer has unchoked us
	if !peer.PeerChoking() {
		for _, req := range s.piecePicker.NextForPeer(addr) {
			peer.SendRequest(req.Piece, req.Begin, req.Length)
		}
	}
}

func (s *Swarm) onHave(addr netip.AddrPort, piece int) {
	peer, ok := s.GetPeer(addr)
	if !ok {
		return
	}
	s.piecePicker.OnPeerHave(addr, piece)

	// Update interest when new availability arrives
	if s.piecePicker.InterestedInPeer(addr) {
		peer.SendInterested()
	} else {
		peer.SendNotInterested()
	}

	// Only request blocks when not choked
	if !peer.PeerChoking() {
		for _, req := range s.piecePicker.NextForPeer(addr) {
			peer.SendRequest(req.Piece, req.Begin, req.Length)
		}
	}
}

func (s *Swarm) onDisconnect(addr netip.AddrPort) {
	s.piecePicker.OnPeerGone(addr)
}

func (s *Swarm) onBlockReceived(addr netip.AddrPort, pieceIdx, begin int, data []byte) {
	completed := s.piecePicker.OnBlockReceived(addr, pieceIdx, begin)

	pieceLen, blockLen, isLastPiece := s.piecePicker.PieceLength(pieceIdx)
	blockIdx := piece.BlockIndexForBegin(begin, int(pieceLen), int(blockLen))

	s.storage.BufferBlock(data, storage.BlockInfo{
		PieceIndex:  pieceIdx,
		BlockIndex:  blockIdx,
		PieceLength: pieceLen,
		BlockLength: blockLen,
		IsLastPiece: isLastPiece,
	})

	if !completed {
		return
	}

	hash := s.piecePicker.PieceHash(pieceIdx)
	if err := s.storage.FlushPiece(pieceIdx, hash); err != nil {
		s.log.Error("piece verification failed", "error", err.Error(), "piece", pieceIdx)
		return
	}

	s.piecePicker.MarkPieceVerified(pieceIdx, true)
	s.BroadcastHAVE(uint32(pieceIdx))
}

func (s *Swarm) requestWork(addr netip.AddrPort) {
	peer, ok := s.GetPeer(addr)
	if !ok {
		return
	}

	for _, req := range s.piecePicker.NextForPeer(addr) {
		peer.SendRequest(req.Piece, req.Begin, req.Length)
	}
}

package peer

import (
	"context"
	"crypto/sha1"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prxssh/rabbit/pkg/config"
	"github.com/prxssh/rabbit/pkg/piece"
	"github.com/prxssh/rabbit/pkg/utils/bitfield"
	"golang.org/x/sync/errgroup"
)

type Manager struct {
	log             *slog.Logger
	peerMu          sync.RWMutex
	peers           map[netip.AddrPort]*Peer
	peerCount       atomic.Int32
	pieceCount      int
	pieceLength     int64
	size            int64
	clientID        [sha1.Size]byte
	infoHash        [sha1.Size]byte
	pieceManager    *piece.Manager
	peerCh          chan netip.AddrPort
	dialSem         chan struct{}
	refillPeerQ     chan<- struct{}
	wantWorkQ       chan netip.AddrPort
	totalDownloaded atomic.Int64
	totalUploaded   atomic.Int64
	downloadRate    atomic.Int64
	uploadRate      atomic.Int64
	lastSampleUnix  atomic.Int64
	lastDownloaded  atomic.Int64
	lastUploaded    atomic.Int64
}

type Stats struct {
	Peers           []PeerMetrics `json:"peers"`
	TotalDownloaded int64         `json:"downloaded"`
	TotalUploaded   int64         `json:"uploaded"`
	DownloadRate    int64         `json:"downloadRate"`
	UploadRate      int64         `json:"uploadRate"`
	PieceStates     []int         `json:"pieceStates"`
}

type ManagerOpts struct {
	ClientID     [sha1.Size]byte
	InfoHash     [sha1.Size]byte
	Pieces       int
	PieceLength  int64
	Size         int64
	RefillPeerQ  chan<- struct{}
	Log          *slog.Logger
	PieceManager *piece.Manager
}

func NewManager(opts *ManagerOpts) *Manager {
	m := &Manager{
		size:         opts.Size,
		pieceCount:   opts.Pieces,
		clientID:     opts.ClientID,
		infoHash:     opts.InfoHash,
		pieceManager: opts.PieceManager,
		pieceLength:  opts.PieceLength,
		refillPeerQ:  opts.RefillPeerQ,
		peers:        make(map[netip.AddrPort]*Peer),
		log:          opts.Log.With("src", "peer_manager"),
		dialSem:      make(chan struct{}, config.Load().MaxPeers>>1),
		peerCh:       make(chan netip.AddrPort, config.Load().MaxPeers),
		wantWorkQ:    make(chan netip.AddrPort, 1024),
	}
	m.lastSampleUnix.Store(time.Now().UnixNano())

	return m
}

func (m *Manager) Run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error { return m.processPeersLoop(ctx) })
	eg.Go(func() error { return m.monitorPeerHeartbeat(ctx) })
	eg.Go(func() error { return m.sampleRatesLoop(ctx) })
	eg.Go(func() error { return m.refillPeersLoop(ctx) })
	eg.Go(func() error { return m.dispatcherLoop(ctx) })
	eg.Go(func() error { return m.monitorBlockTimeouts(ctx) })

	eg.Go(func() error {
		<-ctx.Done()
		m.cleanup()

		return nil
	})

	return eg.Wait()
}

func (m *Manager) Stats() Stats {
	peerMetrics := make([]PeerMetrics, 0, m.peerCount.Load())

	m.peerMu.RLock()
	for _, peer := range m.peers {
		peerMetrics = append(peerMetrics, peer.Metrics())
	}
	m.peerMu.RUnlock()

	pieceStates := m.pieceManager.PieceStates()
	intStates := make([]int, len(pieceStates))
	for i, s := range pieceStates {
		intStates[i] = int(s)
	}

	return Stats{
		Peers:           peerMetrics,
		PieceStates:     intStates,
		TotalDownloaded: m.totalDownloaded.Load(),
		TotalUploaded:   m.totalUploaded.Load(),
		DownloadRate:    m.downloadRate.Load(),
		UploadRate:      m.uploadRate.Load(),
	}
}

func (m *Manager) AdmitPeers(peers []netip.AddrPort) {
	for _, addr := range peers {
		if m.havePeer(addr) {
			continue
		}

		select {
		case m.peerCh <- addr:
		default:
			m.log.Warn("peer queue full; dropping", "addr", addr.String())
		}
	}
}

func (m *Manager) BroadcastHave(pieceIdx int, excludePeer netip.AddrPort) {
	m.peerMu.RLock()
	defer m.peerMu.RUnlock()

	for addr, peer := range m.peers {
		if addr == excludePeer {
			continue
		}

		peer.sendHave(pieceIdx)
	}
}

func (m *Manager) processPeersLoop(ctx context.Context) error {
	l := m.log.With("src", "peer_manager.processPeersLoop")
	l.Debug("started")

	for {
		select {
		case <-ctx.Done():
			l.Error("context canceled; exiting", "error", ctx.Err())
			return ctx.Err()
		case addr, ok := <-m.peerCh:
			if !ok {
				l.Warn("peer channel closed; exiting")
				return nil
			}

			if m.havePeer(addr) || m.countPeers() >= config.Load().MaxPeers {
				continue
			}

			select {
			case m.dialSem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}

			go func(addr netip.AddrPort) {
				defer func() { <-m.dialSem }()

				dctx, cancel := context.WithTimeout(ctx, config.Load().DialTimeout)
				defer cancel()

				peer, err := NewPeer(dctx, addr, &PeerOpts{
					PiecesBF: m.pieceManager.Bitfield(),
					ClientID: m.clientID,
					InfoHash: m.infoHash,
					Pieces:   m.pieceCount,
					Log:      m.log,
					Hooks: Hooks{
						OnNeedWork:        m.onNeedWork,
						OnHave:            m.onHave,
						OnBitfield:        m.onBitfield,
						OnRequest:         m.onRequest,
						OnBlockReceived:   m.onBlockReceived,
						OnCheckInterested: m.onCheckInterested,
					},
				})
				if err != nil {
					l.Debug("peer connection failed",
						"error", err,
						"addr", addr,
					)
					return
				}

				if m.havePeer(addr) || m.countPeers() >= config.Load().MaxPeers {
					peer.cleanup()
					return
				}

				m.addPeer(addr, peer)
				peer.Run(ctx)
				m.removePeer(addr)
			}(addr)
		}
	}
}

func (m *Manager) monitorPeerHeartbeat(ctx context.Context) error {
	l := m.log.With("src", "peer_manager.monitorPeerHearbeat")
	l.Debug("started")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Error("ctx done; exiting", "error", ctx.Err())
			return ctx.Err()

		case <-ticker.C:
			nowNano := time.Now().UnixNano()
			idleCutoff := config.Load().PeerHeartbeatInterval
			count := 0

			m.peerMu.Lock()
			for addr, peer := range m.peers {
				last := peer.stats.LastActiveUnix.Load()
				if last == 0 {
					continue
				}

				idle := time.Duration(nowNano - last)
				if idle >= idleCutoff {
					m.removePeer(addr)
					count++
				}
			}
			defer m.peerMu.Unlock()

			l.Debug("purged inactive peers", "count", count)
		}
	}
}

func (m *Manager) dispatcherLoop(ctx context.Context) error {
	l := m.log.With("src", "peer_manager.dispatcherLoop")
	l.Debug("started")

	for {
		select {
		case <-ctx.Done():
			l.Error("ctx done; exiting", "error", ctx.Err())
			return ctx.Err()
		case addr, ok := <-m.wantWorkQ:
			if !ok {
				l.Warn("queue closed; existing")
				return nil
			}

			m.peerMu.RLock()
			peer := m.peers[addr]
			m.peerMu.RUnlock()

			if peer == nil {
				continue
			}

			has := peer.Bitfield()
			unchoked := !peer.peerChoking()
			interested := peer.amInterested()

			if !unchoked || !interested {
				l.Debug("peer not ready",
					"addr", addr,
					"unchoked", unchoked,
					"interested", interested,
				)
				continue
			}

			capacity := m.pieceManager.CapacityForPeer(addr)
			if capacity <= 0 {
				l.Debug("peer capacity full; will retry", "addr", addr)
				// Re-queue with a small delay to avoid busy loop
				time.AfterFunc(100*time.Millisecond, func() {
					select {
					case m.wantWorkQ <- addr:
					default:
					}
				})
				continue
			}

			reqs := m.pieceManager.NextForPeerN(
				&piece.PeerView{Peer: addr, Has: has, Unchoked: true},
				capacity,
			)
			if len(reqs) == 0 {
				if m.pieceManager.HasAnyWantedPiece(has) {
					select {
					case m.wantWorkQ <- addr:
					default:
					}
					l.Debug("no work now; re-queued", "addr", addr)
				}
				continue
			}

			sent := 0
		sendLoop:
			for i := range reqs {
				select {
				case peer.pieceReqQ <- reqs[i]:
					sent++
				default:
					for j := i; j < len(reqs); j++ {
						m.pieceManager.Unassign(addr, reqs[j].Piece, reqs[j].Begin)
					}
					break sendLoop
				}
			}

			if sent == 0 {
				select {
				case m.wantWorkQ <- addr:
				default:
				}
			}

			l.Debug(
				"dispatched requests",
				"peer",
				addr,
				"sent",
				sent,
				"asked",
				len(reqs),
			)
		}
	}
}

func (m *Manager) sampleRatesLoop(ctx context.Context) error {
	l := m.log.With("src", "peer_manager.smapleRatesLoop")
	l.Debug("started")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	m.lastDownloaded.Store(m.totalDownloaded.Load())
	m.lastUploaded.Store(m.totalUploaded.Load())
	m.lastSampleUnix.Store(time.Now().UnixNano())

	for {
		select {
		case <-ctx.Done():
			l.Error("ctx done; exiting", "error", ctx.Err())
			return ctx.Err()
		case now := <-ticker.C:
			nowUnix := now.UnixNano()
			prevUnix := m.lastSampleUnix.Swap(nowUnix)
			elapsed := float64(nowUnix-prevUnix) / 1e9
			if elapsed <= 0 {
				continue
			}

			td := m.totalDownloaded.Load()
			tu := m.totalUploaded.Load()
			pd := m.lastDownloaded.Swap(td)
			pu := m.lastUploaded.Swap(tu)

			dDelta := td - pd
			uDelta := tu - pu

			m.downloadRate.Store(int64(float64(dDelta) / elapsed))
			m.uploadRate.Store(int64(float64(uDelta) / elapsed))
		}
	}
}

func (m *Manager) refillPeersLoop(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if m.countPeers() >= 5 {
				continue
			}

			m.refillPeerQ <- struct{}{}
		}
	}
}

func (m *Manager) monitorBlockTimeouts(ctx context.Context) error {
	l := m.log.With("src", "peer_manager.monitorBlockTimeouts")
	l.Debug("started")

	// Check timeouts frequently to keep pipeline moving
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Error("ctx done; exiting", "error", ctx.Err())
			return ctx.Err()
		case <-ticker.C:
			timeout := config.Load().RequestTimeout
			timedOut := m.pieceManager.ScanAndReclaimTimedOutBlocks(timeout)

			if len(timedOut) > 0 {
				l.Info("reclaimed timed-out blocks", "count", len(timedOut))

				// Re-queue the peers that timed out first (highest priority)
				for _, to := range timedOut {
					select {
					case m.wantWorkQ <- to.Peer:
					default:
					}
				}

				// Best-effort nudge others as well to keep pipeline full
				m.peerMu.RLock()
				for addr := range m.peers {
					select {
					case m.wantWorkQ <- addr:
					default:
					}
				}
				m.peerMu.RUnlock()
			}
		}
	}
}

func (m *Manager) havePeer(addr netip.AddrPort) bool {
	m.peerMu.RLock()
	defer m.peerMu.RUnlock()

	_, ok := m.peers[addr]
	return ok
}

func (m *Manager) addPeer(addr netip.AddrPort, peer *Peer) {
	m.peerMu.Lock()
	defer m.peerMu.Unlock()

	m.peers[addr] = peer
}

func (m *Manager) removePeer(addr netip.AddrPort) {
	m.peerMu.Lock()
	peer, ok := m.peers[addr]
	if !ok {
		m.peerMu.Unlock()
		return
	}

	bf := peer.Bitfield()
	delete(m.peers, addr)
	m.peerMu.Unlock()

	peer.cleanup()
	m.pieceManager.OnPeerGone(addr, bf)
}

func (m *Manager) countPeers() int {
	m.peerMu.RLock()
	defer m.peerMu.RUnlock()

	return len(m.peers)
}

func (m *Manager) cleanup() {
	peers := make([]*Peer, 0, len(m.peers))

	m.peerMu.Lock()
	for _, peer := range m.peers {
		peers = append(peers, peer)
	}
	m.peerMu.Unlock()

	var wg sync.WaitGroup
	for _, peer := range peers {
		wg.Add(1)
		go func(p *Peer) {
			defer wg.Done()
			p.cleanup()
		}(peer)
	}
	wg.Wait()

	m.peerMu.Lock()
	m.peers = make(map[netip.AddrPort]*Peer)
	m.peerMu.Unlock()
}

func (m *Manager) onNeedWork(addr netip.AddrPort) {
	select {
	case m.wantWorkQ <- addr:
		m.log.Info("requesting work for peer", "addr", addr)
	default:
	}
}

func (m *Manager) onHave(addr netip.AddrPort, piece uint32) {
	m.pieceManager.OnPeerHave(addr, piece)
}

func (m *Manager) onBitfield(addr netip.AddrPort, bf bitfield.Bitfield) {
	m.pieceManager.OnPeerBitfield(addr, bf)
}

func (m *Manager) onRequest(addr netip.AddrPort, piece, begin, length uint32) ([]byte, error) {
	return m.pieceManager.ReadPiece(int(piece), int(begin), int(length))
}

func (m *Manager) onBlockReceived(addr netip.AddrPort, piece, begin uint32, data []byte) {
	completed, cancels, err := m.pieceManager.OnBlockReceived(
		addr,
		int(piece),
		int(begin),
		data,
	)
	if err != nil {
		m.log.Error("failed OnBlockReceived", "error", err, "piece", piece)
		return
	}

	for _, cancel := range cancels {
		m.peerMu.RLock()
		peer, ok := m.peers[cancel.Peer]
		m.peerMu.RUnlock()

		if !ok {
			continue
		}

		m.pieceManager.OnCancel(cancel.Peer, cancel.Piece, cancel.Begin)
		peer.sendCancel(cancel.Piece, cancel.Begin, cancel.Length)
	}

	if completed {
		m.BroadcastHave(int(piece), addr)
	}
}

func (m *Manager) onCheckInterested(bf bitfield.Bitfield) bool {
	return m.pieceManager.HasAnyWantedPiece(bf)
}

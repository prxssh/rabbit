package peer

import (
	"context"
	"crypto/sha1"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/prxssh/rabbit/pkg/config"
	"github.com/prxssh/rabbit/pkg/piece"
	"golang.org/x/sync/errgroup"
)

// Manager coordinates peer connections and data transfer for a single torrent.
//
// It handles peer discovery, connection management, block requests, and
// integrates with the piece picker and storage layer.
type Manager struct {
	// log is the structured logger for peer management events.
	log *slog.Logger

	// peerMu protects the peers map from concurrent access.
	peerMu sync.RWMutex

	// peers maps peer addresses to their active connection state.
	peers map[netip.AddrPort]*Peer

	// pieceCount is the total number of pieces in the torrent.
	pieceCount int

	// clientID is this client's unique 20-byte peer ID sent during
	// handshake.
	clientID [sha1.Size]byte

	// pieceLength is the byte size of each piece (except possibly the
	// last).
	pieceLength int64

	// size is the torrent size in bytes
	size int64

	// infoHash is the SHA-1 hash identifying this torrent.
	infoHash [sha1.Size]byte

	// pieceManager manages piece selection, downloading, and saving them to
	// disk.
	pieceManager *piece.Manager

	// peerCh receives candidate peer addresses to connect to. Buffered to
	// prevent blocking callers of AdmitPeers.
	peerCh chan netip.AddrPort

	// dialSem is a semaphore limiting concurrent outbound connection
	// attempts to prevent resource exhaustion and thundering herd issues.
	dialSem chan struct{}

	refillPeerQ chan<- struct{}

	statsMu         sync.RWMutex
	totalDownloaded int64
	totalUploaded   int64
	downloadRate    int64
	uploadRate      int64

	// rate sampling state
	lastSampleAt   time.Time
	lastDownloaded int64
	lastUploaded   int64

	// IPv6 backoff window if the host has no IPv6 route
	ipv6BlockUntil time.Time
}

type Stats struct {
	Peers           []PeerStats `json:"peers"`
	TotalDownloaded int64       `json:"downloaded"`
	TotalUploaded   int64       `json:"uploaded"`
	DownloadRate    int64       `json:"downloadRate"`
	UploadRate      int64       `json:"uploadRate"`
	PieceStates     []int       `json:"pieceStates"`
}

func NewManager(
	clientID, infoHash [sha1.Size]byte,
	pieceCount int,
	pieceLength,
	size int64,
	pieceManager *piece.Manager,
	refillPeerQ chan<- struct{},
	log *slog.Logger,
) *Manager {
	log = log.With("src", "peer_manager")

	return &Manager{
		log:          log,
		clientID:     clientID,
		infoHash:     infoHash,
		pieceCount:   pieceCount,
		pieceManager: pieceManager,
		pieceLength:  pieceLength,
		size:         size,
		dialSem:      make(chan struct{}, config.Load().MaxPeers>>1),
		peers:        make(map[netip.AddrPort]*Peer),
		peerCh:       make(chan netip.AddrPort, config.Load().MaxPeers),
		refillPeerQ:  refillPeerQ,
	}
}

func (m *Manager) Run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error { return m.processPeersLoop(ctx) })
	eg.Go(func() error { return m.monitorPeerHeartbeat(ctx) })
	eg.Go(func() error { return m.sampleRatesLoop(ctx) })
	eg.Go(func() error { return m.refillPeersLoop(ctx) })

	eg.Go(func() error {
		<-ctx.Done()
		m.cleanup()

		return nil
	})

	return eg.Wait()
}

func (m *Manager) Stats() Stats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()

	peerStats := make([]PeerStats, 0, len(m.peers))
	for _, peer := range m.peers {
		peerStats = append(peerStats, peer.Stats())
	}

	pieceStates := m.pieceManager.PieceStates()
	intStates := make([]int, len(pieceStates))
	for i, state := range pieceStates {
		intStates[i] = int(state)
	}

	return Stats{
		Peers:           peerStats,
		TotalDownloaded: m.totalDownloaded,
		TotalUploaded:   m.totalUploaded,
		DownloadRate:    m.downloadRate,
		UploadRate:      m.uploadRate,
		PieceStates:     intStates,
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
			m.log.Warn(
				"peer queue full; dropping",
				"addr", addr.String(),
			)
		}
	}
}

// BroadcastHave sends a HAVE message for the specified piece to all connected
// peers except the excluded peer.
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
	for {
		select {
		case <-ctx.Done():
			m.log.Info("ctx canceled")

			return ctx.Err()
		case addr, ok := <-m.peerCh:
			if !ok {
				m.log.Info("peer channel closed")
				return nil
			}

			if m.havePeer(addr) ||
				m.peerCount() >= config.Load().MaxPeers {
				continue
			}

			select {
			case m.dialSem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}

			go func(addr netip.AddrPort) {
				defer func() { <-m.dialSem }()

				dctx, cancel := context.WithTimeout(
					ctx,
					config.Load().DialTimeout,
				)
				defer cancel()

				peer, err := dialPeer(dctx, m, addr)
				if err != nil {
					m.log.Error(
						"dial failed",
						"addr", addr.String(),
						"error", err.Error(),
					)
					return
				}

				if m.havePeer(addr) ||
					m.peerCount() >= config.Load().MaxPeers {
					peer.cleanup()
					return
				}

				m.peerAdd(addr, peer)
				peer.run(ctx)
				m.removePeer(addr)
			}(addr)
		}
	}
}

func (m *Manager) havePeer(addr netip.AddrPort) bool {
	m.peerMu.RLock()
	defer m.peerMu.RUnlock()

	_, ok := m.peers[addr]
	return ok
}

func (m *Manager) peerCount() int {
	m.peerMu.RLock()
	defer m.peerMu.RUnlock()

	return len(m.peers)
}

func (m *Manager) peerAdd(addr netip.AddrPort, peer *Peer) {
	m.peerMu.Lock()
	defer m.peerMu.Unlock()

	m.peers[addr] = peer
}

func (m *Manager) removePeer(addr netip.AddrPort) {
	m.peerMu.Lock()
	defer m.peerMu.Unlock()

	delete(m.peers, addr)
}

func (m *Manager) monitorPeerHeartbeat(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			count := m.purgeInactivePeers()
			m.log.Info(
				"purged inactive peers",
				"deleted", count,
				"remaining_peers", m.peerCount(),
			)
		}
	}
}

func (m *Manager) purgeInactivePeers() int {
	count := 0

	m.peerMu.Lock()
	defer m.peerMu.Unlock()

	for addr, peer := range m.peers {
		if time.Since(
			peer.LastActiveAt(),
		) < config.Load().KeepAliveInterval {
			continue
		}

		peer.cleanup()
		delete(m.peers, addr)
		count++
	}

	return count
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

func (m *Manager) updateTotalDownloaded(size int) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()

	m.totalDownloaded += int64(size)
}

func (m *Manager) updateTotalUploaded(size int) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()

	m.totalUploaded += int64(size)
}

// sampleRatesLoop periodically computes aggregate download/upload rates based
// on
// byte deltas over time and stores them in the manager under a lock. This keeps
// Stats.DownloadRate/UploadRate fresh for the UI.
func (m *Manager) sampleRatesLoop(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// initialize sampling baseline
	m.statsMu.Lock()
	m.lastSampleAt = time.Now()
	m.lastDownloaded = m.totalDownloaded
	m.lastUploaded = m.totalUploaded
	m.statsMu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			m.statsMu.Lock()
			elapsed := now.Sub(m.lastSampleAt).Seconds()
			if elapsed <= 0 {
				// avoid div-by-zero; skip this tick
				m.statsMu.Unlock()
				continue
			}

			dDelta := m.totalDownloaded - m.lastDownloaded
			uDelta := m.totalUploaded - m.lastUploaded

			m.downloadRate = int64(float64(dDelta) / elapsed)
			m.uploadRate = int64(float64(uDelta) / elapsed)

			m.lastDownloaded = m.totalDownloaded
			m.lastUploaded = m.totalUploaded
			m.lastSampleAt = now
			m.statsMu.Unlock()
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
			if m.peerCount() >= 5 {
				continue
			}

			m.refillPeerQ <- struct{}{}
		}
	}
}

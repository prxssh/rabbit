package peer

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/prxssh/rabbit/pkg/piece"
	"github.com/prxssh/rabbit/pkg/storage"
	"golang.org/x/sync/errgroup"
)

// Config holds peer manager configuration parameters.
type Config struct {
	// MaxPeers is the maximum number of concurrent peer connections
	// allowed.
	MaxPeers int

	// MaxInflightRequestsPerPeer limits how many requests can be
	// outstanding to a single peer at once.
	MaxInflightRequestsPerPeer int

	// MaxRequestsPerPiece caps the number of duplicate requests for the
	// same piece across all peers to prevent over-downloading.
	MaxRequestsPerPiece int

	// PeerHeartbeatInterval is how often to send keep-alive messages to
	// peer to maintain the connection.
	PeerHeartbeatInterval time.Duration

	// ReadTimeout is the maximum time to wait for data from a peer before
	// considering the connection stalled.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum time to wait when sending data to a peer
	// before considering the connection stalled.
	WriteTimeout time.Duration

	// DialTimeout is the maximum time to wait when establishing a new
	// connection to a peer.
	DialTimeout time.Duration

	// KeepAliveInterval is how often to check peer connection health and
	// close idle connections.
	KeepAliveInterval time.Duration

	// PeerOutboundQueueBacklog is the maximum messages that peer can have
	// in its buffer.
	PeerOutboundQueueBacklog int
}

func withDefaultConfig() Config {
	return Config{
		MaxPeers:                   50,
		MaxInflightRequestsPerPeer: 5,
		MaxRequestsPerPiece:        4,
		PeerHeartbeatInterval:      2 * time.Minute,
		ReadTimeout:                45 * time.Second,
		WriteTimeout:               45 * time.Second,
		DialTimeout:                30 * time.Second,
		KeepAliveInterval:          2 * time.Minute,
		PeerOutboundQueueBacklog:   25,
	}
}

// Manager coordinates peer connections and data transfer for a single torrent.
//
// It handles peer discovery, connection management, block requests, and
// integrates with the piece picker and storage layer.
type Manager struct {
	// cfg holds the manager's configuration parameters.
	cfg Config

	// log is the structured logger for peer management events.
	log *slog.Logger

	// peerMut protects the peers map from concurrent access.
	peerMut sync.RWMutex

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

	// infoHash is the SHA-1 hash identifying this torrent.
	infoHash [sha1.Size]byte

	// picker selects which pieces/blocks to request from peers.
	picker *piece.Picker

	// storage handles reading and writing piece data to disk.
	storage *storage.Disk

	// peerCh receives candidate peer addresses to connect to. Buffered to
	// prevent blocking callers of AdmitPeers.
	peerCh chan netip.AddrPort

	// dialSem is a semaphore limiting concurrent outbound connection
	// attempts to prevent resource exhaustion and thundering herd issues.
	dialSem chan struct{}

	statsMut        sync.RWMutex
	totalDownloaded int64
	totalUploaded   int64
	downloadRate    int64
	uploadRate      int64
}

type Stats struct {
	ActivePeers     int
	TotalDownloaded int64
	TotalUploaded   int64
	DownloadRate    int64
	UploadRate      int64
}

func NewManager(
	clientID, infoHash [sha1.Size]byte,
	pieceCount int,
	pieceLength int64,
	picker *piece.Picker,
	storage *storage.Disk,
	cfg *Config,
) *Manager {
	c := withDefaultConfig()
	if cfg != nil {
		c = *cfg
	}

	log := slog.Default().
		With("src", "peer_manager", "info_hash", hex.EncodeToString(infoHash[:]))

	return &Manager{
		cfg:         c,
		log:         log,
		clientID:    clientID,
		infoHash:    infoHash,
		pieceCount:  pieceCount,
		picker:      picker,
		pieceLength: pieceLength,
		storage:     storage,
		dialSem:     make(chan struct{}, c.MaxPeers>>1),
		peers:       make(map[netip.AddrPort]*Peer),
		peerCh:      make(chan netip.AddrPort, c.MaxPeers),
	}
}

func (m *Manager) Run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error { return m.processPeersLoop(ctx) })
	eg.Go(func() error { return m.monitorPeerHeartbeat(ctx) })

	eg.Go(func() error {
		<-ctx.Done()
		m.cleanup()

		return nil
	})

	return eg.Wait()
}

func (m *Manager) Stats() Stats {
	m.statsMut.RLock()
	defer m.statsMut.RUnlock()

	return Stats{
		ActivePeers:     m.peerCount(),
		TotalDownloaded: m.totalDownloaded,
		TotalUploaded:   m.totalUploaded,
		DownloadRate:    m.downloadRate,
		UploadRate:      m.uploadRate,
	}
}

func (m *Manager) GetAllPeersStats() []PeerStats {
	stats := make([]PeerStats, 0, len(m.peers))

	m.peerMut.Lock()
	defer m.peerMut.Unlock()

	for _, peer := range m.peers {
		stats = append(stats, peer.Stats())
	}
	return stats
}

func (m *Manager) GetPeerStats(addr netip.AddrPort) (PeerStats, bool) {
	m.peerMut.Lock()
	peer, ok := m.peers[addr]
	m.peerMut.Unlock()

	if !ok {
		return PeerStats{}, false
	}
	return peer.Stats(), true
}

func (m *Manager) AdmitPeers(peers []netip.AddrPort) {
	for _, addr := range peers {
		select {
		case m.peerCh <- addr:
		default:
			m.log.Warn(
				"peer queue full; dropping",
				slog.String("addr", addr.String()),
			)
		}
	}
}

// BroadcastHave sends a HAVE message for the specified piece to all connected
// peers except the excluded peer.
func (m *Manager) BroadcastHave(pieceIdx int, excludePeer netip.AddrPort) {
	m.peerMut.Lock()
	defer m.peerMut.Unlock()

	count := 0
	for addr, peer := range m.peers {
		if addr == excludePeer {
			continue
		}

		select {
		case peer.outq <- MessageHave(pieceIdx):
			count++

		default:
			m.log.Warn(
				"failed to broadcast HAVE, queue full",
				slog.String("peer", addr.String()),
				slog.Int("piece", pieceIdx),
			)
		}
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
				m.peerCount() >= m.cfg.MaxPeers {
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
					m.cfg.DialTimeout,
				)
				defer cancel()

				peer, err := dialPeer(dctx, m, addr)
				if err != nil {
					return
				}

				if m.havePeer(addr) ||
					m.peerCount() >= m.cfg.MaxPeers {
					_ = peer.cleanup()
					return
				}

				m.peerAdd(addr, peer)
				peer.run(ctx)
			}(addr)
		}
	}
}

func (m *Manager) havePeer(addr netip.AddrPort) bool {
	m.peerMut.RLock()
	defer m.peerMut.RUnlock()

	_, ok := m.peers[addr]
	return ok
}

func (m *Manager) peerCount() int {
	m.peerMut.RLock()
	defer m.peerMut.RUnlock()

	return len(m.peers)
}

func (m *Manager) peerAdd(addr netip.AddrPort, peer *Peer) {
	m.peerMut.Lock()
	defer m.peerMut.Unlock()

	m.peers[addr] = peer
}

func (m *Manager) removePeer(addr netip.AddrPort) {
	m.peerMut.Lock()
	defer m.peerMut.Unlock()

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
			m.log.Debug(
				"purged inactive peers",
				slog.Int("deleted", count),
			)
		}
	}
}

func (m *Manager) purgeInactivePeers() int {
	count := 0

	m.peerMut.Lock()
	defer m.peerMut.Unlock()

	for addr, peer := range m.peers {
		if time.Since(peer.lastActiveAt) < m.cfg.KeepAliveInterval {
			continue
		}
		delete(m.peers, addr)
		count++
	}

	return count
}

func (m *Manager) cleanup() {
	peers := make([]*Peer, 0, len(m.peers))

	m.peerMut.Lock()
	for _, peer := range m.peers {
		peers = append(peers, peer)
	}
	m.peerMut.Unlock()

	var wg sync.WaitGroup
	for _, peer := range peers {
		wg.Add(1)
		go func(p *Peer) {
			defer wg.Done()
			if err := p.cleanup(); err != nil {
				m.log.Warn(
					"failed to close peer",
					slog.String("addr", p.addr.String()),
					slog.String("error", err.Error()),
				)
			}
		}(peer)
	}
	wg.Wait()
}

func (m *Manager) updateTotalDownloaded(size int) {
	m.statsMut.Lock()
	defer m.statsMut.Unlock()

	m.totalDownloaded += int64(size)
}

func (m *Manager) updateTotalUploaded(size int) {
	m.statsMut.Lock()
	defer m.statsMut.Unlock()

	m.totalUploaded += int64(size)
}

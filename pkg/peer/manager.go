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
)

type Config struct {
	MaxPeers                   int
	MaxInflightRequestsPerPeer int
	MaxRequestsPerPiece        int
	ReadTimeout                time.Duration
	WriteTimeout               time.Duration
	DialTimeout                time.Duration
	KeepAliveInterval          time.Duration
}

func withDefaultConfig() Config {
	return Config{
		MaxPeers:                   50,
		MaxInflightRequestsPerPeer: 5,
		MaxRequestsPerPiece:        4,
		ReadTimeout:                45 * time.Second,
		WriteTimeout:               45 * time.Second,
		DialTimeout:                30 * time.Second,
		KeepAliveInterval:          2 * time.Minute,
	}
}

type Manager struct {
	cfg         Config
	log         *slog.Logger
	wg          sync.WaitGroup
	dialSem     chan struct{}
	peerMut     sync.RWMutex
	peers       map[netip.AddrPort]*Peer
	peerCh      chan netip.AddrPort
	pieceCount  int
	clientID    [sha1.Size]byte
	pieceLength int64
	infoHash    [sha1.Size]byte
	picker      *piece.Picker
	storage     *storage.Disk
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

func (m *Manager) Start(ctx context.Context) error {
	m.wg.Go(func() { m.processPeersLoop(ctx) })

	m.log.Info("started peer manager")

	return nil
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

			if m.peerExists(addr) ||
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

				if m.peerExists(addr) ||
					m.peerCount() >= m.cfg.MaxPeers {
					_ = peer.Stop()
					return
				}

				m.peerAdd(addr, peer)
				peer.Start(ctx)
			}(addr)
		}
	}
}

func (m *Manager) peerExists(addr netip.AddrPort) bool {
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

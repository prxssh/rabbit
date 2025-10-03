package peer

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"log/slog"
	"net/netip"
	"sync"
	"time"
)

type Config struct {
	MaxPeers          int
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	DialTimeout       time.Duration
	KeepAliveInterval time.Duration
}

func withDefaultConfig() Config {
	return Config{
		MaxPeers:          50,
		ReadTimeout:       45 * time.Second,
		WriteTimeout:      45 * time.Second,
		DialTimeout:       30 * time.Second,
		KeepAliveInterval: 2 * time.Minute,
	}
}

type Manager struct {
	cfg Config
	log *slog.Logger
	wg  sync.WaitGroup

	peerMut sync.RWMutex
	peers   map[netip.AddrPort]*Peer
	peerCh  chan netip.AddrPort

	pieceCount int
	clientID   [sha1.Size]byte
	infoHash   [sha1.Size]byte
}

func NewManager(
	clientID, infoHash [sha1.Size]byte,
	pieceCount int,
	cfg *Config,
) *Manager {
	c := withDefaultConfig()
	if cfg != nil {
		c = *cfg
	}

	log := slog.Default().With(
		"source", "peer_manager",
		"info_hash", hex.EncodeToString(infoHash[:]),
		"client_id", hex.EncodeToString(clientID[:]),
	)

	return &Manager{
		cfg:        c,
		log:        log,
		clientID:   clientID,
		infoHash:   infoHash,
		pieceCount: pieceCount,
		peers:      make(map[netip.AddrPort]*Peer),
		peerCh:     make(chan netip.AddrPort, c.MaxPeers),
	}
}

func (m *Manager) Start(ctx context.Context) error {
	m.wg.Add(1)
	m.wg.Go(func() { m.processPeersLoop(ctx) })

	return nil
}

func (m *Manager) AdmitPeers(peers []netip.AddrPort) {
	for _, addr := range peers {
		select {
		case m.peerCh <- addr:
		default:
			m.log.Warn(
				"queue full dropping",
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
				m.log.Info("queue closed")
				return nil
			}

			if m.peerExists(addr) {
				continue
			}

			peer, err := dialPeer(ctx, m, addr)
			if err != nil {
				continue
			}
			m.peerAdd(addr, peer)
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

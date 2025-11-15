package dht

import (
	"crypto/sha1"
	"encoding/binary"
	"net"
	"sync"
	"time"
)

const (
	MaxPeersPerTorrent = 2000
	MaxTorrents        = 10000
	PeerExpiration     = 2 * time.Hour
)

type Storage struct {
	data map[[sha1.Size]byte]*torrentPeers
	mu   sync.RWMutex
}

type torrentPeers struct {
	peers    map[string]*peerEntry
	lastUsed time.Time
}

type peerEntry struct {
	info     [6]byte // Compact peer info (4 byte IP + 2 byte port)
	lastSeen time.Time
}

func NewStorage() *Storage {
	s := &Storage{
		data: make(map[[sha1.Size]byte]*torrentPeers),
	}

	go s.cleanupLoop()

	return s
}

func (s *Storage) StorePeer(infoHash [sha1.Size]byte, peerInfo [6]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tp, exists := s.data[infoHash]
	if !exists {
		if len(s.data) >= MaxTorrents {
			s.evictOldestTorrent()
		}

		tp = &torrentPeers{
			peers:    make(map[string]*peerEntry),
			lastUsed: time.Now(),
		}
		s.data[infoHash] = tp
	}

	tp.lastUsed = time.Now()

	key := string(peerInfo[:])
	if len(tp.peers) >= MaxPeersPerTorrent {
		if _, exists := tp.peers[key]; !exists {
			return
		}
	}

	tp.peers[key] = &peerEntry{
		info:     peerInfo,
		lastSeen: time.Now(),
	}
}

func (s *Storage) GetPeers(infoHash [sha1.Size]byte) [][6]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tp, exists := s.data[infoHash]
	if !exists {
		return nil
	}

	tp.lastUsed = time.Now()

	peers := make([][6]byte, 0, len(tp.peers))
	for _, entry := range tp.peers {
		peers = append(peers, entry.info)
	}

	return peers
}

func (s *Storage) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.cleanup()
	}
}

func (s *Storage) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	for infoHash, tp := range s.data {
		for key, entry := range tp.peers {
			if now.Sub(entry.lastSeen) > PeerExpiration {
				delete(tp.peers, key)
			}
		}

		if len(tp.peers) == 0 {
			delete(s.data, infoHash)
		}
	}
}

func (s *Storage) evictOldestTorrent() {
	var oldestHash [sha1.Size]byte
	var oldestTime time.Time
	first := true

	for hash, tp := range s.data {
		if first || tp.lastUsed.Before(oldestTime) {
			oldestHash = hash
			oldestTime = tp.lastUsed
			first = false
		}
	}

	delete(s.data, oldestHash)
}

func EncodePeerInfo(ip net.IP, port uint16) [6]byte {
	var info [6]byte
	ip4 := ip.To4()
	if ip4 == nil {
		return info
	}

	copy(info[:4], ip4)
	binary.BigEndian.PutUint16(info[4:6], port)
	return info
}

func DecodePeerInfo(info [6]byte) (net.IP, uint16) {
	ip := net.IPv4(info[0], info[1], info[2], info[3])
	port := binary.BigEndian.Uint16(info[4:6])
	return ip, port
}

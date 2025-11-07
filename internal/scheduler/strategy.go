package scheduler

import (
	"net/netip"
)

type DownloadStrategy uint8

const (
	DownloadStrategyRandom DownloadStrategy = iota
	DownloadStrategyRarestFirst
	DownloadStrategySequential
)

func (s *Scheduler) nextForPeer(addr netip.AddrPort) {
	s.peerMut.RLock()
	peer, ok := s.peers[addr]
	s.peerMut.RUnlock()

	if !ok {
		return
	}
}

package scheduler

import (
	"math/rand/v2"
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
	if !ok {
		s.peerMut.RUnlock()
		return
	}
	s.peerMut.RUnlock()

	if peer.maxInflightRequests < 1 {
		return
	}
	if s.endgameStarted {
		s.selectEndgameBlocks(peer, peer.maxInflightRequests)
		return
	}

	assignedBlocks, remCapacity := s.pieceManager.AssignInProgressBlocks(
		addr,
		peer.pieces,
		peer.maxInflightRequests,
	)
	for _, block := range assignedBlocks {
		s.assignBlockToPeer(peer, block)
	}

	var pieceSelectionStrategy func(*peerState, uint32)

	switch s.cfg.DownloadStrategy {
	case DownloadStrategySequential:
		pieceSelectionStrategy = s.selectSequentialBlocks
	case DownloadStrategyRandom:
		pieceSelectionStrategy = s.selectRandomBlocks
	default:
		pieceSelectionStrategy = s.selectRarestFirstBlocks
	}

	pieceSelectionStrategy(peer, remCapacity)
}

func (s *Scheduler) selectEndgameBlocks(peer *peerState, n uint32) {
	assignedBlocks, _ := s.pieceManager.AssignEndgameBlocks(
		peer.addr,
		peer.pieces,
		n,
		uint32(s.cfg.EndgameDuplicatePerBlock),
	)
	for _, block := range assignedBlocks {
		s.assignBlockToPeer(peer, block)
	}
}

func (s *Scheduler) selectSequentialBlocks(peer *peerState, n uint32) {
	assignedBlocks, _ := s.pieceManager.AssignSequentialBlocks(peer.addr, peer.pieces, n)
	for _, block := range assignedBlocks {
		s.assignBlockToPeer(peer, block)
	}
}

func (s *Scheduler) selectRandomBlocks(peer *peerState, n uint32) {
	pieceCount := s.pieceManager.PieceCount()
	available := make([]uint32, 0, pieceCount)

	for i := uint32(0); i < pieceCount; i++ {
		if peer.pieces.Has(int(i)) {
			available = append(available, i)
		}
	}
	if len(available) == 0 {
		return
	}
	rand.Shuffle(len(available), func(i, j int) {
		available[i], available[j] = available[j], available[i]
	})

	assignedBlocks, _ := s.pieceManager.AssignBlocksFromList(peer.addr, available, n)
	for _, blockInfo := range assignedBlocks {
		s.assignBlockToPeer(peer, blockInfo)
	}
}

func (s *Scheduler) selectRarestFirstBlocks(peer *peerState, n uint32) {
	rarestAvail, ok := s.pieceAvailabilityBucket.FirstNonEmpty()
	if !ok {
		return
	}

	pieceIndices := make([]uint32, 0)

	for a := rarestAvail; a <= s.pieceAvailabilityBucket.MaxAvailability(); a++ {
		bucket := s.pieceAvailabilityBucket.Bucket(a)
		if len(bucket) == 0 {
			continue
		}

		rand.Shuffle(len(bucket), func(i, j int) {
			bucket[i], bucket[j] = bucket[j], bucket[i]
		})

		for _, pieceIdx := range bucket {
			if peer.pieces.Has(pieceIdx) &&
				!s.pieceManager.PieceComplete(uint32(pieceIdx)) {
				pieceIndices = append(pieceIndices, uint32(pieceIdx))
			}
		}
	}

	assignedBlocks, _ := s.pieceManager.AssignBlocksFromList(peer.addr, pieceIndices, n)
	for _, block := range assignedBlocks {
		s.assignBlockToPeer(peer, block)
	}
}

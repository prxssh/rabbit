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

func (s *PieceScheduler) nextForPeer(peer netip.AddrPort) {
	ps, ok := s.peerState[peer]
	if !ok {
		s.log.Warn("failed to get peer", "peer", peer)
		return
	}

	capacity := max(0, s.cfg.MaxInflightRequestsPerPeer-ps.inflight)
	if capacity == 0 {
		return
	}

	if s.endgame {
		s.selectEndgameBlocks(ps, capacity)
		return
	}

	for i := 0; i < s.pieceCount && capacity > 0; i++ {
		piece := s.pieces[i]
		if piece.verified || piece.doneBlocks == 0 || !ps.bitfield.Has(i) {
			continue
		}

		for j := 0; j < piece.blockCount; j++ {
			if piece.blocks[j].status != blockWant {
				continue
			}

			s.assignBlockToPeer(peer, i, j)
			capacity--

			if capacity == 0 {
				return
			}

			// Break inner block-loop and continue to the *next piece* This
			// spreads requests across multiple in-progress pieces.
			break
		}
	}

	if capacity == 0 {
		return
	}

	var pieceSelectionStrategy func(*peerState, int)

	switch s.cfg.DownloadStrategy {
	case DownloadStrategySequential:
		pieceSelectionStrategy = s.selectSequential
	case DownloadStrategyRandom:
		pieceSelectionStrategy = s.selectRandom
	default:
		pieceSelectionStrategy = s.selectRarestFirst
	}

	pieceSelectionStrategy(ps, capacity)
}

func (s *PieceScheduler) selectEndgameBlocks(peer *peerState, n int) {
	for i := 0; i < s.pieceCount && n > 0; i++ {
		piece := s.pieces[i]
		if piece.verified || !peer.bitfield.Has(piece.index) {
			continue
		}

		for j := 0; j < piece.blockCount && n > 0; j++ {
			if piece.blocks[j].status != blockWant {
				continue
			}

			s.assignBlockToPeer(peer, i, j)
			n--
		}
	}
}

func (s *PieceScheduler) selectSequential(peer *peerState, n int) {
	for s.nextPiece < s.pieceCount && s.pieces[s.nextPiece].verified {
		s.nextPiece++
		s.nextBlock = 0
	}

	if s.nextPiece >= s.pieceCount {
		return
	}

	if !peer.bitfield.Has(s.nextPiece) {
		return
	}

	piece := s.pieces[s.nextPiece]

	for bi := s.nextBlock; bi < p.blockCount && n > 0; bi++ {
		if piece.blocks[bi].status != blockWant {
			continue
		}

		s.assignBlockToPeer(peer, s.nextPiece, bi)
		n--
		s.nextBlock = bi + 1
	}

	if s.nextBlock >= piece.blockCount {
		s.nextPiece++
		s.nextBlock = 0
	}
}

func (s *PieceScheduler) selectRandom(peer *peerState, n int) {
	available := make([]int, 0, s.pieceCount)
	for i := 0; i < s.pieceCount; i++ {
		if s.isPieceNeeded(i) && peer.bitfield.Has(i) {
			available = append(available, i)
		}
	}

	if len(available) == 0 {
		return
	}

	rand.Shuffle(len(available), func(i, j int) {
		available[i], available[j] = available[j], available[i]
	})

	for _, pieceIdx := range available {
		if n <= 0 {
			break
		}

		piece := s.pieces[pieceIdx]
		if blockIdx, ok := s.findAvailableBlock(piece); ok {
			s.assignBlockToPeer(peer, pieceIdx, blockIdx)
			n--
		}
	}
}

func (s *PieceScheduler) selectRarestFirst(peer *peerState, n int) {
	rarestAvail, ok := s.availability.FirstNonEmpty()
	if !ok {
		return
	}

	for a := rarestAvail; a <= s.availability.maxAvail && n > 0; a++ {
		bucket := s.availability.Bucket(a)
		if len(bucket) == 0 {
			continue
		}

		rand.Shuffle(len(bucket), func(i, j int) {
			bucket[i], bucket[j] = bucket[j], bucket[i]
		})

		for _, pieceIdx := range bucket {
			if n <= 0 {
				break
			}

			if !s.isPieceNeeded(pieceIdx) || !peer.bitfield.Has(pieceIdx) {
				continue
			}

			piece := s.pieces[pieceIdx]
			if blockIdx, ok := s.findAvailableBlock(piece); ok {
				s.assignBlockToPeer(peer, pieceIdx, blockIdx)
				n--
			}
		}
	}
}

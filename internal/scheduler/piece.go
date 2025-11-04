package scheduler

import (
	"crypto/sha1"
	"net/netip"
	"time"
)

type PieceState int

const (
	PieceStateNotStarted PieceState = iota
	PieceStateInProgress
	PieceStateCompleted
)

func (s *PieceScheduler) PieceStates() []PieceState {
	s.mut.RLock()
	defer s.mut.RUnlock()

	states := make([]PieceState, s.pieceCount)
	for i, p := range s.pieces {
		if p.verified {
			states[i] = PieceStateCompleted
		} else if p.doneBlocks > 0 {
			states[i] = PieceStateInProgress
		} else {
			states[i] = PieceStateNotStarted
		}
	}

	return states
}

type blockStatus uint8

const (
	blockWant blockStatus = iota
	blockInflight
	blockDone
)

type blockOwner struct {
	peer        netip.AddrPort
	requestedAt time.Time
}

type block struct {
	pendingRequests int
	status          blockStatus
	owner           *blockOwner
}

// piece describes one piece’s static metadata and dynamic progress.
type piece struct {
	// index is the zero-based piece index within the torrent.
	index int

	// length is the exact byte length of this piece. For all pieces except
	// the last, it will equal the torrent's piece length; the last may be
	// shorter.
	length int32

	// blockCount is the number of requestable blocks in this piece. All
	// blocks except the last are BlockSize long; see LastBlock.
	blockCount int

	// lastBlock is the byte size of the final block in this piece. If
	// Blocks==1, LastBlock == Length. Otherwise LastBlock == Length -
	// (Blocks-1)*BlockSize.
	lastBlock int32

	// isLastPiece is true for the last piece of the torrent (useful for
	// edge cases).
	isLastPiece bool

	// sha is the expected SHA-1 of the *piece* (20 bytes from the
	// metainfo).
	sha [sha1.Size]byte

	// doneBlocks is a fast counter of how many blocks have reached
	// BlockDone. When DoneBlocks == Blocks the piece is byte-complete and
	// ready to verify.
	doneBlocks int

	// verified is true once the piece has been hashed and matched SHA. A
	// verified piece should have all State==BlockDone and no Owners for any
	// block.
	verified bool

	// blocks holds all blocks in this piece, indexed by block offset.
	blocks []*block
}

type PieceInfo struct {
	Length int32
	IsLast bool
}

func (s *PieceScheduler) PieceInfo(piece int) PieceInfo {
	ps := s.pieces[piece]
	return PieceInfo{Length: ps.length, IsLast: ps.isLastPiece}
}

func (s *PieceScheduler) PieceHash(piece int) [sha1.Size]byte {
	s.mut.RLock()
	defer s.mut.RUnlock()

	return s.pieces[piece].sha
}

func (s *PieceScheduler) FirstUnverifiedPiece() (int, bool) {
	s.mut.RLock()
	defer s.mut.RUnlock()

	for i := 0; i < s.pieceCount; i++ {
		if !s.pieces[i].verified {
			return i, true
		}
	}

	return 0, false
}

func (s *PieceScheduler) markPieceVerified(piece int, ok bool) {
	s.mut.Lock()
	defer s.mut.Unlock()

	ps := s.pieces[piece]
	if ps.verified {
		return
	}

	if ok {
		ps.verified = true
		s.bitfield.Set(piece)

		if s.nextPiece == piece {
			s.nextPiece++
			s.nextBlock = 0
		}

		return
	}

	// Bad hash: revert piece to WANT
	for b := 0; b < ps.blockCount; b++ {
		if ps.blocks[b].status == blockDone {
			s.remainingBlocks++
		}

		ps.blocks[b].status = blockWant
		ps.blocks[b].owner = nil
	}

	ps.doneBlocks = 0
}

func blockKey(piece, begin int) uint64 {
	return uint64(piece)<<32 | uint64(begin)
}

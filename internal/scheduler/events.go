package scheduler

import (
	"net/netip"

	"github.com/prxssh/rabbit/pkg/bitfield"
)

type PeerEventType int

const (
	EventPeerUnchoked PeerEventType = iota
	EventPeerChoked
	EventPeerBitfield
	EventPeerHave
	EventPeerPiece
	EventPeerGone
)

// Event is a "marker interface" for all peer events. It allows different
// PeerEvent[T] instantiations to be sent on the same, strongly-typed channel.
type Event interface {
	// isEvent is an unexported "marker" method. This ensures only types in
	// this package can implement the Event interface.
	isEvent()
}

type PeerEvent[T any] struct {
	Peer netip.AddrPort
	Data T
}

// isEvent implements the Event interface for PeerEvent[T]. Because this method
// is defined on the generic type, all instantiations of PeerEvent[T] (like
// BitfieldEvent, HaveEvent, etc.) automatically satisfy the Event interface.
func (e PeerEvent[T]) isEvent() {}

type (
	UnchokedData  struct{}
	ChokedData    struct{}
	PeerGoneData  struct{}
	HandshakeData struct{}
)

type BitfieldData struct {
	Bitfield bitfield.Bitfield
}

type HaveData struct {
	Piece int
}

type PieceData struct {
	Piece int
	Begin int
	Data  []byte
}

type (
	HandshakeEvent = PeerEvent[HandshakeData]
	BitfieldEvent  = PeerEvent[BitfieldData]
	HaveEvent      = PeerEvent[HaveData]
	UnchokedEvent  = PeerEvent[UnchokedData]
	ChokedEvent    = PeerEvent[ChokedData]
	PieceEvent     = PeerEvent[PieceData]
	GoneEvent      = PeerEvent[PeerGoneData]
)

func (s *PieceScheduler) handleEvent(event Event) {
	switch e := event.(type) {
	case BitfieldEvent:
		s.onPeerBitfield(e.Peer, e.Data.Bitfield)
	case HaveEvent:
		s.onPeerHave(e.Peer, e.Data.Piece)
	case UnchokedEvent:
		s.onPeerUnchoke(e.Peer)
	case ChokedEvent:
		s.onPeerChoke(e.Peer)
	case PieceEvent:
		s.onPiece(e.Peer, e.Data)
	case GoneEvent:
		s.onPeerGone(e.Peer)
	default:
		s.log.Warn("unknown event type", "event", e)
	}
}

func (s *PieceScheduler) onPeerBitfield(peer netip.AddrPort, bf bitfield.Bitfield) {
	ok := func() bool {
		s.peerStateMut.Lock()
		defer s.peerStateMut.Unlock()

		if ps, ok := s.peerState[peer]; ok {
			ps.bitfield = bf
			return ok
		}

		return false
	}()

	if !ok {
		s.log.Warn("onPeerBitfield: peer state not initialized", "peer", peer)
		return
	}

	s.updatePieceAvailability(bf, 1)
}

func (s *PieceScheduler) onPeerHave(peer netip.AddrPort, piece int) {
	if piece < 0 || piece >= s.pieceCount {
		return
	}

	s.peerStateMut.Lock()
	defer s.peerStateMut.Unlock()

	ps, ok := s.peerState[peer]
	if !ok {
		s.log.Warn("onPeerHave: peer state not initialized", "peer", peer)
		return
	}

	if ps.bitfield.Has(piece) {
		return
	}

	ps.bitfield.Set(piece)
	s.updatePieceAvailability(ps.bitfield, 1)
}

func (s *PieceScheduler) onPeerChoke(peer netip.AddrPort) {
	s.peerStateMut.Lock()
	defer s.peerStateMut.Unlock()

	ps, ok := s.peerState[peer]
	if !ok {
		s.log.Warn("peer state not initialized", "peer", peer)
		return
	}

	ps.choked = true
}

func (s *PieceScheduler) onPeerUnchoke(peer netip.AddrPort) {
	s.peerStateMut.Lock()
	defer s.peerStateMut.Unlock()

	ps, ok := s.peerState[peer]
	if !ok {
		s.log.Warn("peer state not initialized", "peer", peer)
		return
	}

	ps.choked = false
}

func (s *PieceScheduler) onPiece(peer netip.AddrPort, p PieceData) {
}

func (s *PieceScheduler) onPeerGone(peer netip.AddrPort) {
	var (
		keys   []uint64
		peerBF bitfield.Bitfield
	)

	func() {
		s.peerStateMut.Lock()
		defer s.peerStateMut.Unlock()

		ps, ok := s.peerState[peer]
		if !ok {
			return
		}

		peerBF = ps.bitfield
		keys = make([]uint64, 0, len(ps.blockAssignments))

		for k := range ps.blockAssignments {
			keys = append(keys, k)
		}

		delete(s.peerState, peer)
	}()

	for _, key := range keys {
		pieceIdx := int(key >> 32)
		begin := int(key & 0xFFFFFFFF)

		piece := s.pieces[pieceIdx]
		blockIdx := BlockIndexForBegin(begin, int(piece.length))
		s.resetBlockToWant(pieceIdx, blockIdx)
		s.mut.Unlock()
	}

	s.updatePieceAvailability(peerBF, -1)
}

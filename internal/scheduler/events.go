package scheduler

import (
	"net/netip"

	"github.com/prxssh/rabbit/pkg/bitfield"
	"github.com/prxssh/rabbit/pkg/pieceutil"
)

type peerEventType int

const (
	EventPeerUnchoked peerEventType = iota
	EventPeerChoked
	EventPeerBitfield
	EventPeerHave
	EventPeerPiece
	EventPeerGone
)

type Event interface {
	isEvent()
}

type PeerEvent[T any] struct {
	Peer netip.AddrPort
	Data T
}

func (e PeerEvent[T]) isEvent() {}

type (
	HandshakeEvent = PeerEvent[HandshakeData]
	BitfieldEvent  = PeerEvent[BitfieldData]
	HaveEvent      = PeerEvent[HaveData]
	UnchokedEvent  = PeerEvent[UnchokedData]
	ChokedEvent    = PeerEvent[ChokedData]
	PieceEvent     = PeerEvent[PieceData]
	PeerGoneEvent  = PeerEvent[PeerGoneData]
)

type (
	UnchokedData  struct{}
	ChokedData    struct{}
	PeerGoneData  struct{}
	HandshakeData struct{}
)

func NewChokedEvent(addr netip.AddrPort) ChokedEvent {
	return PeerEvent[ChokedData]{
		Peer: addr,
		Data: ChokedData{},
	}
}

func NewUnchokedEvent(addr netip.AddrPort) UnchokedEvent {
	return PeerEvent[UnchokedData]{
		Peer: addr,
		Data: UnchokedData{},
	}
}

func NewPeerGoneEvent(addr netip.AddrPort) PeerGoneEvent {
	return PeerEvent[PeerGoneData]{
		Peer: addr,
		Data: PeerGoneData{},
	}
}

func NewHandshakeEVent(addr netip.AddrPort) HandshakeEvent {
	return PeerEvent[HandshakeData]{
		Peer: addr,
		Data: HandshakeData{},
	}
}

type BitfieldData struct {
	bf bitfield.Bitfield
}

func NewBitfieldEvent(addr netip.AddrPort, bf bitfield.Bitfield) BitfieldEvent {
	return PeerEvent[BitfieldData]{
		Peer: addr,
		Data: BitfieldData{bf: bf},
	}
}

type HaveData struct {
	Piece int
}

func NewHaveEvent(addr netip.AddrPort, piece uint32) HaveEvent {
	return PeerEvent[HaveData]{
		Peer: addr,
		Data: HaveData{Piece: int(piece)},
	}
}

type PieceData struct {
	Piece int
	Begin int
	Data  []byte
}

func NewPieceEvent(addr netip.AddrPort, piece, begin uint32, data []byte) PieceEvent {
	return PeerEvent[PieceData]{
		Peer: addr,
		Data: PieceData{Piece: int(piece), Begin: int(begin), Data: data},
	}
}

func (s *PieceScheduler) handleEvent(event Event) {
	switch e := event.(type) {
	case HandshakeEvent:
		s.onPeerHandshake(e.Peer)
	case BitfieldEvent:
		s.onPeerBitfield(e.Peer, e.Data.bf)
	case HaveEvent:
		s.onPeerHave(e.Peer, e.Data.Piece)
	case UnchokedEvent:
		s.onPeerUnchoke(e.Peer)
	case ChokedEvent:
		s.onPeerChoke(e.Peer)
	case PieceEvent:
		s.onPiece(e.Peer, e.Data)
	case PeerGoneEvent:
		s.onPeerGone(e.Peer)
	default:
		s.log.Warn("unknown event type", "event", e)
	}
}

func (s *PieceScheduler) onPeerHandshake(peer netip.AddrPort) {
	s.peerStateMut.Lock()
	ps, ok := s.peerState[peer]
	s.peerStateMut.Unlock()

	if !ok {
		s.log.Warn("peer not found", "peer", peer)
		return
	}

	ps.workQueue <- &WorkItem{Type: WorkSendBitfield, Bitfield: s.bitfield}
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

type BlockData struct {
	PieceIdx int
	Begin    int
	PieceLen int
	Data     []byte
}

func (s *PieceScheduler) onPiece(peer netip.AddrPort, p PieceData) {
	ok := func() bool {
		s.peerStateMut.Lock()
		defer s.peerStateMut.Unlock()

		ps, ok := s.peerState[peer]
		if !ok {
			s.log.Warn("peer not found", "peer", peer)
			return false
		}

		ps.inflight--
		key := blockKey(p.Piece, p.Begin)
		delete(ps.blockAssignments, key)

		return true
	}()

	if !ok {
		return
	}

	piece := s.pieces[p.Piece]

	pieceLen := piece.length
	if piece.isLastPiece {
		pieceLen = pieceutil.LastPieceLength(s.totalSize, pieceLen)
	}

	s.pieceQueue <- &BlockData{
		PieceIdx: piece.index,
		PieceLen: int(pieceLen),
		Begin:    p.Begin,
		Data:     p.Data,
	}
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
		blockIdx := pieceutil.BlockIndexForBegin(begin, int(piece.length))
		s.resetBlockToWant(pieceIdx, blockIdx)
	}

	s.updatePieceAvailability(peerBF, -1)
}

package scheduler

import (
	"net/netip"

	"github.com/prxssh/rabbit/internal/piece"
	"github.com/prxssh/rabbit/pkg/bitfield"
)

type Event interface {
	event()
}

type PeerEvent[T any] struct {
	Peer netip.AddrPort
	Data T
}

func (e PeerEvent[T]) event() {}

type (
	PeerHandshakeEvent = PeerEvent[HandshakeData]
	PeerBitfieldEvent  = PeerEvent[bitfield.Bitfield]
	PeerHaveEvent      = PeerEvent[HaveData]
	PeerUnchokedEvent  = PeerEvent[UnchokedData]
	PeerChokedEvent    = PeerEvent[ChokedData]
	PeerPieceEvent     = PeerEvent[PieceData]
	PeerRequestEvent   = PeerEvent[RequestPieceData]
	PeerCancelEvent    = PeerEvent[CancelData]
	PeerGoneEvent      = PeerEvent[GoneData]
	PeerSpeedEvent     = PeerEvent[PeerSpeedUpdate]
)

type (
	HandshakeData struct{}
	ChokedData    struct{}
	UnchokedData  struct{}
	GoneData      struct{}
)

func NewHandshakeEvent(addr netip.AddrPort) PeerHandshakeEvent {
	return PeerHandshakeEvent{Peer: addr}
}

func NewChokedEvent(addr netip.AddrPort) PeerChokedEvent {
	return PeerChokedEvent{Peer: addr}
}

func NewUnchokedEvent(addr netip.AddrPort) PeerUnchokedEvent {
	return PeerUnchokedEvent{Peer: addr}
}

func NewGoneEvent(addr netip.AddrPort) PeerGoneEvent {
	return PeerGoneEvent{Peer: addr}
}

func NewBitfieldEvent(addr netip.AddrPort, bf bitfield.Bitfield) PeerBitfieldEvent {
	return PeerBitfieldEvent{Peer: addr, Data: bf}
}

type HaveData struct {
	Piece uint32
}

func NewHaveEvent(addr netip.AddrPort, pieceIdx uint32) PeerHaveEvent {
	return PeerHaveEvent{Peer: addr, Data: HaveData{Piece: pieceIdx}}
}

type PieceData struct {
	PieceIdx uint32
	Begin    uint32
	Block    []byte
}

func NewPieceEvent(addr netip.AddrPort, pieceIdx, begin uint32, data []byte) PeerPieceEvent {
	return PeerPieceEvent{
		Peer: addr,
		Data: PieceData{
			PieceIdx: pieceIdx,
			Begin:    begin,
			Block:    data,
		},
	}
}

type RequestPieceData struct {
	PieceIdx uint32
	Begin    uint32
	Length   uint32
}

func NewRequestEvent(addr netip.AddrPort, pieceIdx, begin, length uint32) PeerRequestEvent {
	return PeerRequestEvent{
		Peer: addr,
		Data: RequestPieceData{
			PieceIdx: pieceIdx,
			Begin:    begin,
			Length:   length,
		},
	}
}

type CancelData struct {
	PieceIdx uint32
	Begin    uint32
	Length   uint32
}

func NewCancelEvent(addr netip.AddrPort, pieceIdx, begin, length uint32) PeerCancelEvent {
	return PeerCancelEvent{
		Peer: addr,
		Data: CancelData{
			PieceIdx: pieceIdx,
			Begin:    begin,
			Length:   length,
		},
	}
}

type PeerSpeedUpdate struct {
	DownloadBytesPerSec uint64
}

func NewPeerSpeedUpdateEvent(addr netip.AddrPort, downloadBytesPerSec uint64) PeerSpeedEvent {
	return PeerSpeedEvent{
		Peer: addr,
		Data: PeerSpeedUpdate{
			DownloadBytesPerSec: downloadBytesPerSec,
		},
	}
}

func (s *Scheduler) handlePeerEvent(event Event) {
	switch e := event.(type) {
	case PeerHandshakeEvent:
		s.handlePeerHandshakeEvent(e.Peer)
	case PeerChokedEvent:
		s.handlePeerChokedEvent(e.Peer)
	case PeerUnchokedEvent:
		s.handlePeerUnchokedEvent(e.Peer)
	case PeerGoneEvent:
		s.handlePeerGoneEvent(e.Peer)
	case PeerBitfieldEvent:
		s.handlePeerBitfieldEvent(e.Peer, e.Data)
	case PeerHaveEvent:
		s.handlePeerHaveEvent(e.Peer, e.Data)
	case PeerPieceEvent:
		s.handlePeerPieceEvent(e.Peer, e.Data)
	case PeerRequestEvent:
		s.handlePeerRequestEvent(e.Peer, e.Data)
	case PeerCancelEvent:
		s.handlePeerCancelEvent(e.Peer, e.Data)
	case PeerSpeedEvent:
		s.handlePeerSpeedEvent(e.Peer, e.Data)
	default:
		s.logger.Warn("unknown peer event", "event", e)
	}
}

func (s *Scheduler) handlePeerHandshakeEvent(addr netip.AddrPort) {
	s.peerMut.RLock()
	defer s.peerMut.RUnlock()

	peer, ok := s.peers[addr]
	if !ok {
		return
	}

	select {
	case peer.work <- NewBitfieldEvent(addr, s.downloadedPieces):

	default:
		s.logger.Warn(
			"peer work queue full; dropping message",
			"peer", addr,
			"message", "bitfield",
		)
	}
}

func (s *Scheduler) handlePeerChokedEvent(addr netip.AddrPort) {
	s.peerMut.Lock()
	defer s.peerMut.Unlock()

	peer, ok := s.peers[addr]
	if !ok {
		return
	}

	peer.choking = true
}

func (s *Scheduler) handlePeerUnchokedEvent(addr netip.AddrPort) {
	s.peerMut.Lock()
	peer, ok := s.peers[addr]
	if !ok {
		s.peerMut.Unlock()
		return
	}
	peer.choking = false
	s.peerMut.Unlock()

	s.nextForPeer(addr)
}

func (s *Scheduler) handlePeerBitfieldEvent(addr netip.AddrPort, data bitfield.Bitfield) {
	s.peerMut.Lock()
	defer s.peerMut.Unlock()

	peer, ok := s.peers[addr]
	if !ok {
		return
	}

	peer.pieces = data
	s.updateAvailability(data, 1)
}

func (s *Scheduler) handlePeerHaveEvent(addr netip.AddrPort, data HaveData) {
	s.peerMut.Lock()
	defer s.peerMut.Unlock()

	peer, ok := s.peers[addr]
	if !ok {
		return
	}

	pieceIdx := int(data.Piece)
	peer.pieces.Set(pieceIdx)
	s.updateAvailability(peer.pieces, 1)
}

func (s *Scheduler) handlePeerPieceEvent(addr netip.AddrPort, data PieceData) {
	key := blockKey(data.PieceIdx, data.Begin)

	s.peerMut.Lock()
	peer, ok := s.peers[addr]
	if !ok {
		s.peerMut.Unlock()
		return
	}
	delete(peer.blockAssignments, key)
	s.peerMut.Unlock()

	s.mut.Lock()
	s.inflightPieceRequests--
	s.mut.Unlock()

	s.pieceManager.MarkBlockComplete(addr, data.PieceIdx, data.Begin)

	s.outBlocks <- &BlockData{
		PieceIdx: data.PieceIdx,
		Begin:    data.Begin,
		Data:     data.Block,
		PieceLen: s.pieceManager.PieceLength(data.PieceIdx),
	}
}

// TODO
func (s *Scheduler) handlePeerRequestEvent(addr netip.AddrPort, data RequestPieceData) {
}

// TOOD
func (s *Scheduler) handlePeerCancelEvent(addr netip.AddrPort, data CancelData) {
}

func (s *Scheduler) handlePeerGoneEvent(addr netip.AddrPort) {
	s.peerMut.Lock()
	peer, ok := s.peers[addr]
	if !ok {
		s.peerMut.Unlock()
		return
	}
	delete(s.peers, addr)
	s.peerMut.Unlock()

	for key := range peer.blockAssignments {
		pieceIdx := uint32(key >> 32)
		begin := uint32(key & 0xFFFFFFFF)
		s.pieceManager.UnassignBlock(addr, pieceIdx, begin)
	}

	s.mut.Lock()
	s.inflightPieceRequests -= int32(len(peer.blockAssignments))
	s.mut.Unlock()

	s.updateAvailability(peer.pieces, -1)
}

func (s *Scheduler) handlePeerSpeedEvent(addr netip.AddrPort, data PeerSpeedUpdate) {
	s.peerMut.Lock()
	defer s.peerMut.Unlock()

	peer, ok := s.peers[addr]
	if !ok {
		return
	}

	blockPerSecond := data.DownloadBytesPerSec / piece.MaxBlockLength
	peer.maxInflightRequests = max(5, uint32(blockPerSecond))
}

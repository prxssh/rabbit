package piece

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

type PeerEvent[T any] struct {
	Peer netip.AddrPort
	Data T
}

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

func (pk *Picker) handleEvent(event any) {
	switch e := event.(type) {
	case BitfieldEvent:
		pk.OnPeerBitfield(e.Peer, e.Data.Bitfield)
	case HaveEvent:
		pk.OnPeerHave(e.Peer, e.Data.Piece)
	case UnchokedEvent:
		pk.OnPeerUnchoke(e.Peer)
	case ChokedEvent:
		pk.OnPeerChoke(e.Peer)
	case PieceEvent:
		pk.OnPiece(e.Peer, e.Data)
	case GoneEvent:
		pk.OnDisconnect(e.Peer)
	default:
		pk.log.Warn("unknown event type", "event", e)
	}
}

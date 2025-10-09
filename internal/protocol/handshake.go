package protocol

import (
	"crypto/sha1"
	"encoding"
	"errors"
	"io"
)

const (
	btProtocol = "BitTorrent protocol"
	reservedN  = 8
)

// Handshake represents the initial BitTorrent wire handshake.
//
// Wire format (in bytes):
//
//	<pstrlen><pstr><reserved:8><info_hash:20><peer_id:20>
//
// Example:
//
//	19 "BitTorrent protocol" <8 zero bytes> <info_hash> <peer_id>
//
// The handshake is always the first message sent upon connecting to a peer. It
// identifies the torrent being downloaded (via info_hash) and the local peer.
type Handshake struct {
	Pstr     string          // Protocol identifier, usually "BitTorrent protocol"
	Reserved [reservedN]byte // Reserved bytes used for feature flags (DHT, Fast, Extension, etc.)
	InfoHash [sha1.Size]byte // SHA1 hash of the torrent's "info" dictionary.
	PeerID   [sha1.Size]byte // Unique 20-byte peer identifier.
}

var (
	ErrProtocolMismatch = errors.New("handshake: protocol string mismatch")
	ErrBadPstrlen       = errors.New("handshake: invalid protocol string length")
	ErrShortHandshake   = errors.New("handshake: short read")
	ErrInfoHashMismatch = errors.New("handshake: info hash mismatch")
)

var (
	_ encoding.BinaryMarshaler   = (*Handshake)(nil)
	_ encoding.BinaryUnmarshaler = (*Handshake)(nil)
	_ io.WriterTo                = (*Handshake)(nil)
	_ io.ReaderFrom              = (*Handshake)(nil)
)

// NewHandshake returns a canonical BitTorrent handshake using the given
// torrent info hash and local peer ID.
//
// The returned handshake uses the standard protocol identifier "BitTorrent
// protocol" and zeroed reserved bytes.
func NewHandshake(infoHash, peerID [sha1.Size]byte) *Handshake {
	return &Handshake{
		Pstr:     btProtocol,
		InfoHash: infoHash,
		PeerID:   peerID,
	}
}

// MarshalBinary encodes the handshake into its wire representation.
//
// The result can be written directly to a network connection or buffer.
// Returns ErrBadPstrlen if Pstr is empty or longer than 255 bytes.
func (h *Handshake) MarshalBinary() ([]byte, error) {
	if len(h.Pstr) == 0 || len(h.Pstr) > 255 {
		return nil, ErrBadPstrlen
	}

	n := 1 + len(h.Pstr) + reservedN + sha1.Size + sha1.Size
	buf := make([]byte, n)

	buf[0] = byte(len(h.Pstr))
	offset := 1
	offset += copy(buf[offset:], []byte(h.Pstr))
	offset += copy(buf[offset:], make([]byte, reservedN))
	offset += copy(buf[offset:], h.InfoHash[:])
	offset += copy(buf[offset:], h.PeerID[:])

	return buf, nil
}

// UnmarshalBinary parses a handshake from its wire format.
//
// It validates the protocol string length and ensures enough bytes are present
// for reserved, info_hash, and peer_id fields.
func (h *Handshake) UnmarshalBinary(b []byte) error {
	if len(b) < 1 {
		return ErrShortHandshake
	}

	pstrlen := int(b[0])
	if pstrlen == 0 || pstrlen > 255 {
		return ErrBadPstrlen
	}
	const tail = reservedN + sha1.Size + sha1.Size
	if len(b) < 1+pstrlen+tail {
		return ErrShortHandshake
	}

	pstrStart := 1
	pstrEnd := pstrStart + pstrlen
	copy(h.Reserved[:], b[pstrEnd:pstrEnd+reservedN])
	copy(h.InfoHash[:], b[pstrEnd+reservedN:pstrEnd+reservedN+sha1.Size])
	copy(h.PeerID[:], b[pstrEnd+reservedN+sha1.Size:])

	h.Pstr = string(b[pstrStart:pstrEnd])
	return nil
}

// WriteTo implements io.WriterTo.
//
// It writes the binary representation of the handshake to w.
// It is equivalent to calling w.Write(h.MarshalBinary()).
func (h *Handshake) WriteTo(w io.Writer) (int64, error) {
	b, err := h.MarshalBinary()
	if err != nil {
		return 0, err
	}

	n, err := w.Write(b)
	return int64(n), err
}

// ReadFrom implements io.ReaderFrom.
//
// It reads and decodes a complete handshake from r.
// This method blocks until the full handshake is read or an error occurs.
func (h *Handshake) ReadFrom(r io.Reader) (int64, error) {
	var hdr [1]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, ErrShortHandshake
		}
		return 0, err
	}
	pstrlen := int(hdr[0])
	if pstrlen == 0 || pstrlen > 255 {
		return 1, ErrBadPstrlen
	}

	rest := make([]byte, pstrlen+reservedN+sha1.Size+sha1.Size)
	if _, err := io.ReadFull(r, rest); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return int64(1 + len(rest)), ErrShortHandshake
		}
		return int64(1 + len(rest)), err
	}

	if err := h.UnmarshalBinary(append(hdr[:], rest...)); err != nil {
		return int64(1 + len(rest)), err
	}
	return int64(1 + len(rest)), nil
}

// ReadHandshake reads a full handshake from r and returns it.
func ReadHandshake(r io.Reader) (Handshake, error) {
	var h Handshake
	_, err := h.ReadFrom(r)
	return h, err
}

// WriteHandshake writes h to w in wire format.
func WriteHandshake(w io.Writer, h Handshake) error {
	_, err := h.WriteTo(w)
	return err
}

// Exchange performs the outbound handshake exchange.
//
// It writes the local handshake to rw, reads the remote handshake, and
// (optionally) verifies that both sides share the same info hash.
//
// Returns the remote peer's handshake or an error if validation fails.
func (h Handshake) Exchange(rw io.ReadWriter, verifyInfoHash bool) (peer Handshake, err error) {
	if _, err = (&h).WriteTo(rw); err != nil {
		return Handshake{}, err
	}
	if _, err = (&peer).ReadFrom(rw); err != nil {
		return Handshake{}, err
	}

	if peer.Pstr != btProtocol {
		return Handshake{}, ErrProtocolMismatch
	}
	if verifyInfoHash && peer.InfoHash != h.InfoHash {
		return Handshake{}, ErrInfoHashMismatch
	}
	return peer, nil
}

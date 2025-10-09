package peer

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"io"
)

type Handshake struct {
	Pstr     string
	InfoHash [sha1.Size]byte
	PeerID   [sha1.Size]byte
}

const szReservedBytes = 8

func NewHandshake(infoHash, peerID [sha1.Size]byte) *Handshake {
	return &Handshake{
		Pstr:     "BitTorrent protocol",
		InfoHash: infoHash,
		PeerID:   peerID,
	}
}

func (h *Handshake) Serialize() []byte {
	buf := make([]byte, len(h.Pstr)+49)

	buf[0] = byte(len(h.Pstr))
	offset := 1
	offset += copy(buf[offset:], []byte(h.Pstr))
	offset += copy(buf[offset:], make([]byte, szReservedBytes))
	offset += copy(buf[offset:], h.InfoHash[:])
	offset += copy(buf[offset:], h.PeerID[:])

	return buf
}

func (h *Handshake) Perform(w io.ReadWriter) error {
	_, err := w.Write(h.Serialize())
	if err != nil {
		return err
	}
	res, err := readHanshake(w)
	if err != nil {
		return err
	}

	if !bytes.Equal(h.InfoHash[:], res.InfoHash[:]) {
		return errors.New("handshake: info hash mismatch")
	}
	return nil
}

func readHanshake(r io.Reader) (*Handshake, error) {
	sizeBuf := make([]byte, 1)
	_, err := io.ReadFull(r, sizeBuf)
	if err != nil {
		return nil, err
	}

	pstrlen := sizeBuf[0]
	if pstrlen == 0 {
		return nil, errors.New("pstrlen can't be 0")
	}
	handshakeBuf := make([]byte, 48+pstrlen)
	if _, err := io.ReadFull(r, handshakeBuf); err != nil {
		return nil, err
	}

	var infoHash, peerID [sha1.Size]byte
	copy(
		infoHash[:],
		handshakeBuf[pstrlen+szReservedBytes:pstrlen+szReservedBytes+sha1.Size],
	)
	copy(peerID[:], handshakeBuf[pstrlen+szReservedBytes+sha1.Size:])

	return &Handshake{
		Pstr:     string(handshakeBuf[0:pstrlen]),
		InfoHash: infoHash,
		PeerID:   peerID,
	}, nil
}

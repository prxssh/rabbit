package protocol

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"io"
	"strings"
	"testing"
)

func mustBytes20(s string) [sha1.Size]byte {
	var a [sha1.Size]byte
	copy(a[:], []byte(s))
	return a
}

func TestHandshake_MarshalUnmarshal_OK(t *testing.T) {
	info := mustBytes20("info_hash_1234567890")
	peer := mustBytes20("peer_id_1234567890_")

	h := NewHandshake(info, peer)

	b, err := h.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary error: %v", err)
	}

	// Validate layout: <pstrlen><pstr><reserved:8><info_hash:20><peer_id:20>
	if got, want := int(b[0]), len(btProtocol); got != want {
		t.Fatalf("pstrlen = %d, want %d", got, want)
	}
	if got, want := string(b[1:1+len(btProtocol)]), btProtocol; got != want {
		t.Fatalf("pstr = %q, want %q", got, want)
	}
	// Reserved should be zeroed by marshal.
	if r := b[1+len(btProtocol) : 1+len(btProtocol)+reservedN]; bytes.Count(
		r,
		[]byte{0},
	) != reservedN {
		t.Fatalf("reserved not zeroed: %v", r)
	}

	// InfoHash and PeerID should round-trip correctly on unmarshal.
	var got Handshake
	if err := (&got).UnmarshalBinary(b); err != nil {
		t.Fatalf("UnmarshalBinary error: %v", err)
	}
	if got.Pstr != btProtocol {
		t.Fatalf("Pstr = %q, want %q", got.Pstr, btProtocol)
	}
	if got.InfoHash != info {
		t.Fatalf("InfoHash mismatch: got %x, want %x", got.InfoHash, info)
	}
	if got.PeerID != peer {
		t.Fatalf("PeerID mismatch: got %x, want %x", got.PeerID, peer)
	}

	// Unmarshal should copy reserved bytes.
	var zeros [reservedN]byte
	if got.Reserved != zeros {
		t.Fatalf("Reserved not zero: %v", got.Reserved)
	}
}

func TestHandshake_MarshalBinary_BadPstrlen(t *testing.T) {
	info := mustBytes20("info_hash_1234567890")
	peer := mustBytes20("peer_id_1234567890_")

	// Empty protocol string is invalid.
	h := &Handshake{Pstr: "", InfoHash: info, PeerID: peer}
	if _, err := h.MarshalBinary(); !errors.Is(err, ErrBadPstrlen) {
		t.Fatalf("want ErrBadPstrlen, got %v", err)
	}

	// Too long protocol string (>255) is invalid.
	h.Pstr = strings.Repeat("x", 256)
	if _, err := h.MarshalBinary(); !errors.Is(err, ErrBadPstrlen) {
		t.Fatalf("want ErrBadPstrlen for long pstr, got %v", err)
	}
}

func TestHandshake_UnmarshalBinary_Short(t *testing.T) {
	var h Handshake
	if err := (&h).UnmarshalBinary(nil); !errors.Is(err, ErrShortHandshake) {
		t.Fatalf("want ErrShortHandshake, got %v", err)
	}

	// Declare a header but truncate the rest
	bad := []byte{19} // pstrlen=19, but no further bytes
	if err := (&h).UnmarshalBinary(bad); !errors.Is(err, ErrShortHandshake) {
		t.Fatalf("want ErrShortHandshake for truncated payload, got %v", err)
	}
}

func TestHandshake_ReadFrom_BadAndShort(t *testing.T) {
	var h Handshake

	// pstrlen = 0 -> ErrBadPstrlen
	r := bytes.NewReader([]byte{0})
	if n, err := (&h).ReadFrom(r); !errors.Is(err, ErrBadPstrlen) || n != 1 {
		t.Fatalf("want (1, ErrBadPstrlen), got (%d, %v)", n, err)
	}

	// pstrlen ok but truncated remainder -> ErrShortHandshake
	r = bytes.NewReader([]byte{1 /*pstrlen*/, 'A'})
	if _, err := (&h).ReadFrom(r); !errors.Is(err, ErrShortHandshake) {
		t.Fatalf("want ErrShortHandshake, got %v", err)
	}
}

func TestHandshake_ReadWrite_Wrappers(t *testing.T) {
	info := mustBytes20("info_hash_1234567890")
	peer := mustBytes20("peer_id_1234567890_")
	h := NewHandshake(info, peer)

	var buf bytes.Buffer
	if err := WriteHandshake(&buf, *h); err != nil {
		t.Fatalf("WriteHandshake error: %v", err)
	}

	rd := bytes.NewReader(buf.Bytes())
	got, err := ReadHandshake(rd)
	if err != nil {
		t.Fatalf("ReadHandshake error: %v", err)
	}

	if got.Pstr != btProtocol || got.InfoHash != info || got.PeerID != peer {
		t.Fatalf("handshake mismatch: got %+v", got)
	}
}

// rwPair allows reading from a fixed reader and capturing writes.
type rwPair struct {
	io.Reader
	io.Writer
}

func TestHandshake_Exchange_OK(t *testing.T) {
	info := mustBytes20("info_hash_1234567890")
	peer := mustBytes20("peer_id_peer_peer_id_")

	local := NewHandshake(info, mustBytes20("local_peer_id________"))

	// Prepare remote handshake bytes to be read by Exchange.
	remote := &Handshake{Pstr: btProtocol, InfoHash: info, PeerID: peer}
	rb, err := remote.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary remote: %v", err)
	}

	var written bytes.Buffer
	rw := &rwPair{Reader: bytes.NewReader(rb), Writer: &written}

	got, err := local.Exchange(rw, true)
	if err != nil {
		t.Fatalf("Exchange error: %v", err)
	}

	// Validate what we wrote equals our local handshake.
	lb, _ := local.MarshalBinary()
	if !bytes.Equal(written.Bytes(), lb) {
		t.Fatalf("written != local handshake")
	}

	if got.Pstr != btProtocol || got.InfoHash != info || got.PeerID != peer {
		t.Fatalf("peer mismatch: got %+v", got)
	}
}

func TestHandshake_Exchange_ProtocolMismatch(t *testing.T) {
	info := mustBytes20("info_hash_1234567890")
	local := NewHandshake(info, mustBytes20("local_peer_id________"))

	remote := &Handshake{
		Pstr:     "OtherProto",
		InfoHash: info,
		PeerID:   mustBytes20("peer_________________"),
	}
	rb, _ := remote.MarshalBinary()

	rw := &rwPair{Reader: bytes.NewReader(rb), Writer: &bytes.Buffer{}}

	if _, err := local.Exchange(rw, true); !errors.Is(err, ErrProtocolMismatch) {
		t.Fatalf("want ErrProtocolMismatch, got %v", err)
	}
}

func TestHandshake_Exchange_InfoHashMismatch(t *testing.T) {
	info1 := mustBytes20("info_hash_1234567890")
	info2 := mustBytes20("DIFFERENT_info_hash_____")
	local := NewHandshake(info1, mustBytes20("local_peer_id________"))

	remote := &Handshake{
		Pstr:     btProtocol,
		InfoHash: info2,
		PeerID:   mustBytes20("peer_________________"),
	}
	rb, _ := remote.MarshalBinary()

	rw := &rwPair{Reader: bytes.NewReader(rb), Writer: &bytes.Buffer{}}

	if _, err := local.Exchange(rw, true); !errors.Is(err, ErrInfoHashMismatch) {
		t.Fatalf("want ErrInfoHashMismatch, got %v", err)
	}
}

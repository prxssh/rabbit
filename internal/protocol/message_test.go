package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestMessage_KeepAlive_MarshalUnmarshal(t *testing.T) {
	var m *Message
	b, err := m.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary keep-alive error: %v", err)
	}

	if want := []byte{0, 0, 0, 0}; !bytes.Equal(b, want) {
		t.Fatalf("keep-alive encoded = %v, want %v", b, want)
	}

	var dec Message
	if err := (&dec).UnmarshalBinary(b); err != nil {
		t.Fatalf("UnmarshalBinary keep-alive: %v", err)
	}
	if dec.ID != 0 || dec.Payload != nil {
		t.Fatalf("decoded keep-alive unexpected: %+v", dec)
	}
}

func TestMessage_ConstructorsAndParsers(t *testing.T) {
	// Have
	m := MessageHave(42)
	if idx, ok := m.ParseHave(); !ok || idx != 42 {
		t.Fatalf("ParseHave = (%d,%v), want (42,true)", idx, ok)
	}
	if err := m.ValidatePayloadSize(); err != nil {
		t.Fatalf("ValidatePayloadSize(Have) err: %v", err)
	}

	// Request
	m = MessageRequest(7, 16, 16384)
	i, b, l, ok := m.ParseRequest()
	if !ok || i != 7 || b != 16 || l != 16384 {
		t.Fatalf("ParseRequest got (%d,%d,%d,%v)", i, b, l, ok)
	}
	if err := m.ValidatePayloadSize(); err != nil {
		t.Fatalf("ValidatePayloadSize(Request) err: %v", err)
	}

	// Piece
	block := []byte("data block")
	m = MessagePiece(3, 32, block)
	pi, pb, blk, ok := m.ParsePiece()
	if !ok || pi != 3 || pb != 32 || !bytes.Equal(blk, block) {
		t.Fatalf("ParsePiece mismatch")
	}
	if err := m.ValidatePayloadSize(); err != nil {
		t.Fatalf("ValidatePayloadSize(Piece) err: %v", err)
	}

	// Bitfield copies input
	bits := []byte{0xAA, 0x55}
	m = MessageBitfield(bits)
	bits[0] ^= 0xFF // mutate original
	if len(m.Payload) != 2 || m.Payload[0] != 0xAA || m.Payload[1] != 0x55 {
		t.Fatalf("MessageBitfield did not copy input: %v", m.Payload)
	}
}

func TestMessage_ValidatePayloadSize_Errors(t *testing.T) {
	tests := []Message{
		{ID: Have, Payload: []byte{}},
		{ID: Request, Payload: []byte("too short")},       // 10 bytes
		{ID: Cancel, Payload: []byte{1, 2, 3}},            // 3 bytes
		{ID: Piece, Payload: []byte{0, 1, 2, 3, 4, 5, 6}}, // 7 bytes
	}
	for _, m := range tests {
		if err := (&m).ValidatePayloadSize(); !errors.Is(err, ErrBadPayloadSize) {
			t.Fatalf("want ErrBadPayloadSize for %+v, got %v", m, err)
		}
	}
}

func TestMessage_MarshalUnmarshal_Normal(t *testing.T) {
	m := MessageRequest(1, 2, 3)
	b, err := m.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary error: %v", err)
	}
	if got, want := binary.BigEndian.Uint32(b[0:4]), uint32(13); got != want { // 1 byte id + 12 payload
		t.Fatalf("length prefix = %d, want %d", got, want)
	}
	if got := b[4]; got != byte(Request) {
		t.Fatalf("id = %d, want %d", got, Request)
	}

	var dec Message
	if err := (&dec).UnmarshalBinary(b); err != nil {
		t.Fatalf("UnmarshalBinary error: %v", err)
	}
	if dec.ID != Request || !bytes.Equal(dec.Payload, m.Payload) {
		t.Fatalf("decoded mismatch: %+v vs %+v", dec, m)
	}
}

func TestMessage_WriteRead_RoundTrip(t *testing.T) {
	src := MessagePiece(9, 1024, []byte("hello"))

	var buf bytes.Buffer
	if _, err := src.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}

	var dst Message
	if _, err := (&dst).ReadFrom(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}

	if dst.ID != src.ID || !bytes.Equal(dst.Payload, src.Payload) {
		t.Fatalf("round-trip mismatch: %+v vs %+v", dst, src)
	}
}

func TestReadMessage_KeepAliveNormalization(t *testing.T) {
	// 4 zero bytes represent a keep-alive
	r := bytes.NewReader([]byte{0, 0, 0, 0})
	m, err := ReadMessage(r)
	if err != nil {
		t.Fatalf("ReadMessage error: %v", err)
	}
	if m != nil {
		t.Fatalf("want nil for keep-alive, got %+v", m)
	}
}

func TestMessage_ReadFrom_Errors(t *testing.T) {
	// length < 1 but non-zero is invalid; construct prefix=0 then handled as keep-alive
	// Test truncated payload path instead.
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 5) // id(1)+payload(4) but we'll truncate

	r := bytes.NewReader(
		append(hdr[:], []byte{byte(Have), 0x00, 0x00}...),
	) // only 3 of 4 payload bytes
	var m Message
	if _, err := (&m).ReadFrom(r); err == nil {
		t.Fatalf("expected error for truncated message, got nil")
	}
}

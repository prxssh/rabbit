package protocol

import (
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

type MessageID uint8

const (
	MsgChoke         MessageID = 0
	MsgUnchoke       MessageID = 1
	MsgInterested    MessageID = 2
	MsgNotInterested MessageID = 3
	MsgHave          MessageID = 4
	MsgBitfield      MessageID = 5
	MsgRequest       MessageID = 6
	MsgPiece         MessageID = 7
	MsgCancel        MessageID = 8
)

func (mid MessageID) String() string {
	switch mid {
	case MsgChoke:
		return "Choke"
	case MsgUnchoke:
		return "Unchoke"
	case MsgInterested:
		return "Interested"
	case MsgNotInterested:
		return "Not Interested"
	case MsgHave:
		return "Have"
	case MsgBitfield:
		return "Bitfield"
	case MsgRequest:
		return "Request"
	case MsgPiece:
		return "Piece"
	case MsgCancel:
		return "Cancel"
	default:
		return fmt.Sprintf("Unknown(%d)", mid)
	}
}

// Message represents a single BitTorrent length-prefixed message.
//
// Wire format:
//
//	keep-alive: <length=0>
//	otherwise: <length:4><id:1><payload:length-1>
//
// A nil *Message denotes a keep-alive frame.
// For non-nil messages, Payload may be empty for messages that carry no data.
type Message struct {
	ID      MessageID
	Payload []byte
}

var (
	ErrShortMessage    = errors.New("protocol: short message")
	ErrBadLengthPrefix = errors.New("protocol: invalid length prefix")
	ErrBadPayloadSize  = errors.New("protocol: invalid payload size for message")
)

var (
	_ encoding.BinaryMarshaler   = (*Message)(nil)
	_ encoding.BinaryUnmarshaler = (*Message)(nil)
	_ io.WriterTo                = (*Message)(nil)
	_ io.ReaderFrom              = (*Message)(nil)
)

// IsKeepAlive reports whether m denotes a keep-alive frame.
// By convention, a nil *Message is a keep-alive.
func IsKeepAlive(m *Message) bool { return m == nil }

func MessageChoke() *Message         { return &Message{ID: MsgChoke} }
func MessageUnchoke() *Message       { return &Message{ID: MsgUnchoke} }
func MessageInterested() *Message    { return &Message{ID: MsgInterested} }
func MessageNotInterested() *Message { return &Message{ID: MsgNotInterested} }

func MessageHave(index int) *Message {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(index))

	return &Message{ID: MsgHave, Payload: payload}
}

func MessageBitfield(bits []byte) *Message {
	cp := make([]byte, len(bits))
	copy(cp, bits)

	return &Message{ID: MsgBitfield, Payload: cp}
}

func MessageRequest(index, begin, length int) *Message {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	binary.BigEndian.PutUint32(payload[8:12], uint32(length))

	return &Message{ID: MsgRequest, Payload: payload}
}

func MessagePiece(index, begin int, block []byte) *Message {
	payload := make([]byte, 8+len(block))
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	copy(payload[8:], block)

	return &Message{ID: MsgPiece, Payload: payload}
}

func MessageCancel(index, begin, length int) *Message {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	binary.BigEndian.PutUint32(payload[8:12], uint32(length))

	return &Message{ID: MsgCancel, Payload: payload}
}

// ParseHave returns the piece index for a Have message.
// ok is false if the payload length is not exactly 4 bytes.
func (m *Message) ParseHave() (index uint32, ok bool) {
	if m == nil || m.ID != MsgHave || len(m.Payload) != 4 {
		return 0, false
	}

	return binary.BigEndian.Uint32(m.Payload), true
}

// ParseRequest parses a Request payload into index, begin, and length.
// ok is false if the payload length is not exactly 12 bytes.
func (m *Message) ParseRequest() (idx, begin, length uint32, ok bool) {
	if m == nil || m.ID != MsgRequest || len(m.Payload) != 12 {
		return 0, 0, 0, false
	}

	return binary.BigEndian.Uint32(m.Payload[0:4]),
		binary.BigEndian.Uint32(m.Payload[4:8]),
		binary.BigEndian.Uint32(m.Payload[8:12]),
		true
}

// ParsePiece parses a Piece payload into index, begin, and the data block.
// ok is false if there are fewer than 8 bytes of header.
func (m *Message) ParsePiece() (idx, begin uint32, block []byte, ok bool) {
	if m == nil || m.ID != MsgPiece || len(m.Payload) < 8 {
		return 0, 0, nil, false
	}

	return binary.BigEndian.Uint32(m.Payload[0:4]),
		binary.BigEndian.Uint32(m.Payload[4:8]),
		m.Payload[8:], true
}

func (m *Message) MarshalBinary() ([]byte, error) {
	if m == nil {
		return []byte{0, 0, 0, 0}, nil
	}

	// length prefix excludes itself; includes id + payload.
	length := 1 + len(m.Payload)
	if length < 1 || length > int(^uint32(0)) {
		return nil, ErrBadLengthPrefix
	}

	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf[0:4], uint32(length))
	buf[4] = byte(m.ID)
	copy(buf[5:], m.Payload)

	return buf, nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler.
//
// Accepts both keep-alive (length=0) and normal frames.
func (m *Message) UnmarshalBinary(b []byte) error {
	if len(b) < 4 {
		return ErrShortMessage
	}

	length := binary.BigEndian.Uint32(b[0:4])
	if length == 0 {
		*m = Message{}
		return nil
	}
	if len(b) < 4+int(length) {
		return ErrShortMessage
	}

	id := b[4]
	payload := b[5 : 4+int(length)]
	m.ID = MessageID(id)
	m.Payload = append(m.Payload[:0], payload...)

	return nil
}

// WriteTo implements io.WriterTo.
//
// For keep-alive (m==nil), it writes 4 zero bytes.
// For normal messages, it writes the 4-byte length prefix, id, and payload.
func (m *Message) WriteTo(w io.Writer) (int64, error) {
	if m == nil {
		var z [4]byte
		n, err := w.Write(z[:])
		return int64(n), err
	}

	var hdr [5]byte

	length := 1 + len(m.Payload)
	binary.BigEndian.PutUint32(hdr[0:4], uint32(length))
	hdr[4] = byte(m.ID)

	n1, err := w.Write(hdr[:])
	if err != nil {
		return int64(n1), err
	}
	if len(m.Payload) == 0 {
		return int64(n1), nil
	}

	n2, err := w.Write(m.Payload)
	return int64(n1 + n2), err
}

// ReadFrom implements io.ReaderFrom.
//
// It reads a full message frame from r. For keep-alive (length=0),
// the receiver is zeroed (ID=0, Payload=nil) and the caller can use IsKeepAlive(nil)
// convention by checking the return of ReadMessage wrapper.
func (m *Message) ReadFrom(r io.Reader) (int64, error) {
	var lp [4]byte
	if _, err := io.ReadFull(r, lp[:]); err != nil {
		return 0, err
	}

	length := binary.BigEndian.Uint32(lp[:])
	if length == 0 {
		*m = Message{} // keep-alive frame
		return 4, nil
	}
	if length < 1 {
		return 4, ErrBadLengthPrefix
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return int64(4 + len(buf)), err
	}
	m.ID = MessageID(buf[0])
	m.Payload = append(m.Payload[:0], buf[1:]...)

	return int64(4 + len(buf)), nil
}

func ReadMessage(r io.Reader) (*Message, error) {
	var m Message
	if _, err := m.ReadFrom(r); err != nil {
		return nil, err
	}

	// Normalize keep-alive to nil.
	if m.Payload == nil && m.ID == 0 {
		return nil, nil
	}

	return &m, nil
}

// WriteMessage writes m to w.
// If m is nil, it writes a keep-alive frame.
func WriteMessage(w io.Writer, m *Message) error {
	_, err := m.WriteTo(w)
	return err
}

func (m *Message) ValidatePayloadSize() error {
	if m == nil {
		return nil // keep-alive
	}

	switch m.ID {
	case MsgHave:
		if len(m.Payload) != 4 {
			return ErrBadPayloadSize
		}
	case MsgRequest, MsgCancel:
		if len(m.Payload) != 12 {
			return ErrBadPayloadSize
		}
	case MsgPiece:
		if len(m.Payload) < 8 {
			return ErrBadPayloadSize
		}
	}
	return nil
}

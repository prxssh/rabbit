package bencode

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
)

// Marshal returns the bencoded form of v.
//
// See Encoder.Encode for the supported Go types. Marshal returns an error if
// v's type is not supported.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	e := NewEncoder(&buf)

	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Encoder writes bencoded values to an io.Writer.
//
// The zero value of Encoder is not usable; construct with NewEncoder.
type Encoder struct {
	w io.Writer
}

// NewEncoder returns a new Encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode writes the bencoded representation of v to the underlying writer.
//
// Supported value types:
//
//	string, []byte, bool, int/int8/int16/int32/int64,
//	uint/uint8/uint16/uint32/uint64,
//	[]any, map[string]any.
//
// For map[string]any, keys are emitted in lexicographic order. Encode returns
// an error for unsupported types.
func (e *Encoder) Encode(v any) error {
	switch x := v.(type) {
	case string:
		return e.encodeString(x)
	case []byte:
		return e.encodeString(string(x))
	case bool:
		if x {
			return e.encodeInt64(1)
		}
		return e.encodeInt64(0)
	case int:
		return e.encodeInt64(int64(x))
	case int8:
		return e.encodeInt64(int64(x))
	case int16:
		return e.encodeInt64(int64(x))
	case int32:
		return e.encodeInt64(int64(x))
	case int64:
		return e.encodeInt64(x)
	case uint:
		return e.encodeUint(uint64(x))
	case uint8:
		return e.encodeUint(uint64(x))
	case uint16:
		return e.encodeUint(uint64(x))
	case uint32:
		return e.encodeUint(uint64(x))
	case uint64:
		return e.encodeUint(x)
	case []any:
		return e.encodeSlice(x)
	case map[string]any:
		return e.encodeDict(x)
	default:
		return fmt.Errorf("bencode: unsupported datatype '%T'", v)
	}
}

// encodeInt64 writes an integer as: 'i' <base10 digits> 'e'.
// Example: i42e, i-7e.
func (e *Encoder) encodeInt64(n int64) error {
	if _, err := e.w.Write([]byte{TokenInteger.Byte()}); err != nil {
		return err
	}

	var buf [32]byte
	b := strconv.AppendInt(buf[:0], n, 10)
	if _, err := e.w.Write(b); err != nil {
		return err
	}

	_, err := e.w.Write([]byte{TokenEnding.Byte()})
	return err
}

// encodeUint writes an unsigned integer using the same integer production.
// Bencode has no distinct unsigned type; this uses base-10 digits without a
// sign.
func (e *Encoder) encodeUint(u uint64) error {
	if _, err := e.w.Write([]byte{TokenInteger.Byte()}); err != nil {
		return err
	}

	var buf [32]byte
	b := strconv.AppendUint(buf[:0], u, 10)
	if _, err := e.w.Write(b); err != nil {
		return err
	}

	_, err := e.w.Write([]byte{TokenEnding.Byte()})
	return err
}

// encodeString writes a byte string as: <len> ':' <bytes>.
// The length is the number of bytes in s; s is written as-is.
func (e *Encoder) encodeString(s string) error {
	size := len(s)

	var buf [32]byte
	b := strconv.AppendInt(buf[:0], int64(size), 10)
	if _, err := e.w.Write(b); err != nil {
		return err
	}

	if _, err := e.w.Write([]byte{TokenStringSeparator.Byte()}); err != nil {
		return err
	}

	_, err := io.WriteString(e.w, s)
	return err
}

// encodeSlice writes a list: 'l' <elements> 'e'.
// Each element is encoded recursively via Encode.
func (e *Encoder) encodeSlice(xs []any) error {
	if _, err := e.w.Write([]byte{TokenList.Byte()}); err != nil {
		return err
	}

	for _, v := range xs {
		if err := e.Encode(v); err != nil {
			return err
		}
	}

	_, err := e.w.Write([]byte{TokenEnding.Byte()})
	return err
}

// encodeDict writes a dictionary: 'd' <key><value> ... 'e'.
//
// Keys are encoded as strings and emitted in lexicographic order, as required
// by BEP 3. Values are encoded recursively via Encode.
func (e *Encoder) encodeDict(m map[string]any) error {
	if _, err := e.w.Write([]byte{TokenDict.Byte()}); err != nil {
		return err
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if err := e.encodeString(k); err != nil {
			return err
		}
		if err := e.Encode(m[k]); err != nil {
			return err
		}
	}

	_, err := e.w.Write([]byte{TokenEnding.Byte()})
	return err
}

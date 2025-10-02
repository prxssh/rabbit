package bencode

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
)

func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	e := NewEncoder(&buf)

	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type Encoder struct {
	w io.Writer
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

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

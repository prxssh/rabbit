package bencode

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Unmarshal parses a single complete bencoded value from data and returns it.
//
// Returns an error if the input is malformed, exceeds Decoder limits, or
// contains trailing data after the first value.
func Unmarshal(data []byte) (any, error) {
	d := NewDecoder(data)

	v, err := d.Decode()
	if err != nil {
		return nil, err
	}

	if _, err := d.r.Peek(1); err == nil {
		return nil, fmt.Errorf("bencoding: trailing data after first value")
	} else if err != io.EOF {
		return nil, err
	}

	return v, nil
}

// Token identifies syntactic markers in the bencode stream.
type Token byte

func (t Token) Byte() byte {
	return byte(t)
}

const (
	// TokenDict begins a dictionary: 'd'
	TokenDict Token = 'd'
	// TokenInteger begins an integer: 'i'
	TokenInteger Token = 'i'
	// TokenEnding terminates a list, dictionary, or integer: 'e'
	TokenEnding Token = 'e'
	// TokenList begins a list: 'l'
	TokenList Token = 'l'
	// TokenStringSeparator separates a string length from its data ':'
	TokenStringSeparator Token = ':'
)

// Decoder reads bencoded value from an in-memory byte slice.
//
// A Decoder is safe for use by a single goroutine at a time.
type Decoder struct {
	r         *bufio.Reader // source of bytes
	maxDepth  int           // maximum nesting depth
	maxStrLen int64         // maximum string length in bytes
	maxDigits int           // maximum base-10 digits in an integer
}

// NewDecoder returns a new Decoder reading from data with conservative limits.
// The returned Decoder is independent of data; the caller may modify data
// after construction.
func NewDecoder(data []byte) *Decoder {
	return &Decoder{
		r:         bufio.NewReader(bytes.NewBuffer(data)),
		maxDepth:  2048,     // protects against pathological nesting
		maxStrLen: 16 << 20, // 16 MiB
		maxDigits: 19,       // first int64 range
	}
}

// Decode parses and returns the next bencoded value from the input.
// It may return one of: int64, string, []any, or map[string]any.
//
// If limits are exceeded or input is malformed, Decode returns a non-nil
// error.
func (d *Decoder) Decode() (any, error) { return d.decode(0) }

// decode is the recursive implementation of Decode. depth is the current
// nesting level.
func (d *Decoder) decode(depth int) (any, error) {
	if depth > d.maxDepth {
		return nil, errors.New("max depth exceeded")
	}

	delim, err := d.r.ReadByte()
	if err != nil {
		return nil, err
	}

	switch delim {
	case byte(TokenDict):
		return d.decodeDict(depth + 1)
	case byte(TokenList):
		return d.decodeList(depth + 1)
	case byte(TokenInteger):
		return d.decodeInteger()
	default:
		if err := d.r.UnreadByte(); err != nil {
			return nil, err
		}

		return d.decodeString()
	}
}

// decodeDict parses a dictionary and returns it as map[string]any.
// Keys must be bencoded strings; values may be any bencoded type.
func (d *Decoder) decodeDict(depth int) (map[string]any, error) {
	dict := make(map[string]any, 8)

	for {
		next, err := d.r.Peek(1)
		if err != nil {
			return nil, err
		}
		if next[0] == byte(TokenEnding) {
			// consume 'e'
			if _, err := d.r.ReadByte(); err != nil {
				return nil, err
			}
			break
		}

		k, err := d.decodeString()
		if err != nil {
			return nil, err
		}
		v, err := d.decode(depth + 1)
		if err != nil {
			return nil, err
		}
		dict[k] = v
	}

	return dict, nil
}

// decodeList parses a list and returns it as []any.
func (d *Decoder) decodeList(depth int) ([]any, error) {
	var list []any

	for {
		next, err := d.r.Peek(1)
		if err != nil {
			return nil, err
		}
		if next[0] == byte(TokenEnding) {
			// consume 'e'
			if _, err := d.r.ReadByte(); err != nil {
				return nil, err
			}
			break
		}

		v, err := d.decode(depth + 1)
		if err != nil {
			return nil, err
		}
		list = append(list, v)
	}

	return list, nil
}

// decodeInteger parses an integer value 'i' <digits> 'e' and returns it as int64.
func (d *Decoder) decodeInteger() (int64, error) {
	return d.readInteger(TokenEnding)
}

// decodeString parses a byte string with the form <len> ':' <bytes> and
// returns it as a Go string.
func (d *Decoder) decodeString() (string, error) {
	n, err := d.readInteger(TokenStringSeparator)
	if err != nil {
		return "", err
	}

	if n < 0 {
		return "", fmt.Errorf("invalid string: length can't be negative")
	}
	if n > d.maxStrLen {
		return "", fmt.Errorf("string too large: %d > %d", n, d.maxStrLen)
	}
	if n == 0 {
		return "", nil
	}

	buf := make([]byte, n)
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return "", fmt.Errorf("read string: %w", err)
	}
	return string(buf), nil
}

// readInteger reads a base-10, optionally signed integer terminated by delim,
// enforcing d.maxDigits. The returned value is int64.
//
// For strings, delim should be TokenStringSeparator (':'); for numbers, 'e'.
// readInteger performs basic canonicality checks (no leading zeros, no "-0").
func (d *Decoder) readInteger(delim Token) (int64, error) {
	buf, err := d.r.ReadSlice(byte(delim))
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return 0, fmt.Errorf("integer too long")
		}
		return 0, err
	}

	// drop the delim
	n := len(buf) - 1
	if n <= 0 {
		return 0, fmt.Errorf("invalid integer: empty")
	}
	s := buf[:n]

	if s[0] == '-' {
		if n == 0 {
			return 0, fmt.Errorf("invalid integer: lone '-'")
		}
		if n > 1 && s[1] == '0' {
			return 0, fmt.Errorf("invalid integer: negative zero")
		}
	} else if s[0] == '0' && n > 1 {
		return 0, fmt.Errorf("invalid integer: leading zero")
	}

	if len(s) > d.maxDigits+1 {
		return 0, fmt.Errorf("invalid integer: too many digits")
	}

	v, err := strconv.ParseInt(string(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer: %w", err)
	}
	return v, nil
}

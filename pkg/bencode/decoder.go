package bencode

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

func Unmarshal(data []byte) (any, error) {
	d := NewDecoder(data)

	v, err := d.Decode()
	if err != nil {
		return nil, err
	}

	if _, err := d.r.Peek(1); err == nil {
		return nil, fmt.Errorf(
			"bencoding: trailing data after first value",
		)
	} else if err != io.EOF {
		return nil, err
	}

	return v, nil
}

type Token byte

func (t Token) Byte() byte {
	return byte(t)
}

const (
	TokenDict            Token = 'd'
	TokenInteger         Token = 'i'
	TokenEnding          Token = 'e'
	TokenList            Token = 'l'
	TokenStringSeparator Token = ':'
)

// Decoder reads bencoded values from a buffered reader. It implements a
// recursive descent over the data structure and therefore enforces a maximum
// nesting depth to prevent stack overflows on malicious inputs.
type Decoder struct {
	r         *bufio.Reader
	maxDepth  int   // Cap on nested lists/dicts
	maxStrLen int64 // Cap on single byte-string length
	maxDigits int   // Cap on decimal digits for ints/lengths
}

func NewDecoder(data []byte) *Decoder {
	return &Decoder{
		r:         bufio.NewReader(bytes.NewBuffer(data)),
		maxDepth:  2048,     // prevents pathological nesting
		maxStrLen: 16 << 20, // 16 MiB per string
		maxDigits: 19,       // fits int64 safely
	}
}

func (d *Decoder) Decode() (any, error) { return d.decode(0) }

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

func (d *Decoder) decodeInteger() (int64, error) {
	return d.readInteger(TokenEnding)
}

func (d *Decoder) decodeString() (string, error) {
	n, err := d.readInteger(TokenStringSeparator)
	if err != nil {
		return "", err
	}

	if n < 0 {
		return "", errors.New(
			"invalid string: length can't be negative",
		)
	}
	if n > d.maxStrLen {
		return "", fmt.Errorf(
			"string too large: %d > %d",
			n,
			d.maxStrLen,
		)
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

func (d *Decoder) readInteger(delim Token) (int64, error) {
	buf, err := d.r.ReadSlice(byte(delim))
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return 0, errors.New("integer too long")
		}
		return 0, err
	}

	// drop the delim
	n := len(buf) - 1
	if n <= 0 {
		return 0, errors.New("invalid integer: empty")
	}
	s := buf[:n]

	if s[0] == '-' {
		if n == 0 {
			return 0, errors.New("invalid integer: lone '-'")
		}
		if n > 1 && s[1] == '0' {
			return 0, errors.New("invalid integer: negative zero")
		}
	} else if s[0] == '0' && n > 1 {
		return 0, errors.New("invalid integer: leading zero")
	}

	if len(s) > d.maxDigits+1 {
		return 0, errors.New("invalid integer: too many digits")
	}

	v, err := strconv.ParseInt(string(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer: %w", err)
	}
	return v, nil
}

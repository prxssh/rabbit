package bitfield

import (
	"bytes"
	"math/bits"
)

type Bitfield []byte

func New(n int) Bitfield {
	size := (n + 7) / 8
	if size < 0 {
		size = 0
	}

	return make(Bitfield, size)
}

func FromBytes(b []byte) Bitfield {
	bf := make(Bitfield, len(b))
	copy(bf, b)
	return bf
}

func (bf Bitfield) ToBytes() []byte {
	out := make([]byte, len(bf))
	copy(out, bf)
	return out
}

func (bf Bitfield) Has(index int) bool {
	byteIndex, offset := index/8, index%8
	if byteIndex < 0 || byteIndex >= len(bf) {
		return false
	}
	return bf[byteIndex]>>(7-offset)&1 != 0
}

func (bf Bitfield) Set(index int) {
	byteIndex, offset := index/8, index%8
	if byteIndex < 0 || byteIndex >= len(bf) {
		return
	}

	bf[byteIndex] |= 1 << (7 - offset)
}

func (bf Bitfield) Clear(index int) {
	byteIndex, offset := index/8, index%8
	if byteIndex < 0 || byteIndex >= len(bf) {
		return
	}

	bf[byteIndex] &^= 1 << (7 - offset)
}

func (bf Bitfield) Len() int { return len(bf) * 8 }

func (bf Bitfield) Count() int {
	c := 0
	for _, b := range bf {
		c += bits.OnesCount8(b)
	}

	return c
}

func (bf Bitfield) Equals(other Bitfield) bool {
	return bytes.Equal(bf, other)
}

func (bf Bitfield) String() string {
	var buf bytes.Buffer

	for i := 0; i < bf.Len(); i++ {
		if bf.Has(i) {
			buf.WriteByte('1')
		} else {
			buf.WriteByte('0')
		}
	}

	return buf.String()
}

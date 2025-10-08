package bitfield

import (
	"bytes"
	"math/bits"
)

// Bitfield represents a fixed-sized bitset. Bits are stored MSB-first within
// each byte.
type Bitfield []byte

// New returns a zerored bitfield able to hold nbits bits.
// If nbits <= 0, New returns nil.
func New(nbits int) Bitfield {
	if nbits <= 0 {
		return nil
	}

	return make(Bitfield, (nbits+7)/8)
}

// FromBytes returns a new Bitfield that copies b.
func FromBytes(b []byte) Bitfield {
	return append(Bitfield(nil), b...)
}

// Bytes returns a copy of the underlying bytes.
func (bf Bitfield) Bytes() []byte {
	return append([]byte(nil), bf...)
}

// Has reports whether bit at idx is set.
//
// Returns false if idx is out of range.
func (bf Bitfield) Has(idx int) bool {
	if idx < 0 || idx >= bf.Len() {
		return false
	}

	at, offset := idx/8, 7-(idx%8)
	return (bf[at]>>offset)&1 == 1
}

// Set sets bit at idx.
//
// Returns true if the bit was changed, false if out-of-range or already set.
func (bf Bitfield) Set(idx int) bool {
	if idx < 0 || idx >= bf.Len() {
		return false
	}

	at, offset := idx/8, 7-(idx%8)
	mask := byte(1 << offset)
	old := bf[at]
	bf[at] = old | mask

	return old&mask == 0
}

// Clear clears bit at idx.
//
// Returns true if the bit was changed, false if out-of-range or already set.
func (bf Bitfield) Clear(idx int) bool {
	if idx < 0 || idx >= bf.Len() {
		return false
	}

	at, offset := idx/8, 7-(idx%8)
	mask := byte(1 << offset)
	old := bf[at]
	bf[at] = old &^ mask

	return old&mask != 0
}

// Len returns the number of addressable bits.
func (bf Bitfield) Len() int { return len(bf) * 8 }

// Count returns the number of set bits.
func (bf Bitfield) Count() int {
	n := 0
	for _, b := range bf {
		n += bits.OnesCount8(b)
	}

	return n
}

// Equals compares bitfields byte-wise.
func (bf Bitfield) Equals(oth Bitfield) bool {
	return bytes.Equal(bf, oth)
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

package bitfield

import (
	"bytes"
	"math/bits"
)

// Bitfield represents a fixed-size bitset. Bits are stored MSB-first within
// each byte.
type Bitfield []byte

// New returns a zeroed bitfield able to hold nbits bits.
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

// Len returns the number of addressable bits.
func (bf Bitfield) Len() int { return len(bf) * 8 }

// Has reports whether bit at index is set. Returns false if index is out of
// range.
func (bf Bitfield) Has(index int) bool {
	if index < 0 || index >= bf.Len() {
		return false
	}

	byteIndex, off := index/8, 7-(index%8)
	return (bf[byteIndex]>>off)&1 == 1
}

// Set sets bit at index. It returns true if the bit was changed, false if
// out-of-range or already set.
func (bf Bitfield) Set(index int) bool {
	if index < 0 || index >= bf.Len() {
		return false
	}

	byteIndex, off := index/8, 7-(index%8)
	mask := byte(1 << off)
	old := bf[byteIndex]
	bf[byteIndex] = old | mask

	return old&mask == 0
}

// Clear clears bit at index. It returns true if the bit was changed, false if
// out-of-range or already clear.
func (bf Bitfield) Clear(index int) bool {
	if index < 0 || index >= bf.Len() {
		return false
	}

	byteIndex, off := index/8, 7-(index%8)
	mask := byte(1 << off)
	old := bf[byteIndex]
	bf[byteIndex] = old &^ mask

	return old&mask != 0
}

// Count returns the number of set bits.
func (bf Bitfield) Count() int {
	n := 0
	for _, b := range bf {
		n += bits.OnesCount8(b)
	}

	return n
}

// Any reports whether any bit is set.
func (bf Bitfield) Any() bool { return bf.Count() != 0 }

// None reports whether no bit is set.
func (bf Bitfield) None() bool { return bf.Count() == 0 }

// All reports whether all bits in the last full byte range are set.
func (bf Bitfield) All() bool {
	for _, b := range bf {
		if b != 0xFF {
			return false
		}
	}

	return len(bf) > 0
}

// Equals compares bitfields byte-wise.
func (bf Bitfield) Equals(other Bitfield) bool {
	return bytes.Equal(bf, other)
}

// Clone returns an independent copy.
func (bf Bitfield) Clone() Bitfield { return bf.Bytes() }

// String returns a 0/1 bitstring (MSB-first). Fast via a precomputed table.
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

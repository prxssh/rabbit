package dht

import (
	"bytes"
	"crypto/sha1"
	"math/bits"
)

func Distance(a, b [sha1.Size]byte) [sha1.Size]byte {
	var d [sha1.Size]byte

	for i := 0; i < sha1.Size; i++ {
		d[i] = a[i] ^ b[i]
	}
	return d
}

// CompareDistance returns:
// -1 if a is closer to target than b
// 0 if a and b are equidistant to target
// 1 if b is closer to target than a
func CompareDistance(target, a, b [sha1.Size]byte) int {
	da := Distance(target, a)
	db := Distance(target, b)
	return bytes.Compare(da[:], db[:])
}

// PrefixLen returns the number of leading zero bits in the XOR distance.
// Used to determine which bucket the node belongs to.
func PrefixLen(a, b [sha1.Size]byte) int {
	d := Distance(a, b)

	for i := 0; i < sha1.Size; i++ {
		if d[i] != 0 {
			return i*8 + bits.LeadingZeros(uint(d[i]))
		}
	}

	return sha1.Size * 8 // Identical
}

// BucketIndex returns which bucket (0-159) a node belongs to
// relative to the local node id.
func BucketIndex(localID, remoteID [sha1.Size]byte) int {
	prefixLen := PrefixLen(localID, remoteID)

	if prefixLen >= 159 {
		return 159
	}
	return 159 - prefixLen
}

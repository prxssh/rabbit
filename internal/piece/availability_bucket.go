package piece

import (
	"math/bits"
	"math/rand"
	"sync"

	"github.com/prxssh/rabbit/internal/config"
)

// AvailabilityBucket efficiently tracks which pieces belong to each
// availability level (i.e., how many peers currently have that piece).
//
// It maintains O(1) updates when peers join/leave by moving piece indices
// between small dense arrays ("buckets") and records each piece's current
// bucket position for constant-time removals.
//
// The structure is highly cache-friendly and supports fast rarest-first
// selection via a compact bitmap of non-empty buckets.
type availabilityBucket struct {
	mu sync.RWMutex

	// buckets[a] holds a dense slice of piece indices whose availability
	// equals 'a'. For example, buckets[3] contains all pieces that exactly
	// 3 peers currently have.
	//
	// Buckets are always densely packed: when a piece moves, it is removed
	// via swap-with-last, ensuring O(1) deletion and preserving
	// compactness.
	buckets [][]int

	// avail[i] stores the current availability count for piece i. Values
	// range from 0..maxAvail inclusive.
	//
	// This acts as the authoritative record of each piece’s rarity and is
	// used to determine which bucket the piece belongs to.
	avail []uint16

	// pos[i] gives the index of piece i inside buckets[avail[i]].
	//
	// This allows constant-time swap-remove when a piece moves to a new
	// availability bucket.
	pos []int

	// maxAvail is the upper bound on availability, typically equal to
	// MaxPeers. It defines the maximum number of buckets.
	maxAvail int

	// nonEmptyBits is a bitmap representing which buckets currently contain
	// at least one piece. Bit k in word w corresponds to bucket index
	// (w*64 + k).
	//
	// This lets the picker find the smallest non-empty bucket (the rarest
	// pieces) in O(1)–O(64) time without scanning every bucket.
	nonEmptyBits []uint64

	rng *rand.Rand
}

func newAvailabilityBucket(pieceCount int) *availabilityBucket {
	maxAvail := config.Load().MaxPeers
	rng := rand.New(rand.NewSource(rand.Int63()))

	b := &availabilityBucket{
		rng:          rng,
		maxAvail:     maxAvail,
		buckets:      make([][]int, maxAvail+1),
		avail:        make([]uint16, pieceCount),
		pos:          make([]int, pieceCount),
		nonEmptyBits: make([]uint64, (maxAvail>>6)+1),
	}

	capacity := max(1, pieceCount/(maxAvail+1))
	for a := range b.buckets {
		b.buckets[a] = make([]int, 0, capacity)
	}

	b.buckets[0] = make([]int, pieceCount)
	for i := 0; i < pieceCount; i++ {
		b.buckets[0][i] = i
		b.pos[i] = i
		b.avail[i] = 0
	}
	b.setBit(0)

	return b
}

// Availability returns the current availability of piece i.
func (b *availabilityBucket) Availability(i int) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return int(b.avail[i])
}

// FirstNonEmpty returns the smallest availability a that has at least one
// piece.
func (b *availabilityBucket) FirstNonEmpty() (a int, ok bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for w := 0; w < len(b.nonEmptyBits); w++ {
		if x := b.nonEmptyBits[w]; x != 0 {
			off := bits.TrailingZeros64(x)
			return (w<<6 + off), true
		}
	}

	return 0, false
}

func (b *availabilityBucket) Bucket(a int) []int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if a < 0 || a > b.maxAvail {
		return nil
	}

	return append([]int(nil), b.buckets[a]...)
}

// Move changes the availability count for piece i by delta (+1 or -1).
func (b *availabilityBucket) Move(i, delta int) {
	b.mu.RLock()
	oldA := int(b.avail[i])
	newA := min(b.maxAvail, max(0, oldA+delta))
	b.mu.RUnlock()

	if newA == oldA {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.removeFrom(i, oldA)
	b.addTo(i, newA)
	b.avail[i] = uint16(newA)
}

// removeFrom removes piece i from buckets[avail].
func (b *availabilityBucket) removeFrom(i, avail int) {
	pos := b.pos[i]
	bucket := b.buckets[avail]
	lastIdx := len(bucket) - 1

	bucket[pos] = bucket[lastIdx]
	b.pos[bucket[pos]] = pos
	bucket = bucket[:lastIdx]
	b.buckets[avail] = bucket

	if len(bucket) == 0 {
		b.clearBit(avail)
	}
}

// addTo inserts piece i into buckets[avail], randomizing its position slightly
// to avoid deterministic herding behavior.
func (b *availabilityBucket) addTo(i, avail int) {
	bucket := b.buckets[avail]
	bucket = append(bucket, i)
	idx := len(bucket) - 1

	if idx > 0 {
		j := b.rng.Intn(idx + 1)
		bucket[idx], bucket[j] = bucket[j], bucket[idx]
		b.pos[bucket[idx]] = idx
		b.pos[bucket[j]] = j
	} else {
		b.pos[i] = 0
	}

	b.buckets[avail] = bucket
	b.setBit(avail)
}

func (b *availabilityBucket) setBit(a int) {
	w, bit := a>>6, uint(a&63)
	b.nonEmptyBits[w] |= 1 << bit
}

func (b *availabilityBucket) clearBit(a int) {
	w, bit := a>>6, uint(a&63)
	b.nonEmptyBits[w] &^= 1 << bit
}

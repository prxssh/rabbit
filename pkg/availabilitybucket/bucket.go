package availabilitybucket

import (
	"math/bits"
	"math/rand"
	"sync"
)

// Bucket is a generic data structure that efficiently tracks items by their
// availability count.
//
// It supports O(1) availability updates and O(1) random selection from any
// availability level.
type Bucket struct {
	rng *rand.Rand
	mut sync.RWMutex

	// buckets[a] holds a dense slice of items whose availability equals 'a'.
	// For example, buckets[3] contains all items that exactly 3 peers have.
	//
	// Buckets are always densely packed: when an item moves, it is removed
	// via swap-with-last, ensuring O(1) deletion.
	buckets [][]int

	// avail[item] stores the current availability count for item.
	// Values range from 0..maxAvail inclusive.
	avail []uint16

	// pos[item] gives the index of item inside buckets[avail[item]].
	pos []int

	// maxAvail is the upper bound on availability.
	maxAvail int

	// nonEmptyBits is a bitmap representing which buckets currently contain
	// at least one item. Bit k in word w corresponds to bucket index (w*64 + k).
	nonEmptyBits []uint64
}

func NewBucket(n, maxAvail int) *Bucket {
	rng := rand.New(rand.NewSource(rand.Int63()))

	b := &Bucket{
		rng:          rng,
		maxAvail:     maxAvail,
		buckets:      make([][]int, maxAvail+1),
		avail:        make([]uint16, n),
		pos:          make([]int, n),
		nonEmptyBits: make([]uint64, (maxAvail>>6)+1),
	}

	capacity := max(1, n/(maxAvail+1))
	for a := range b.buckets {
		b.buckets[a] = make([]int, 0, capacity)
	}

	b.buckets[0] = make([]int, n)
	for i := 0; i < int(n); i++ {
		b.buckets[0][i] = i
		b.pos[i] = i
		b.avail[i] = 0
	}
	b.setBit(0)

	return b
}

// Availability returns the current availability of piece i.
func (b *Bucket) Availability(i int) int {
	b.mut.RLock()
	defer b.mut.RUnlock()

	return int(b.avail[i])
}

// FirstNonEmpty returns the smallest availability a that has at least one
// piece.
func (b *Bucket) FirstNonEmpty() (a int, ok bool) {
	b.mut.RLock()
	defer b.mut.RUnlock()

	for w := 0; w < len(b.nonEmptyBits); w++ {
		if x := b.nonEmptyBits[w]; x != 0 {
			off := bits.TrailingZeros64(x)
			return (w<<6 + off), true
		}
	}

	return 0, false
}

func (b *Bucket) Bucket(a int) []int {
	b.mut.RLock()
	defer b.mut.RUnlock()

	if a < 0 || a > b.maxAvail {
		return nil
	}

	return append([]int(nil), b.buckets[a]...)
}

// Move changes the availability count for piece i by delta (+1 or -1).
func (b *Bucket) Move(i, delta int) {
	b.mut.Lock()
	defer b.mut.Unlock()

	oldA := int(b.avail[i])
	newA := min(b.maxAvail, max(0, oldA+delta))

	if newA == oldA {
		return
	}

	b.removeFrom(i, oldA)
	b.addTo(i, newA)
	b.avail[i] = uint16(newA)
}

// removeFrom removes piece i from buckets[avail].
func (b *Bucket) removeFrom(i, avail int) {
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
func (b *Bucket) addTo(i, avail int) {
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

func (b *Bucket) setBit(a int) {
	w, bit := a>>6, uint(a&63)
	b.nonEmptyBits[w] |= 1 << bit
}

func (b *Bucket) clearBit(a int) {
	w, bit := a>>6, uint(a&63)
	b.nonEmptyBits[w] &^= 1 << bit
}

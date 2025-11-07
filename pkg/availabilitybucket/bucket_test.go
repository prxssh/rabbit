package availabilitybucket

import (
	"math/rand"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

// checkInvariants verifies that the internal state of the bucket is consistent.
func checkInvariants(t *testing.T, b *Bucket, n int) {
	t.Helper()

	b.mut.RLock()
	defer b.mut.RUnlock()

	totalItems := 0
	// seen is used to detect duplicates or missing items.
	seen := make(map[int]bool, n)

	for a, bucket := range b.buckets {
		totalItems += len(bucket)

		// Check bitmap consistency
		w, bit := a>>6, uint(a&63)
		isSet := (b.nonEmptyBits[w] & (1 << bit)) != 0
		isEmpty := len(bucket) == 0

		if isSet && isEmpty {
			t.Errorf("invariant violation: bit %d is set, but bucket %d is empty", a, a)
		}
		if !isSet && !isEmpty {
			t.Errorf(
				"invariant violation: bit %d is clear, but bucket %d has %d items",
				a,
				a,
				len(bucket),
			)
		}

		// Check each item in the bucket
		for posInBucket, i := range bucket {
			if i < 0 || i >= n {
				t.Errorf(
					"invariant violation: item %d in bucket %d is out of bounds [0, %d)",
					i,
					a,
					n,
				)
				continue
			}

			if seen[i] {
				t.Errorf(
					"invariant violation: item %d found in multiple buckets or positions",
					i,
				)
			}
			seen[i] = true

			if int(b.avail[i]) != a {
				t.Errorf(
					"invariant violation: item %d in bucket %d, but b.avail[%d] = %d",
					i,
					a,
					i,
					b.avail[i],
				)
			}
			if b.pos[i] != posInBucket {
				t.Errorf(
					"invariant violation: item %d in bucket %d at pos %d, but b.pos[%d] = %d",
					i,
					a,
					posInBucket,
					i,
					b.pos[i],
				)
			}
		}
	}

	if totalItems != n {
		t.Errorf(
			"invariant violation: total items mismatch. Expected %d, found %d",
			n,
			totalItems,
		)
	}

	// For n > 0, we must have seen all items.
	if n > 0 && len(seen) != n {
		t.Errorf(
			"invariant violation: item count mismatch. Expected %d unique items, found %d",
			n,
			len(seen),
		)
	}
}

// TestNewBucket checks the initial state after creation.
func TestNewBucket(t *testing.T) {
	n, maxAvail := 100, 10
	b := NewBucket(n, maxAvail)

	if b.maxAvail != maxAvail {
		t.Fatalf("expected maxAvail %d, got %d", maxAvail, b.maxAvail)
	}
	if len(b.buckets) != maxAvail+1 {
		t.Fatalf("expected %d buckets, got %d", maxAvail+1, len(b.buckets))
	}
	if len(b.avail) != n {
		t.Fatalf("expected avail size %d, got %d", n, len(b.avail))
	}
	if len(b.pos) != n {
		t.Fatalf("expected pos size %d, got %d", n, len(b.pos))
	}
	if len(b.buckets[0]) != n {
		t.Fatalf("expected bucket[0] size %d, got %d", n, len(b.buckets[0]))
	}

	// Check initial availability and position for all items
	for i := 0; i < n; i++ {
		if b.Availability(i) != 0 {
			t.Errorf("expected avail[%d] = 0, got %d", i, b.avail[i])
		}
		if b.pos[i] != i {
			t.Errorf("expected pos[%d] = %d, got %d", i, i, b.pos[i])
		}
		if b.buckets[0][i] != i {
			t.Errorf("expected buckets[0][%d] = %d, got %d", i, i, b.buckets[0][i])
		}
	}

	// Check bitmap
	if b.nonEmptyBits[0] != 1 {
		t.Errorf("expected nonEmptyBits[0] = 1, got %d", b.nonEmptyBits[0])
	}

	// Check FirstNonEmpty
	a, ok := b.FirstNonEmpty()
	if !ok || a != 0 {
		t.Errorf("expected FirstNonEmpty = (0, true), got (%d, %v)", a, ok)
	}

	// Check invariants
	checkInvariants(t, b, n)
}

// TestNewBucketEmpty tests the edge case of n=0.
func TestNewBucketEmpty(t *testing.T) {
	n, maxAvail := 0, 5
	b := NewBucket(n, maxAvail)

	if len(b.avail) != 0 {
		t.Fatalf("expected avail size 0, got %d", len(b.avail))
	}
	if len(b.buckets[0]) != 0 {
		t.Fatalf("expected bucket[0] size 0, got %d", len(b.buckets[0]))
	}

	a, ok := b.FirstNonEmpty()
	if ok {
		t.Errorf("expected FirstNonEmpty = (0, false) for n=0, got (%d, %v)", a, ok)
	}

	checkInvariants(t, b, n)
}

// TestMoveBasic tests simple +1 and -1 moves.
func TestMoveBasic(t *testing.T) {
	n, maxAvail := 10, 5
	b := NewBucket(n, maxAvail)
	item := 4

	// Move item 4 up from 0 to 1
	b.Move(item, 1)
	if b.Availability(item) != 1 {
		t.Fatalf("expected avail=1, got %d", b.Availability(item))
	}
	if len(b.buckets[0]) != n-1 {
		t.Fatalf("expected bucket[0] size %d, got %d", n-1, len(b.buckets[0]))
	}
	if len(b.buckets[1]) != 1 {
		t.Fatalf("expected bucket[1] size 1, got %d", len(b.buckets[1]))
	}
	checkInvariants(t, b, n)

	// Move item 4 up from 1 to 2
	b.Move(item, 1)
	if b.Availability(item) != 2 {
		t.Fatalf("expected avail=2, got %d", b.Availability(item))
	}
	if len(b.buckets[1]) != 0 {
		t.Fatalf("expected bucket[1] size 0, got %d", len(b.buckets[1]))
	}
	if len(b.buckets[2]) != 1 {
		t.Fatalf("expected bucket[2] size 1, got %d", len(b.buckets[2]))
	}
	checkInvariants(t, b, n)

	// Move item 4 down from 2 to 1
	b.Move(item, -1)
	if b.Availability(item) != 1 {
		t.Fatalf("expected avail=1, got %d", b.Availability(item))
	}
	if len(b.buckets[1]) != 1 {
		t.Fatalf("expected bucket[1] size 1, got %d", len(b.buckets[1]))
	}
	if len(b.buckets[2]) != 0 {
		t.Fatalf("expected bucket[2] size 0, got %d", len(b.buckets[2]))
	}
	checkInvariants(t, b, n)
}

// TestMoveBoundaries tests clamping at 0 and maxAvail.
func TestMoveBoundaries(t *testing.T) {
	n, maxAvail := 2, 3
	b := NewBucket(n, maxAvail)
	item := 0

	// Move below 0
	b.Move(item, -1)
	if b.Availability(item) != 0 {
		t.Fatalf("expected avail=0 after moving below 0, got %d", b.Availability(item))
	}
	if len(b.buckets[0]) != n {
		t.Fatalf("expected bucket[0] size %d, got %d", n, len(b.buckets[0]))
	}
	checkInvariants(t, b, n)

	// Move up to maxAvail
	for i := 0; i <= maxAvail; i++ {
		b.Move(item, 1)
	}

	if b.Availability(item) != maxAvail {
		t.Fatalf("expected avail=%d, got %d", maxAvail, b.Availability(item))
	}
	if len(b.buckets[maxAvail]) != 1 {
		t.Fatalf("expected bucket[maxAvail] size 1, got %d", len(b.buckets[maxAvail]))
	}
	checkInvariants(t, b, n)

	// Move above maxAvail
	b.Move(item, 1)
	if b.Availability(item) != maxAvail {
		t.Fatalf(
			"expected avail=%d after moving above max, got %d",
			maxAvail,
			b.Availability(item),
		)
	}
	if len(b.buckets[maxAvail]) != 1 {
		t.Fatalf("expected bucket[maxAvail] size 1, got %d", len(b.buckets[maxAvail]))
	}
	checkInvariants(t, b, n)

	// Move back down to 0
	for i := 0; i <= maxAvail; i++ {
		b.Move(item, -1)
	}

	if b.Availability(item) != 0 {
		t.Fatalf("expected avail=0, got %d", b.Availability(item))
	}
	if len(b.buckets[0]) != n {
		t.Fatalf("expected bucket[0] size %d, got %d", n, len(b.buckets[0]))
	}
	checkInvariants(t, b, n)
}

// TestFirstNonEmpty tracks the lowest bucket as it changes.
func TestFirstNonEmpty(t *testing.T) {
	n, maxAvail := 2, 3
	b := NewBucket(n, maxAvail)

	checkFnE := func(wantA int, wantOK bool) {
		t.Helper()
		gotA, gotOK := b.FirstNonEmpty()
		if gotA != wantA || gotOK != wantOK {
			t.Fatalf(
				"FirstNonEmpty: want (%d, %v), got (%d, %v)",
				wantA,
				wantOK,
				gotA,
				gotOK,
			)
		}
	}

	checkFnE(0, true) // Initially [0, 1] in bucket 0

	b.Move(0, 1) // [1] in bucket 0, [0] in bucket 1
	checkFnE(0, true)

	b.Move(1, 1) // [] in bucket 0, [0, 1] in bucket 1
	checkFnE(1, true)

	b.Move(0, 1) // [1] in bucket 1, [0] in bucket 2
	checkFnE(1, true)

	b.Move(1, 2) // [] in bucket 1, [0] in bucket 2, [1] in bucket 3
	checkFnE(2, true)

	b.Move(0, 1) // [] in bucket 2, [0, 1] in bucket 3
	checkFnE(3, true)
}

// TestBucketAccessor tests the Bucket() method.
func TestBucketAccessor(t *testing.T) {
	n, maxAvail := 3, 2
	b := NewBucket(n, maxAvail) // [0, 1, 2] in bucket 0

	b.Move(1, 1) // [0, 2] in bucket 0, [1] in bucket 1
	b.Move(0, 2) // [2] in bucket 0, [1] in bucket 1, [0] in bucket 2

	// Test out of bounds
	if b.Bucket(-1) != nil {
		t.Error("expected nil for bucket -1")
	}
	if b.Bucket(maxAvail+1) != nil {
		t.Error("expected nil for bucket maxAvail+1")
	}

	// Helper to get and sort a bucket
	getSorted := func(a int) []int {
		s := b.Bucket(a)
		sort.Ints(s)
		return s
	}

	// Check contents
	if !reflect.DeepEqual(getSorted(0), []int{2}) {
		t.Errorf("expected bucket 0 = [2], got %v", b.Bucket(0))
	}
	if !reflect.DeepEqual(getSorted(1), []int{1}) {
		t.Errorf("expected bucket 1 = [1], got %v", b.Bucket(1))
	}
	if !reflect.DeepEqual(getSorted(2), []int{0}) {
		t.Errorf("expected bucket 2 = [0], got %v", b.Bucket(2))
	}

	// Test that it returns a copy
	b1 := b.Bucket(1)
	if b1 == nil {
		t.Fatal("bucket 1 is nil")
	}
	b1[0] = 999 // Mutate the returned slice
	if b.buckets[1][0] == 999 {
		t.Fatal("Bucket() did not return a copy")
	}
	if b.Availability(1) != 1 {
		t.Fatal("mutation corrupted internal state")
	}
}

// TestBitmapLarge tests that the bitmap works across word boundaries.
func TestBitmapLarge(t *testing.T) {
	n, maxAvail := 1, 130
	b := NewBucket(n, maxAvail) // 3 bitmap words (0-63, 64-127, 128-191)

	if len(b.nonEmptyBits) != 3 {
		t.Fatalf("expected 3 bitmap words, got %d", len(b.nonEmptyBits))
	}

	checkFnE := func(wantA int, wantOK bool) {
		t.Helper()
		gotA, gotOK := b.FirstNonEmpty()
		if gotA != wantA || gotOK != wantOK {
			t.Fatalf(
				"FirstNonEmpty: want (%d, %v), got (%d, %v)",
				wantA,
				wantOK,
				gotA,
				gotOK,
			)
		}
	}

	checkFnE(0, true)
	if b.nonEmptyBits[0] != 1 || b.nonEmptyBits[1] != 0 || b.nonEmptyBits[2] != 0 {
		t.Fatal("bitmap initial state wrong")
	}

	// Move to bucket 70 (second bitmap word)
	for i := 0; i < 70; i++ {
		b.Move(0, 1)
	}
	if b.Availability(0) != 70 {
		t.Fatalf("expected avail=70, got %d", b.Availability(0))
	}
	checkFnE(70, true)
	if b.nonEmptyBits[0] != 0 || b.nonEmptyBits[1] == 0 || b.nonEmptyBits[2] != 0 {
		t.Fatal("bitmap state wrong for bucket 70")
	}
	checkInvariants(t, b, n)

	// Move to bucket 129 (third bitmap word)
	for i := 0; i < 59; i++ { // 70 + 59 = 129
		b.Move(0, 1)
	}
	if b.Availability(0) != 129 {
		t.Fatalf("expected avail=129, got %d", b.Availability(0))
	}
	checkFnE(129, true)
	if b.nonEmptyBits[0] != 0 || b.nonEmptyBits[1] != 0 || b.nonEmptyBits[2] == 0 {
		t.Fatal("bitmap state wrong for bucket 129")
	}
	checkInvariants(t, b, n)

	// Move back to 0
	for i := 0; i < 129; i++ {
		b.Move(0, -1)
	}
	checkFnE(0, true)
	if b.nonEmptyBits[0] != 1 || b.nonEmptyBits[1] != 0 || b.nonEmptyBits[2] != 0 {
		t.Fatal("bitmap state wrong after moving back to 0")
	}
	checkInvariants(t, b, n)
}

// TestBucketConcurrentMoves performs many concurrent moves and checks
// for race conditions (with -race flag) and final state consistency.
func TestBucketConcurrentMoves(t *testing.T) {
	n, maxAvail := 100, 10
	b := NewBucket(n, maxAvail)

	numGoroutines := 16
	movesPerGoroutine := 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(g int) {
			defer wg.Done()
			// Create a local RNG for this goroutine
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(g)))

			for i := 0; i < movesPerGoroutine; i++ {
				item := rng.Intn(n)
				delta := rng.Intn(2)*2 - 1 // Randomly +1 or -1
				b.Move(item, delta)
			}
		}(g)
	}

	wg.Wait()

	// After all concurrent moves, check the final state for consistency.
	// The -race detector will fail the test if any data races occurred.
	checkInvariants(t, b, n)
}

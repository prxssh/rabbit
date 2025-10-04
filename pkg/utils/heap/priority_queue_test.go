package heap

import (
	"reflect"
	"sort"
	"testing"
)

func TestPriorityQueue_MinHeapOrder(t *testing.T) {
	pq := NewPriorityQueue[int](func(a, b int) bool { return a < b })

	input := []int{3, 1, 4, 1, 5, 9, 2, 6, 5}
	for _, v := range input {
		pq.Enqueue(v)
	}

	var got []int
	for {
		v, ok := pq.Dequeue()
		if !ok {
			break
		}
		got = append(got, v)
	}

	want := make([]int, len(input))
	copy(want, input)
	sort.Ints(want)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf(
			"min-heap order mismatch:\n got: %v\nwant: %v",
			got,
			want,
		)
	}
}

func TestPriorityQueue_MaxHeapOrder(t *testing.T) {
	pq := NewPriorityQueue[int](func(a, b int) bool { return a > b })

	input := []int{3, 1, 4, 1, 5, 9, 2, 6, 5}
	for _, v := range input {
		pq.Enqueue(v)
	}

	var got []int
	for {
		v, ok := pq.Dequeue()
		if !ok {
			break
		}
		got = append(got, v)
	}

	want := make([]int, len(input))
	copy(want, input)
	sort.Sort(sort.Reverse(sort.IntSlice(want)))

	if !reflect.DeepEqual(got, want) {
		t.Fatalf(
			"max-heap order mismatch:\n got: %v\nwant: %v",
			got,
			want,
		)
	}
}

func TestPriorityQueue_PeekDoesNotRemove(t *testing.T) {
	// Min-heap behavior
	pq := NewPriorityQueue[int](func(a, b int) bool { return a < b })
	input := []int{7, 3, 5, 1}
	for _, v := range input {
		pq.Enqueue(v)
	}

	top, ok := pq.Peek()
	if !ok {
		t.Fatalf("expected peek on non-empty queue to succeed")
	}

	// For min-heap, the smallest should be at the top
	if top != 1 {
		t.Fatalf("unexpected peek value: got %d, want %d", top, 1)
	}

	// Dequeue should return the same top element first
	first, ok := pq.Dequeue()
	if !ok {
		t.Fatalf("expected dequeue to succeed after peek")
	}
	if first != top {
		t.Fatalf(
			"dequeue after peek mismatch: got %d, want %d",
			first,
			top,
		)
	}
}

func TestPriorityQueue_EmptyBehavior(t *testing.T) {
	pq := NewPriorityQueue[int](func(a, b int) bool { return a < b })

	if _, ok := pq.Peek(); !ok {
		// ok to be false on empty, ensure it is false
	} else {
		t.Fatalf("peek on empty queue should fail")
	}

	if _, ok := pq.Dequeue(); !ok {
		// ok to be false on empty, ensure it is false
	} else {
		t.Fatalf("dequeue on empty queue should fail")
	}
}

package heap

import "container/heap"

type PriorityQueue[T any] struct {
	items    []*Item[T]
	lessFunc func(a, b T) bool
}

type Item[T any] struct {
	Value T
	Index int
}

func NewPriorityQueue[T any](lessFunc func(a, b T) bool) *PriorityQueue[T] {
	pq := &PriorityQueue[T]{
		items:    make([]*Item[T], 0),
		lessFunc: lessFunc,
	}
	heap.Init(pq)

	return pq
}

func (pq PriorityQueue[T]) Len() int { return len(pq.items) }

func (pq PriorityQueue[T]) Less(i, j int) bool {
	return pq.lessFunc(pq.items[i].Value, pq.items[j].Value)
}

func (pq PriorityQueue[T]) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[j].Index = i
	pq.items[i].Index = j
}

func (pq *PriorityQueue[T]) Push(x any) {
	n := len(pq.items)
	item := x.(*Item[T])
	item.Index = n
	pq.items = append(pq.items, item)
}

func (pq *PriorityQueue[T]) Pop() any {
	old := pq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.Index = -1
	pq.items = old[0 : n-1]
	return item
}

func (pq *PriorityQueue[T]) Enqueue(value T) {
	heap.Push(pq, &Item[T]{Value: value})
}

func (pq *PriorityQueue[T]) Dequeue() (T, bool) {
	if pq.Len() == 0 {
		var zero T
		return zero, false
	}

	item := heap.Pop(pq).(*Item[T])
	return item.Value, true
}

func (pq *PriorityQueue[T]) Peek() (T, bool) {
	if pq.Len() == 0 {
		var zero T
		return zero, false
	}

	return pq.items[0].Value, true
}

package syncmap

import "sync"

type Map[K comparable, V any] struct {
	mut  sync.RWMutex
	data map[K]V
}

func New[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{data: make(map[K]V)}
}

func (m *Map[K, V]) Put(key K, val V) {
	m.mut.Lock()
	m.data[key] = val
	m.mut.Unlock()
}

func (m *Map[K, V]) Get(key K) (V, bool) {
	m.mut.RLock()
	val, exists := m.data[key]
	m.mut.Unlock()

	return val, exists
}

func (m *Map[K, V]) Delete(keys ...K) {
	for _, key := range keys {
		m.mut.Lock()
		delete(m.data, key)
		m.mut.Unlock()
	}
}

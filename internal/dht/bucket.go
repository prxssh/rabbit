package dht

import (
	"crypto/sha1"
	"sync"
	"time"
)

const K = 8

type Bucket struct {
	mut         sync.RWMutex
	contacts    []*Contact
	lastChanged time.Time
}

func NewBucket() *Bucket {
	return &Bucket{
		contacts:    make([]*Contact, 0, K),
		lastChanged: time.Now(),
	}
}

func (b *Bucket) Len() int {
	b.mut.RLock()
	defer b.mut.RUnlock()

	return len(b.contacts)
}

func (b *Bucket) IsFull() bool {
	b.mut.RLock()
	defer b.mut.RUnlock()

	return len(b.contacts) >= K
}

func (b *Bucket) Get(id [sha1.Size]byte) *Contact {
	b.mut.RLock()
	defer b.mut.RUnlock()

	for _, c := range b.contacts {
		if c.ID() == id {
			return c
		}
	}

	return nil
}

func (b *Bucket) Insert(contact *Contact) bool {
	b.mut.Lock()
	defer b.mut.Unlock()

	for i, c := range b.contacts {
		if c.ID() == contact.ID() {
			b.contacts = append(b.contacts[:i], b.contacts[i+1:]...)
			b.contacts = append(b.contacts, contact)
			b.lastChanged = time.Now()
			return true
		}
	}

	if len(b.contacts) < K {
		b.contacts = append(b.contacts, contact)
		b.lastChanged = time.Now()
		return true
	}

	return false
}

func (b *Bucket) Remove(id [sha1.Size]byte) bool {
	b.mut.Lock()
	defer b.mut.RUnlock()

	for i, c := range b.contacts {
		if c.ID() == id {
			b.contacts = append(b.contacts[:i], b.contacts[i+1:]...)
			b.lastChanged = time.Now()
			return true
		}
	}

	return false
}

func (b *Bucket) LRU() *Contact {
	b.mut.RLock()
	defer b.mut.RUnlock()

	if len(b.contacts) == 0 {
		return nil
	}
	return b.contacts[0]
}

func (b *Bucket) NeedsRefresh() bool {
	b.mut.RLock()
	defer b.mut.RUnlock()

	return time.Since(b.lastChanged) > 15*time.Minute
}

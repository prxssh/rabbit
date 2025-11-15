package dht

import (
	"crypto/sha1"
	"sort"
	"sync"
)

const BucketSize = 160

type RoutingTable struct {
	localID [sha1.Size]byte
	mut     sync.RWMutex
	buckets [BucketSize]*Bucket
}

func NewRoutingTable(localID [sha1.Size]byte) *RoutingTable {
	rt := &RoutingTable{localID: localID}
	for i := 0; i < BucketSize; i++ {
		rt.buckets[i] = NewBucket()
	}

	return rt
}

func (rt *RoutingTable) ID() [sha1.Size]byte {
	return rt.localID
}

func (rt *RoutingTable) Insert(contact *Contact) bool {
	if contact.ID() == rt.localID {
		return false
	}

	bucketIdx := BucketIndex(rt.localID, contact.ID())
	bucket := rt.buckets[bucketIdx]

	if bucket.Insert(contact) {
		return true
	}
	return rt.handleFullBucket(bucket, contact)
}

func (rt *RoutingTable) handleFullBucket(bucket *Bucket, newContact *Contact) bool {
	lru := bucket.LRU()
	if lru == nil {
		return false
	}

	if lru.IsBad() {
		bucket.Remove(lru.ID())
		bucket.Insert(newContact)
		return true
	}

	// If LRU is questionable, it should be pinged by maintenance routine. For now, reject the
	// new contact.
	return false
}

func (rt *RoutingTable) Remove(id [sha1.Size]byte) bool {
	bucketIdx := BucketIndex(rt.localID, id)
	return rt.buckets[bucketIdx].Remove(id)
}

func (rt *RoutingTable) Get(id [sha1.Size]byte) *Contact {
	bucketIdx := BucketIndex(rt.localID, id)
	return rt.buckets[bucketIdx].Get(id)
}

func (rt *RoutingTable) FindClosestK(target [sha1.Size]byte, k int) []*Contact {
	rt.mut.Lock()
	defer rt.mut.Unlock()

	targetBucket := BucketIndex(rt.localID, target)

	var contacts []*Contact
	contacts = append(contacts, rt.buckets[targetBucket].All()...)

	for i := 1; len(contacts) < k && (targetBucket-i >= 0 || targetBucket+1 < BucketSize); i++ {
		if targetBucket-i >= 0 {
			contacts = append(contacts, rt.buckets[targetBucket-i].All()...)
		}

		if len(contacts) >= k {
			break
		}

		if targetBucket+1 < BucketSize {
			contacts = append(contacts, rt.buckets[targetBucket+i].All()...)
		}
	}

	sort.Slice(contacts, func(i, j int) bool {
		return CompareDistance(target, contacts[i].ID(), contacts[j].ID()) < 0
	})

	if len(contacts) > k {
		contacts = contacts[:k]
	}

	return contacts
}

func (rt *RoutingTable) Size() int {
	rt.mut.Lock()
	defer rt.mut.Unlock()

	count := 0
	for _, bucket := range rt.buckets {
		count += bucket.Len()
	}

	return count
}

func (rt *RoutingTable) GetBucketsNeedingRefresh() []int {
	rt.mut.RLock()
	defer rt.mut.RUnlock()

	var indices []int
	for i, bucket := range rt.buckets {
		if bucket.Len() > 0 && bucket.NeedsRefresh() {
			indices = append(indices, i)
		}
	}

	return indices
}

func (rt *RoutingTable) GetQuestionableContacts() []*Contact {
	rt.mut.RLock()
	defer rt.mut.RUnlock()

	var questionable []*Contact
	for _, bucket := range rt.buckets {
		for _, contact := range bucket.All() {
			if contact.IsQuestionable() {
				questionable = append(questionable, contact)
			}
		}
	}

	return questionable
}

type RoutingTableStats struct {
	TotalContacts        int
	GoodContacts         int
	QuestionableContacts int
	BadContacts          int
	FilledBuckets        int
	EmptyBuckets         int
}

func (rt *RoutingTable) GetStats() RoutingTableStats {
	rt.mut.RLock()
	defer rt.mut.RUnlock()

	stats := RoutingTableStats{}

	for _, bucket := range rt.buckets {
		contacts := bucket.All()
		if len(contacts) == 0 {
			stats.EmptyBuckets++
			continue
		}

		stats.FilledBuckets++
		stats.TotalContacts += len(contacts)

		for _, c := range contacts {
			if c.IsGood() {
				stats.GoodContacts++
			} else if c.IsQuestionable() {
				stats.QuestionableContacts++
			} else if c.IsBad() {
				stats.BadContacts++
			}
		}
	}

	return stats
}

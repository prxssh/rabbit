package dht

import (
	"crypto/sha1"
	"net"
	"sync"
	"time"
)

type ContactState int

const (
	StateGood         ContactState = iota // Responded in last 15m
	StateQuestionable                     // No response but not timed out
	StateBad                              // Failed multiple tims
)

type Contact struct {
	node          *Node
	lastSeen      time.Time
	lastQuery     time.Time
	failedQueries int
	state         ContactState

	mut     sync.RWMutex
	pending map[string]time.Time // Transaction ID -> sent time
}

func NewContact(node *Node) *Contact {
	return &Contact{
		node:     node,
		lastSeen: time.Now(),
		state:    StateQuestionable,
		pending:  make(map[string]time.Time),
	}
}

func (c *Contact) ID() [sha1.Size]byte {
	return c.node.ID
}

func (c *Contact) Addr() *net.UDPAddr {
	return c.node.UDPAddr()
}

// MarkSeen updates contact as having responded successfully.
func (c *Contact) MarkSeen() {
	c.mut.Lock()
	defer c.mut.Unlock()

	c.lastSeen = time.Now()
	c.failedQueries = 0
	c.state = StateGood
}

// MarkQueried records that we sent a query to this contact
func (c *Contact) MarkQueried(transactionID string) {
	c.mut.Lock()
	defer c.mut.Unlock()

	c.lastQuery = time.Now()
	c.pending[transactionID] = time.Now()
}

func (c *Contact) MarkResponse(transactionID string) {
	c.mut.Lock()
	defer c.mut.Unlock()

	delete(c.pending, transactionID)
}

func (c *Contact) MarkFailed() {
	c.mut.Lock()
	defer c.mut.Unlock()

	c.failedQueries++

	if c.failedQueries >= 3 {
		c.state = StateBad
	} else {
		c.state = StateQuestionable
	}
}

func (c *Contact) IsGood() bool {
	c.mut.RLock()
	defer c.mut.RUnlock()

	return c.state == StateGood && time.Since(c.lastSeen) < 15*time.Minute
}

func (c *Contact) IsQuestionable() bool {
	c.mut.RLock()
	defer c.mut.RUnlock()

	if c.state == StateBad {
		return false
	}
	return time.Since(c.lastSeen) >= 15*time.Minute
}

func (c *Contact) IsBad() bool {
	c.mut.RLock()
	defer c.mut.RUnlock()

	return c.state == StateBad
}

func (c *Contact) PendingQueries() int {
	c.mut.RLock()
	defer c.mut.RUnlock()

	return len(c.pending)
}

func (c *Contact) CleanStaleQueries(timeout time.Duration) {
	c.mut.Lock()
	defer c.mut.Unlock()

	now := time.Now()
	for txID, sentAt := range c.pending {
		if now.Sub(sentAt) > timeout {
			delete(c.pending, txID)
			c.failedQueries++
		}
	}
}

func (b *Bucket) All() []*Contact {
	b.mut.RLock()
	defer b.mut.RUnlock()

	result := make([]*Contact, len(b.contacts))
	copy(result, b.contacts)
	return result
}

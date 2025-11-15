package dht

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

var (
	ErrNotStarted = errors.New("DHT not started")
	ErrStopped    = errors.New("DHT stopped")
)

type DHT struct {
	config *Config

	localID [sha1.Size]byte
	table   *RoutingTable
	krpc    *KRPC
	storage *Storage
	token   *TokenManager
	handler *QueryHandler

	started bool
	mu      sync.RWMutex
	done    chan struct{}
	wg      sync.WaitGroup
}

type Config struct {
	Logger         *slog.Logger
	LocalID        [sha1.Size]byte
	ListenAddr     string
	BootstrapNodes []string // "ip:port" format
}

func NewDHT(config *Config) (*DHT, error) {
	krpc, err := NewKRPC(config.LocalID, config.ListenAddr, config.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create KRPC: %w", err)
	}

	table := NewRoutingTable(config.LocalID)
	storage := NewStorage()
	token := NewTokenManager()

	dht := &DHT{
		config:  config,
		localID: config.LocalID,
		table:   table,
		krpc:    krpc,
		storage: storage,
		token:   token,
		done:    make(chan struct{}),
	}

	dht.handler = NewQueryHandler(krpc, table, storage, token)
	krpc.SetQueryHandler(dht.handler.HandleQuery)

	return dht, nil
}

func (d *DHT) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return errors.New("already started")
	}

	d.krpc.Start()

	d.wg.Add(3)
	go d.bootstrapLoop()
	go d.refreshLoop()
	go d.pingLoop()

	d.started = true
	return nil
}

func (d *DHT) Stop() {
	d.mu.Lock()
	if !d.started {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	close(d.done)
	d.krpc.Stop()
	d.wg.Wait()

	d.mu.Lock()
	d.started = false
	d.mu.Unlock()
}

func (d *DHT) GetPeers(infoHash [sha1.Size]byte) ([]net.Addr, error) {
	if !d.isStarted() {
		return nil, ErrNotStarted
	}

	lookup := NewLookup(d, infoHash, LookupTypePeers)
	result := lookup.Run()

	if result.Err != nil {
		return nil, result.Err
	}

	return result.Peers, nil
}

// AnnouncePeer announces that we are downloading/seeding a torrent.
func (d *DHT) AnnouncePeer(infoHash [sha1.Size]byte, port int) error {
	if !d.isStarted() {
		return ErrNotStarted
	}

	// First get peers to obtain tokens
	lookup := NewLookup(d, infoHash, LookupTypePeers)
	result := lookup.Run()

	if result.Err != nil {
		return result.Err
	}

	// Announce to closest nodes that returned tokens
	var wg sync.WaitGroup
	for _, node := range result.ClosestNodes {
		if node.Token == "" {
			continue
		}

		wg.Add(1)
		go func(n *LookupNode) {
			defer wg.Done()
			d.announce(n.Contact, infoHash, port, n.Token)
		}(node)
	}

	wg.Wait()
	return nil
}

// announce sends announce_peer to a single node.
func (d *DHT) announce(contact *Contact, infoHash [sha1.Size]byte, port int, token string) {
	msg := AnnouncePeerQuery(d.krpc.generateTransactionID(), d.localID, infoHash, port, token)

	timeout := 15 * time.Second
	d.krpc.SendQuery(msg, contact.Addr(), timeout)
}

// Ping sends a ping to a node and updates routing table.
func (d *DHT) Ping(addr *net.UDPAddr) error {
	if !d.isStarted() {
		return ErrNotStarted
	}

	msg := PingQuery(d.krpc.generateTransactionID(), d.localID)

	timeout := 15 * time.Second
	response, err := d.krpc.SendQuery(msg, addr, timeout)
	if err != nil {
		return err
	}

	// Extract node ID and update routing table
	nodeID, ok := response.GetNodeID()
	if !ok {
		return ErrInvalidMsg
	}

	contact := NewContact(&Node{
		ID:   nodeID,
		IP:   addr.IP,
		Port: int16(addr.Port),
	})
	contact.MarkSeen()
	d.table.Insert(contact)

	return nil
}

// FindNode performs iterative lookup to find nodes close to target.
func (d *DHT) FindNode(target [sha1.Size]byte) ([]*Contact, error) {
	if !d.isStarted() {
		return nil, ErrNotStarted
	}

	lookup := NewLookup(d, target, LookupTypeNodes)
	result := lookup.Run()

	if result.Err != nil {
		return nil, result.Err
	}

	contacts := make([]*Contact, len(result.ClosestNodes))
	for i, node := range result.ClosestNodes {
		contacts[i] = node.Contact
	}

	return contacts, nil
}

// bootstrapLoop performs initial bootstrap.
func (d *DHT) bootstrapLoop() {
	defer d.wg.Done()

	// Bootstrap immediately on start
	d.bootstrap()

	// Re-bootstrap every hour
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			d.bootstrap()
		}
	}
}

// bootstrap contacts bootstrap nodes and performs self-lookup.
func (d *DHT) bootstrap() {
	// Ping bootstrap nodes
	for _, addrStr := range d.config.BootstrapNodes {
		addr, err := net.ResolveUDPAddr("udp", addrStr)
		if err != nil {
			continue
		}

		d.Ping(addr)
	}

	// Wait for some responses
	time.Sleep(2 * time.Second)

	// Perform lookup for our own ID to populate routing table
	d.FindNode(d.localID)
}

// refreshLoop refreshes stale buckets.
func (d *DHT) refreshLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			d.refresh()
		}
	}
}

// refresh finds and refreshes stale buckets.
func (d *DHT) refresh() {
	buckets := d.table.GetBucketsNeedingRefresh()

	for _, bucketIdx := range buckets {
		// Generate random ID in bucket range
		target := d.randomIDInBucket(bucketIdx)

		// Perform lookup
		d.FindNode(target)
	}
}

// pingLoop pings questionable contacts.
func (d *DHT) pingLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			d.pingQuestionable()
		}
	}
}

// pingQuestionable pings questionable contacts to verify liveness.
func (d *DHT) pingQuestionable() {
	contacts := d.table.GetQuestionableContacts()

	for _, contact := range contacts {
		msg := PingQuery(d.krpc.generateTransactionID(), d.localID)

		timeout := 15 * time.Second
		response, err := d.krpc.SendQuery(msg, contact.Addr(), timeout)
		if err != nil {
			contact.MarkFailed()
			if contact.IsBad() {
				d.table.Remove(contact.ID())
			}
			continue
		}

		// Verify node ID matches
		nodeID, ok := response.GetNodeID()
		if !ok || nodeID != contact.ID() {
			d.table.Remove(contact.ID())
			continue
		}

		contact.MarkSeen()
	}
}

// randomIDInBucket generates a random node ID within a bucket's range.
func (d *DHT) randomIDInBucket(bucketIdx int) [sha1.Size]byte {
	// Simple implementation: XOR local ID with random bits
	// positioned at the bucket index
	var id [sha1.Size]byte
	copy(id[:], d.localID[:])

	// Flip bit at position (159 - bucketIdx)
	bitPos := 159 - bucketIdx
	byteIdx := bitPos / 8
	bitIdx := byte(bitPos % 8)

	id[byteIdx] ^= (1 << (7 - bitIdx))

	return id
}

// isStarted checks if DHT is running.
func (d *DHT) isStarted() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.started
}

// Stats returns current DHT statistics.
func (d *DHT) Stats() RoutingTableStats {
	return d.table.GetStats()
}

// LocalAddr returns the local UDP address.
func (d *DHT) LocalAddr() *net.UDPAddr {
	return d.krpc.LocalAddr()
}

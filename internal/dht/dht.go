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

func WithDefaultConfig() *Config {
	return &Config{
		ListenAddr:     "0.0.0.0:6881",
		BootstrapNodes: DefaultBootstrapNodes,
	}
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

	d.started = false
	d.mu.Unlock()

	close(d.done)
	d.krpc.Stop()
	d.wg.Wait()
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

func (d *DHT) AnnouncePeer(infoHash [sha1.Size]byte, port int) error {
	if !d.isStarted() {
		return ErrNotStarted
	}

	lookup := NewLookup(d, infoHash, LookupTypePeers)
	result := lookup.Run()

	if result.Err != nil {
		return result.Err
	}

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

func (d *DHT) announce(contact *Contact, infoHash [sha1.Size]byte, port int, token string) {
	msg := AnnouncePeerQuery(d.krpc.generateTransactionID(), d.localID, infoHash, port, token)

	timeout := 15 * time.Second
	d.krpc.SendQuery(msg, contact.Addr(), timeout)
}

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

	nodeID, ok := response.GetNodeID()
	if !ok {
		return ErrInvalidMsg
	}

	contact := NewContact(&Node{
		ID:   nodeID,
		IP:   addr.IP,
		Port: addr.Port,
	})
	contact.MarkSeen()
	d.table.Insert(contact)

	return nil
}

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

func (d *DHT) bootstrapLoop() {
	defer d.wg.Done()

	d.bootstrap()

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

func (d *DHT) bootstrap() {
	d.config.Logger.Info("Starting DHT bootstrap", "nodes", len(d.config.BootstrapNodes))

	successCount := 0
	for _, addrStr := range d.config.BootstrapNodes {
		addr, err := net.ResolveUDPAddr("udp", addrStr)
		if err != nil {
			d.config.Logger.Warn("Failed to resolve bootstrap node", "addr", addrStr, "error", err)
			continue
		}

		err = d.Ping(addr)
		if err == nil {
			successCount++
		}
	}

	d.config.Logger.Info("Bootstrap pings sent", "successful", successCount, "total", len(d.config.BootstrapNodes))

	time.Sleep(2 * time.Second)

	stats := d.table.GetStats()
	d.config.Logger.Info("Routing table after bootstrap",
		"total_contacts", stats.TotalContacts,
		"good", stats.GoodContacts,
		"filled_buckets", stats.FilledBuckets)

	_, err := d.FindNode(d.localID)
	if err != nil {
		d.config.Logger.Warn("Self-lookup failed", "error", err)
	} else {
		d.config.Logger.Info("Self-lookup completed")
	}
}

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

func (d *DHT) refresh() {
	buckets := d.table.GetBucketsNeedingRefresh()

	for _, bucketIdx := range buckets {
		target := d.randomIDInBucket(bucketIdx)
		d.FindNode(target)
	}
}

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

		nodeID, ok := response.GetNodeID()
		if !ok || nodeID != contact.ID() {
			d.table.Remove(contact.ID())
			continue
		}

		contact.MarkSeen()
	}
}

func (d *DHT) randomIDInBucket(bucketIdx int) [sha1.Size]byte {
	var id [sha1.Size]byte
	copy(id[:], d.localID[:])

	bitPos := 159 - bucketIdx
	byteIdx := bitPos / 8
	bitIdx := byte(bitPos % 8)

	id[byteIdx] ^= (1 << (7 - bitIdx))

	return id
}

func (d *DHT) isStarted() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.started
}

func (d *DHT) Stats() RoutingTableStats {
	return d.table.GetStats()
}

func (d *DHT) LocalAddr() *net.UDPAddr {
	return d.krpc.LocalAddr()
}

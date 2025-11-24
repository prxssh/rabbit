package dht

import (
	"container/heap"
	"crypto/sha1"
	"errors"
	"net"
	"sync"
	"time"
)

type LookupType int

const (
	LookupTypeNodes LookupType = iota // find_node lookup
	LookupTypePeers                   // get_peers lookup
)

const (
	Alpha         = 3 // Concurrency factor (parallel queries)
	LookupK       = 8 // Number of closest nodes to find
	LookupTimeout = 30 * time.Second
	QueryTimeout  = 15 * time.Second
)

type Lookup struct {
	dht        *DHT
	target     [sha1.Size]byte
	lookupType LookupType

	closest   *nodeHeap
	contacted map[[sha1.Size]byte]bool
	pending   map[string]*LookupNode
	peers     []net.Addr

	mu         sync.Mutex
	done       chan struct{}
	queryCh    chan *LookupNode
	responseCh chan *LookupResponse
}

type LookupNode struct {
	Contact *Contact
	Token   string // For get_peers responses
	Queried bool
}

type LookupResponse struct {
	Node  *LookupNode
	Nodes []*Contact
	Peers []net.Addr
	Token string
	Err   error
}

type LookupResult struct {
	ClosestNodes []*LookupNode
	Peers        []net.Addr
	Err          error
}

func NewLookup(dht *DHT, target [sha1.Size]byte, lookupType LookupType) *Lookup {
	return &Lookup{
		dht:        dht,
		target:     target,
		lookupType: lookupType,
		closest:    newNodeHeap(target),
		contacted:  make(map[[sha1.Size]byte]bool),
		pending:    make(map[string]*LookupNode),
		done:       make(chan struct{}),
		queryCh:    make(chan *LookupNode, Alpha),
		responseCh: make(chan *LookupResponse, Alpha),
	}
}

func (l *Lookup) Run() *LookupResult {
	seeds := l.dht.table.FindClosestK(l.target, LookupK)
	for _, contact := range seeds {
		l.addNode(&LookupNode{Contact: contact})
	}

	if len(seeds) == 0 {
		return &LookupResult{Err: errors.New("no nodes in routing table")}
	}

	l.dht.config.Logger.Debug("Starting lookup", "type", l.lookupType, "seeds", len(seeds))

	var wg sync.WaitGroup
	for i := 0; i < Alpha; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.queryWorker()
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		l.responseHandler()
	}()

	timeout := time.After(LookupTimeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			close(l.done)
			wg.Wait()
			l.dht.config.Logger.Warn("Lookup timeout", "type", l.lookupType, "contacted", len(l.contacted), "closest", l.closest.Len())
			return l.buildResult(errors.New("lookup timeout"))

		case <-ticker.C:
			if l.isComplete() {
				close(l.done)
				wg.Wait()
				l.dht.config.Logger.Debug("Lookup complete", "type", l.lookupType, "contacted", len(l.contacted), "peers", len(l.peers))
				return l.buildResult(nil)
			}

			l.scheduleQueries()
		}
	}
}

func (l *Lookup) queryWorker() {
	for {
		select {
		case <-l.done:
			return
		case node := <-l.queryCh:
			l.sendQuery(node)
		}
	}
}

func (l *Lookup) sendQuery(node *LookupNode) {
	var msg *Message
	txID := l.dht.krpc.generateTransactionID()

	switch l.lookupType {
	case LookupTypeNodes:
		msg = FindNodeQuery(txID, l.dht.localID, l.target)
	case LookupTypePeers:
		msg = GetPeersQuery(txID, l.dht.localID, l.target)
	}

	l.mu.Lock()
	node.Queried = true
	l.pending[txID] = node
	node.Contact.MarkQueried(txID)
	l.mu.Unlock()

	response, err := l.dht.krpc.SendQuery(msg, node.Contact.Addr(), QueryTimeout)

	result := &LookupResponse{
		Node: node,
		Err:  err,
	}

	if err == nil {
		l.parseResponse(response, result)
	}

	select {
	case l.responseCh <- result:
	case <-l.done:
	}
}

func (l *Lookup) parseResponse(msg *Message, result *LookupResponse) {
	nodeID, ok := msg.GetNodeID()
	if !ok || nodeID != result.Node.Contact.ID() {
		result.Err = errors.New("node ID mismatch")
		return
	}

	if token, ok := msg.GetToken(); ok {
		result.Token = token
	}

	if values, ok := msg.GetValues(); ok {
		for _, value := range values {
			if len(value) == 6 {
				var peerInfo [6]byte
				copy(peerInfo[:], value)
				ip, port := DecodePeerInfo(peerInfo)
				result.Peers = append(result.Peers, &net.UDPAddr{IP: ip, Port: int(port)})
			}
		}
	}

	if nodesData, ok := msg.GetNodes(); ok {
		nodes := DecodeCompactNodeInfoList(nodesData)
		for _, node := range nodes {
			result.Nodes = append(result.Nodes, NewContact(node))
		}
	}
}

func (l *Lookup) responseHandler() {
	for {
		select {
		case <-l.done:
			return
		case response := <-l.responseCh:
			l.handleResponse(response)
		}
	}
}

func (l *Lookup) handleResponse(response *LookupResponse) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for txID, node := range l.pending {
		if node == response.Node {
			delete(l.pending, txID)
			break
		}
	}

	if response.Err != nil {
		response.Node.Contact.MarkFailed()
		return
	}

	response.Node.Contact.MarkSeen()
	response.Node.Token = response.Token
	l.peers = append(l.peers, response.Peers...)

	for _, contact := range response.Nodes {
		l.addNode(&LookupNode{Contact: contact})
	}
}

func (l *Lookup) addNode(node *LookupNode) {
	if l.contacted[node.Contact.ID()] {
		return
	}

	if node.Contact.ID() == l.dht.localID {
		return
	}

	heap.Push(l.closest, node)

	if l.closest.Len() > LookupK*2 {
		heap.Pop(l.closest)
	}
}

func (l *Lookup) scheduleQueries() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.pending) >= Alpha {
		return
	}

	scheduled := 0
	for i := 0; i < l.closest.Len() && scheduled < Alpha-len(l.pending); i++ {
		node := l.closest.nodes[i]

		if !node.Queried && !l.contacted[node.Contact.ID()] {
			l.contacted[node.Contact.ID()] = true

			select {
			case l.queryCh <- node:
				scheduled++
			case <-l.done:
				return
			}
		}
	}
}

func (l *Lookup) isComplete() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.pending) > 0 {
		return false
	}

	queriedClosest := 0
	for i := 0; i < l.closest.Len() && i < LookupK; i++ {
		if l.closest.nodes[i].Queried {
			queriedClosest++
		}
	}

	return queriedClosest >= LookupK || queriedClosest >= l.closest.Len()
}

func (l *Lookup) buildResult(err error) *LookupResult {
	l.mu.Lock()
	defer l.mu.Unlock()

	closestCount := LookupK
	if l.closest.Len() < closestCount {
		closestCount = l.closest.Len()
	}

	closest := make([]*LookupNode, closestCount)
	for i := 0; i < closestCount; i++ {
		closest[i] = l.closest.nodes[i]
	}

	return &LookupResult{
		ClosestNodes: closest,
		Peers:        l.peers,
		Err:          err,
	}
}

// nodeHeap is a min-heap of nodes sorted by distance to target.
type nodeHeap struct {
	target [sha1.Size]byte
	nodes  []*LookupNode
}

func newNodeHeap(target [sha1.Size]byte) *nodeHeap {
	h := &nodeHeap{
		target: target,
		nodes:  make([]*LookupNode, 0),
	}
	heap.Init(h)
	return h
}

func (h *nodeHeap) Len() int {
	return len(h.nodes)
}

func (h *nodeHeap) Less(i, j int) bool {
	// Closer nodes come first (min-heap)
	return CompareDistance(h.target, h.nodes[i].Contact.ID(), h.nodes[j].Contact.ID()) < 0
}

func (h *nodeHeap) Swap(i, j int) {
	h.nodes[i], h.nodes[j] = h.nodes[j], h.nodes[i]
}

func (h *nodeHeap) Push(x interface{}) {
	h.nodes = append(h.nodes, x.(*LookupNode))
}

func (h *nodeHeap) Pop() interface{} {
	old := h.nodes
	n := len(old)
	x := old[n-1]
	h.nodes = old[0 : n-1]
	return x
}

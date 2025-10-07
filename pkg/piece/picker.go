package piece

import (
	"crypto/sha1"
	"math/bits"
	"math/rand"
	"net/netip"
	"sync"
	"time"

	"github.com/prxssh/rabbit/pkg/config"
	"github.com/prxssh/rabbit/pkg/utils/bitfield"
)

// Cancel represents a block request that should be cancelled.
type Cancel struct {
	Peer  netip.AddrPort // candidate peer
	Piece int            // piece index
	Begin int            // block offset
}

// PeerView is a read-only snapshot of what the picker needs to decide whether
// THIS peer can fetch something right now.
//
// It must reflect the peer’s current choke state, bitfield, and remaining
// per-peer pipeline capacity (if you want the peer to limit itself). If you
// centralize capacity in the picker, Capacity can be ignored by NextForPeer.
type PeerView struct {
	Peer     netip.AddrPort    // the candidate peer
	Has      bitfield.Bitfield // peer's current bitfield
	Unchoked bool              // must be true to issue requests
}

// Request is a concrete plan for a single block request the writer can send.
//
// It’s returned by the picker, which also marks the chosen block inflight.
type Request struct {
	Peer   netip.AddrPort // who this was assigned to
	Piece  int            // piece index
	Begin  int            // byte offset inside the piece
	Length int            // block length
}

// PieceState represents the download state of a piece
type PieceState int

const (
	PieceStateNotStarted PieceState = 0
	PieceStateInProgress PieceState = 1
	PieceStateCompleted  PieceState = 2
)

// PieceStates returns a slice of piece states indicating the download status.
// The slice index corresponds to the piece index.
func (pk *Picker) PieceStates() []PieceState {
	pk.mu.RLock()
	defer pk.mu.RUnlock()

	states := make([]PieceState, pk.PieceCount)
	for i, p := range pk.pieces {
		if p.verified {
			states[i] = PieceStateCompleted
		} else if p.doneBlocks > 0 {
			states[i] = PieceStateInProgress
		} else {
			states[i] = PieceStateNotStarted
		}
	}
	return states
}

// AvailabilityBucket efficiently tracks which pieces belong to each
// availability level (i.e., how many peers currently have that piece).
//
// It maintains O(1) updates when peers join/leave by moving piece indices
// between small dense arrays ("buckets") and records each piece's current
// bucket position for constant-time removals.
//
// The structure is highly cache-friendly and supports fast rarest-first
// selection via a compact bitmap of non-empty buckets.
type availabilityBucket struct {
	// buckets[a] holds a dense slice of piece indices whose availability
	// equals 'a'. For example, buckets[3] contains all pieces that exactly
	// 3 peers currently have.
	//
	// Buckets are always densely packed: when a piece moves, it is removed
	// via swap-with-last, ensuring O(1) deletion and preserving
	// compactness.
	buckets [][]int

	// avail[i] stores the current availability count for piece i. Values
	// range from 0..maxAvail inclusive.
	//
	// This acts as the authoritative record of each piece’s rarity and is
	// used to determine which bucket the piece belongs to.
	avail []uint16

	// pos[i] gives the index of piece i inside buckets[avail[i]].
	//
	// This allows constant-time swap-remove when a piece moves to a new
	// availability bucket.
	pos []int

	// maxAvail is the upper bound on availability, typically equal to
	// MaxPeers. It defines the maximum number of buckets.
	maxAvail int

	// nonEmptyBits is a bitmap representing which buckets currently contain
	// at least one piece. Bit k in word w corresponds to bucket index (w*64
	// + k).
	//
	// This lets the picker find the smallest non-empty bucket (the rarest
	// pieces) in O(1)–O(64) time without scanning every bucket.
	nonEmptyBits []uint64
}

func newAvailabilityBucket(pieceCount, maxAvail int) *availabilityBucket {
	b := &availabilityBucket{
		maxAvail: maxAvail,
		buckets:  make([][]int, maxAvail+1),
		avail:    make([]uint16, pieceCount),
		pos:      make([]int, pieceCount),
		// enough words to covert maxAvail + 1 buckets.
		nonEmptyBits: make([]uint64, ((maxAvail + 63) / 64)),
	}

	capacity := max(1, pieceCount/(maxAvail+1))
	for a := range b.buckets {
		b.buckets[a] = make([]int, 0, capacity)
	}

	// Initially every piece is in availability==0
	b.buckets[0] = make([]int, pieceCount)
	for i := 0; i < pieceCount; i++ {
		b.buckets[0][i] = i
		b.pos[i] = i
		b.avail[i] = 0
	}
	b.setBit(0)

	return b
}

// Move updates piece i by delta (+1 or -1 typically) and keeps buckets
// consistent.
//
// It randomizes tie-break by swapping the newly appended element with a random
// slot. rng must be non-nil.
func (b *availabilityBucket) Move(i, delta int, rng *rand.Rand) {
	oldAvail := int(b.avail[i])

	newAvail := oldAvail + delta
	if newAvail < 0 {
		newAvail = 0
	} else if newAvail > b.maxAvail {
		newAvail = b.maxAvail
	}

	if newAvail == oldAvail {
		return
	}

	// remove from old bucket
	ob := b.buckets[oldAvail]
	p := b.pos[i]
	last := len(ob) - 1
	ob[p] = ob[last]
	b.pos[ob[p]] = p
	ob = ob[:last]
	b.buckets[oldAvail] = ob
	if len(ob) == 0 {
		b.clearBit(oldAvail)
	}

	// insert into new bucket
	nb := b.buckets[newAvail]
	nb = append(nb, i)
	ni := len(nb) - 1
	if ni > 0 {
		j := rng.Intn(ni + 1) // [0..ni]
		nb[ni], nb[j] = nb[j], nb[ni]
		b.pos[nb[ni]] = ni
		b.pos[nb[j]] = j
	} else {
		b.pos[i] = 0
	}

	b.buckets[newAvail] = nb
	b.setBit(newAvail)

	b.avail[i] = uint16(newAvail)
}

// FirstNonEmpty returns the smallest availability a that has at least one
// piece.
func (b *availabilityBucket) FirstNonEmpty() (a int, ok bool) {
	for w := 0; w < len(b.nonEmptyBits); w++ {
		if x := b.nonEmptyBits[w]; x != 0 {
			off := bits.TrailingZeros64(x)
			return (w<<6 + off), true
		}
	}

	return 0, false
}

// Bucket returns the slice of piece indices for availability a (read-only use).
func (b *availabilityBucket) Bucket(a int) []int {
	if a < 0 || a > b.maxAvail {
		return nil
	}

	return b.buckets[a]
}

func (b *availabilityBucket) setBit(a int) {
	w, bit := a>>6, uint(a&63)
	b.nonEmptyBits[w] |= 1 << bit
}

func (b *availabilityBucket) clearBit(a int) {
	w, bit := a>>6, uint(a&63)
	if len(b.buckets[a]) == 0 {
		b.nonEmptyBits[w] &^= 1 << bit
	}
}

// Picker is the global download planner/state holder for a single torrent.
type Picker struct {
	// BlockLength mirrors BlockSize for clarity; kept as a field in case
	// you want to support different block sizes (e.g., for testing).
	BlockLength int

	// LastPieceLen caches the exact byte length of the final piece (handy
	// when emitting block sizes for that piece).
	LastPieceLen int

	// PieceCount = len(Pieces).
	PieceCount int

	// pieces is the complete set of per-piece states, indexed by piece
	// index.
	pieces []*pieceState

	// availability maintains a compact, O(1)-updatable mapping from
	// "availability count → pieces" for implementing rarest-first
	// selection.
	availability *availabilityBucket

	// nextPiece and nextBlock act as cursors for StrategySequential. They
	// can be ignored by other strategies that compute candidates
	// differently.
	nextPiece int
	nextBlock int

	// wanted is an optional "selective download" filter. If non-nil, only
	// pieces with Wanted[index]==true are eligible. If nil, all pieces are
	// eligible.
	wanted map[int]bool

	// endgame toggles duplication of remaining not-done blocks. When
	// enabled, you should allow multiple Owners per block up to
	// Cfg.EndgameDupPerBlock and cancel losers as soon as the first copy
	// arrives.
	endgame bool

	// remainingBlocks is a global counter of blocks whose State !=
	// BlockDone. Once it drops below a small threshold (e.g., 32), you
	// typically enable Endgame. When it reaches 0, the torrent is
	// byte-complete.
	remainingBlocks int

	// rng is used for StrategyRandomFirst and tie-breaking when multiple
	// pieces have identical rank (e.g., same rarity and priority).
	rng *rand.Rand
	mu  sync.RWMutex

	// peerBlockAssignments is a reverse index mapping each peer to the set
	// of blocks they are currently assigned to fetch. Allows for fast
	// cleanup when a peer disconnects.
	peerBlockAssignments map[netip.AddrPort]map[uint64]struct{}

	// peerInflightCount tracks the number of outstanding (inflight) block
	// requests per peer. Enforces Config.MaxInflightRequests limit. When a
	// peer reaches this limit, NextForPeer() returns nil until some
	// requests complete or timeout. This prevents any single peer from
	// monopolizing the download pipeline.
	peerInflightCount map[netip.AddrPort]int

	// bitfield caches which pieces are verified. Updated whenever
	// MarkPieceVerified is called.
	bitfield bitfield.Bitfield

	// inflightRequests keeps track of the number of pieces that we're
	// currently downloading. This is helpful in implementing backpressure.
	inflightRequests int
}

func NewPicker(
	torrentSize, pieceLength int64,
	pieceHashes [][sha1.Size]byte,
) *Picker {
	cfg := config.Load()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	n := len(pieceHashes)

	availability := newAvailabilityBucket(n, cfg.MaxPeers)

	totalBlocks := 0
	lastPieceLen := LastPieceLength(torrentSize, pieceLength)
	pieces := make([]*pieceState, n)

	for i := 0; i < n; i++ {
		plen, _ := PieceLengthAt(i, torrentSize, pieceLength)
		blockCount := BlocksInPiece(plen)
		totalBlocks += blockCount
		blocks := make([]*block, blockCount)

		for j := 0; j < blockCount; j++ {
			blocks[j] = &block{
				status: blockWant,
				owners: make(map[netip.AddrPort]*ownerMeta),
			}
		}

		pieces[i] = &pieceState{
			index:        i,
			availability: 0,
			priority:     0,
			doneBlocks:   0,
			length:       plen,
			verified:     false,
			blocks:       blocks,
			isLastPiece:  i == n-1,
			blockCount:   blockCount,
			sha:          pieceHashes[i],
			lastBlock:    LastBlockInPiece(plen),
		}
	}

	return &Picker{
		nextPiece:         0,
		nextBlock:         0,
		PieceCount:        n,
		wanted:            nil,
		rng:               rng,
		endgame:           false,
		pieces:            pieces,
		remainingBlocks:   totalBlocks,
		BlockLength:       BlockLength,
		availability:      availability,
		LastPieceLen:      lastPieceLen,
		bitfield:          bitfield.New(n),
		peerInflightCount: make(map[netip.AddrPort]int),
		peerBlockAssignments: make(
			map[netip.AddrPort]map[uint64]struct{},
		),
	}
}

func (pk *Picker) Bitfield() bitfield.Bitfield {
	pk.mu.RLock()
	defer pk.mu.RUnlock()

	return pk.bitfield
}

// OnPeerGone removes 'peer' from ownership of all blocks and updates
// availability based on its bitfield. Any blocks left with no owners are
// moved back to WANT so they can be reassigned quickly.
func (pk *Picker) OnPeerGone(peer netip.AddrPort, bf bitfield.Bitfield) {
	pk.mu.Lock()
	defer pk.mu.Unlock()

	for i := 0; i < pk.PieceCount; i++ {
		if !bf.Has(i) {
			continue
		}

		pk.updatePieceAvailability(i, -1)
	}

	keys := pk.peerBlockAssignments[peer]
	if len(keys) == 0 {
		delete(pk.peerBlockAssignments, peer)
		delete(pk.peerInflightCount, peer)

		return
	}

	for key := range keys {
		pieceIdx := int(uint32(key >> 32))
		blockIdx := int(uint32(key))
		if pieceIdx < 0 || pieceIdx >= pk.PieceCount {
			continue
		}

		ps := pk.pieces[pieceIdx]
		if blockIdx < 0 || blockIdx >= ps.blockCount {
			continue
		}
		delete(ps.blocks[blockIdx].owners, peer)

		block := ps.blocks[blockIdx]

		if block.status == blockInflight && len(block.owners) == 0 {
			block.status = blockWant
			if pieceIdx == pk.nextPiece && blockIdx < pk.nextBlock {
				pk.nextBlock = blockIdx
			}
		}
	}

	delete(pk.peerBlockAssignments, peer)
	delete(pk.peerInflightCount, peer)
}

// OnBlockReceived marks the block DONE, clears duplicate owners, and returns a
// list of cancellations to send for any duplicates (endgame). The bool result
// indicates whether the piece is now complete.
func (pk *Picker) OnBlockReceived(
	peer netip.AddrPort,
	pieceIdx, begin int,
) (bool, []Cancel) {
	pk.mu.Lock()
	defer pk.mu.Unlock()

	if pieceIdx < 0 || pieceIdx >= pk.PieceCount {
		return false, nil
	}

	ps := pk.pieces[pieceIdx]
	bi := BlockIndexForBegin(begin, ps.length, pk.BlockLength)
	if bi < 0 || bi >= ps.blockCount {
		return false, nil
	}

	var cancels []Cancel
	key := packKey(pieceIdx, bi)
	freedSelf := false
	owners := ps.blocks[bi].owners

	for owner := range ps.blocks[bi].owners {
		if owner != peer {
			cancels = append(cancels, Cancel{
				Peer:  owner,
				Piece: pieceIdx,
				Begin: begin,
			})
		} else {
			freedSelf = true
		}
		delete(pk.peerBlockAssignments[owner], key)

		pk.peerInflightCount[owner]--
		if pk.peerInflightCount[owner] < 0 {
			pk.peerInflightCount[owner] = 0
		}
	}

	if !freedSelf {
		delete(pk.peerBlockAssignments[peer], key)

		pk.peerInflightCount[peer]--
		if pk.peerInflightCount[peer] < 0 {
			pk.peerInflightCount[peer] = 0
		}
	}

	dec := len(owners)
	if !freedSelf {
		dec++
	}

	pk.inflightRequests -= dec
	if pk.inflightRequests < 0 {
		pk.inflightRequests = 0
	}

	ps.blocks[bi].owners = make(map[netip.AddrPort]*ownerMeta)
	ps.blocks[bi].pendingRequests = 0

	if ps.blocks[bi].status != blockDone {
		ps.blocks[bi].status = blockDone
		ps.doneBlocks++
		pk.remainingBlocks--
	}

	return ps.doneBlocks == ps.blockCount, cancels
}

func (pk *Picker) HasAnyWantedPiece(bf bitfield.Bitfield) bool {
	pk.mu.RLock()
	defer pk.mu.RUnlock()

	for i := 0; i < pk.PieceCount; i++ {
		ps := pk.pieces[i]
		if ps.verified {
			continue
		}
		if pk.wanted != nil && !pk.wanted[i] {
			continue
		}
		if !bf.Has(i) {
			continue
		}

		for b := 0; b < ps.blockCount; b++ {
			if ps.blocks[b].status == blockWant {
				return true
			}
		}
	}

	return false
}

// NextForPeer chooses up to one next block to request from this peer,
// respecting its unchoked state and per-peer pipeline limit. It also registers
// ownership (peer→block) when a request is issued.
func (pk *Picker) NextForPeer(pv *PeerView) []*Request {
	cfg := config.Load()

	if !pv.Unchoked {
		return nil
	}

	pk.mu.Lock()
	defer pk.mu.Unlock()

	perPeerLeft := cfg.MaxInflightRequests - pk.peerInflightCount[pv.Peer]
	if perPeerLeft <= 0 {
		return nil
	}

	globalLeft := cfg.MaxInflightRequests - pk.inflightRequests
	if globalLeft <= 0 {
		return nil
	}

	limit := perPeerLeft
	if globalLeft < limit {
		limit = globalLeft
	}
	if limit <= 0 {
		return nil
	}

	var reqs []*Request

	switch config.Load().PieceDownloadStrategy {
	case config.PieceDownloadStrategySequential:
		reqs = pk.selectSequentialPiecesToDownload(
			pv.Peer,
			pv.Has,
			limit,
		)
	case config.PieceDownloadStrategyRarestFirst:
		reqs = pk.selectRarestPiecesForDownload(pv.Peer, pv.Has, limit)
	default:
		reqs = pk.selectRandomFirstPiecesForDownload(
			pv.Peer,
			pv.Has,
			limit,
		)
	}

	pk.inflightRequests += len(reqs)
	return reqs
}

// OnTimeout reclaims a single block for a given peer if they owned it. If no
// owners remain after removal, the block returns to WANT.
func (pk *Picker) OnTimeout(peer netip.AddrPort, pieceIdx, begin int) {
	pk.mu.Lock()
	defer pk.mu.Unlock()

	if pieceIdx < 0 || pieceIdx >= pk.PieceCount {
		return
	}

	ps := pk.pieces[pieceIdx]
	bi := BlockIndexForBegin(begin, ps.length, pk.BlockLength)
	if bi < 0 || bi >= ps.blockCount {
		return
	}

	if _, had := ps.blocks[bi].owners[peer]; !had {
		return
	}
	delete(ps.blocks[bi].owners, peer)
	delete(pk.peerBlockAssignments[peer], packKey(pieceIdx, bi))
	pk.peerInflightCount[peer]--
	if pk.peerInflightCount[peer] < 0 {
		pk.peerInflightCount[peer] = 0
	}

	// If nobody else is fetching it, return to WANT.
	if ps.blocks[bi].status == blockInflight &&
		len(ps.blocks[bi].owners) == 0 {
		ps.blocks[bi].status = blockWant
		// Pull back sequential cursor to retry sooner.
		if pieceIdx == pk.nextPiece {
			if b := bi; b < pk.nextBlock {
				pk.nextBlock = b
			}
		}
	}
}

// OnPeerBitfield updates piece availability counts based on a newly received
// full bitfield from 'peer'. Availability increases for every piece the peer
// has.
func (pk *Picker) OnPeerBitfield(peer netip.AddrPort, bf bitfield.Bitfield) {
	pk.mu.Lock()
	defer pk.mu.Unlock()

	for i := 0; i < pk.PieceCount; i++ {
		if bf.Has(i) {
			pk.updatePieceAvailability(i, 1)
		}
	}
}

// OnPeerHave updates availability for a single piece announced by 'peer' via a
// HAVE message. Used for incremental availability updates after handshake.
func (pk *Picker) OnPeerHave(peer netip.AddrPort, pieceIdx int) {
	pk.mu.Lock()
	defer pk.mu.Unlock()

	pk.updatePieceAvailability(pieceIdx, 1)
}

// updatePieceAvailability applies delta to piece idx and moves it between
// buckets.
func (pk *Picker) updatePieceAvailability(idx, delta int) {
	if idx < 0 || idx >= pk.PieceCount {
		return
	}

	pk.availability.Move(idx, delta, pk.rng)
	pk.pieces[idx].availability = int(pk.availability.avail[idx])
}

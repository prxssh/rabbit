package piece

import (
	"crypto/sha1"
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

	// availabilityBuckets maps availability count to pieces with that
	// availability. availabilityBuckets[3] = all pieces that exactly have 3
	// peers. When availability changes form 5->6 just move pieces from
	// availabilityBuckets[5] to availabilityBuckets[6].
	availabilityBuckets map[int]map[int]struct{}

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

	// piecesInTransit keeps track of the number of pieces that we're
	// currently downloading. This is helpful in implementing backpressure.
	piecesInTransit int
}

func NewPicker(
	torrentSize, pieceLength int64,
	pieceHashes [][sha1.Size]byte,
) *Picker {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	n, totalBlocks := len(pieceHashes), 0
	pieces := make([]*pieceState, n)
	lastPieceLen := LastPieceLength(torrentSize, pieceLength)

	for i := 0; i < n; i++ {
		plen, _ := PieceLengthAt(i, torrentSize, pieceLength)
		blockCount := BlocksInPiece(plen)
		totalBlocks += blockCount
		last := LastBlockInPiece(plen)

		blocks := make([]*block, blockCount)

		for i := 0; i < blockCount; i++ {
			blocks[i] = &block{
				owners: make(map[netip.AddrPort]*ownerMeta),
			}
		}

		pieces[i] = &pieceState{
			index:       i,
			lastBlock:   last,
			verified:    false,
			blockCount:  blockCount,
			isLastPiece: i == n-1,
			length:      int(plen),
			sha:         pieceHashes[i],
			blocks:      blocks,
		}
	}

	peerBlockAssignments := make(map[netip.AddrPort]map[uint64]struct{})

	availabilityBuckets := make(map[int]map[int]struct{})
	availabilityBuckets[0] = make(map[int]struct{})
	for i := 0; i < n; i++ {
		availabilityBuckets[0][i] = struct{}{}
	}

	return &Picker{
		PieceCount:           n,
		pieces:               pieces,
		LastPieceLen:         lastPieceLen,
		BlockLength:          BlockLength,
		nextPiece:            0,
		nextBlock:            0,
		wanted:               nil,
		endgame:              false,
		rng:                  rng,
		remainingBlocks:      totalBlocks,
		availabilityBuckets:  availabilityBuckets,
		peerBlockAssignments: peerBlockAssignments,
		peerInflightCount:    make(map[netip.AddrPort]int),
		bitfield:             bitfield.New(n),
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

	for owner := range ps.blocks[bi].owners {
		if owner != peer {
			cancels = append(
				cancels,
				Cancel{
					Peer:  owner,
					Piece: pieceIdx,
					Begin: begin,
				},
			)
		} else {
			freedSelf = true
		}

		// Remove reverse index and free inflight capacity for every
		// owner
		delete(pk.peerBlockAssignments[owner], key)
		pk.peerInflightCount[owner]--
		if pk.peerInflightCount[owner] < 0 {
			pk.peerInflightCount[owner] = 0
		}
	}

	// Defensive: if the delivering peer wasn't recorded as an owner for any
	// reason, ensure we still free its reverse index and inflight capacity.
	if !freedSelf {
		delete(pk.peerBlockAssignments[peer], key)
		pk.peerInflightCount[peer]--
		if pk.peerInflightCount[peer] < 0 {
			pk.peerInflightCount[peer] = 0
		}
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

	if pk.piecesInTransit >= cfg.MaxInflightRequests {
		return []*Request{}
	}
	limit := cfg.MaxInflightRequests - pk.piecesInTransit

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

	pk.piecesInTransit += len(reqs)
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
		if !bf.Has(i) {
		}

		pk.updatePieceAvailability(i, 1)
	}
}

// OnPeerHave updates availability for a single piece announced by 'peer' via a
// HAVE message. Used for incremental availability updates after handshake.
func (pk *Picker) OnPeerHave(peer netip.AddrPort, pieceIdx int) {
	pk.mu.Lock()
	defer pk.mu.Unlock()

	pk.updatePieceAvailability(pieceIdx, 1)
}

// updatePieceAvailability moves a piece index between availability buckets and
// applies the delta to the piece's availability counter. 'val' should usually
// be +1 (peer has it) or -1 (peer gone/didn't have it).
func (pk *Picker) updatePieceAvailability(idx, val int) {
	oldAvail := pk.pieces[idx].availability
	newAvail := oldAvail + val

	delete(pk.availabilityBuckets[oldAvail], idx)
	if len(pk.availabilityBuckets[oldAvail]) <= 0 {
		delete(pk.availabilityBuckets, oldAvail)
	}

	if pk.availabilityBuckets[newAvail] == nil {
		pk.availabilityBuckets[newAvail] = make(map[int]struct{})
	}
	pk.availabilityBuckets[newAvail][idx] = struct{}{}
	pk.pieces[idx].availability = newAvail
}

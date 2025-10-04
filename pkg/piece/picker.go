package piece

import (
	"crypto/sha1"
	"math/rand"
	"net/netip"
	"sync"
	"time"

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
	Peer      netip.AddrPort    // the candidate peer
	Has       bitfield.Bitfield // peer's current bitfield
	Unchoked  bool              // must be true to issue requests
	Pipelined int               // number of pieces to return
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

// Config captures picker-wide knobs that selection logic and timers use.
type Config struct {
	// DownloadStrategy chooses how to rank eligible pieces (see Strategy).
	DownloadStrategy Strategy

	// MaxInflightRequests is the per-peer cap the picker should respect
	// when handing out requests to a single connection. The picker
	// doesn’t enforce per-peer counters by itself — your peer loop
	// should pass a view (capacity) and the picker should not exceed it.
	MaxInflightRequests int

	// RequestTimeout is the baseline time after which an in-flight block
	// can be considered timed-out and re-assigned. You can adapt it
	// per-peer using RTT.
	RequestTimeout time.Duration

	// EndgameDupPerBlock, when Endgame is enabled, caps the number of
	// duplicate owners (peers concurrently fetching the same block).
	EndgameDupPerBlock int

	// MaxRequestsPerBlocks limit how many duplicate blocks can be requested
	// from a single piece at once, preventing over-downloading of
	// individual blocks.
	MaxRequestsPerBlocks int
}

func withDefaultConfig() Config {
	return Config{
		DownloadStrategy:     StrategySequential,
		MaxInflightRequests:  20,
		RequestTimeout:       30 * time.Second,
		EndgameDupPerBlock:   2,
		MaxRequestsPerBlocks: 4,
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

	// Cfg holds picker configuration.
	cfg Config

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
	mut sync.RWMutex

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
}

func NewPicker(
	pieceLength, torrentSize int64,
	pieceHashes [][sha1.Size]byte,
	cfg *Config,
) *Picker {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	c := withDefaultConfig()
	if cfg != nil {
		c = *cfg
	}

	n := len(pieceHashes)

	lastPieceLen := int(torrentSize - (int64(n-1) * pieceLength))
	if lastPieceLen <= 0 {
		lastPieceLen = int(pieceLength)
	}

	totalBlocks := 0
	pieces := make([]*pieceState, n)

	for i := 0; i < n; i++ {
		plen := int(pieceLength)
		if i == n-1 {
			plen = int(lastPieceLen)
		}

		blockCount := (plen + BlockLength - 1) / BlockLength
		totalBlocks += blockCount

		last := plen - (blockCount-1)*BlockLength
		if blockCount == 1 {
			last = plen
		}

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
		cfg:                  c,
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
	}
}

// OnPeerGone: drop this peer from every block’s owner-set and reclaim any
// blocks that now have no owners (i.e., nobody is fetching them).
func (pk *Picker) OnPeerGone(peer netip.AddrPort, bf bitfield.Bitfield) {
	pk.mut.Lock()
	defer pk.mut.Unlock()

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

		if ps.blocks[blockIdx].status == blockInflight &&
			len(ps.blocks[blockIdx].owners) == 0 {
			ps.blocks[blockIdx].status = blockWant
			if pieceIdx == pk.nextPiece && blockIdx < pk.nextBlock {
				pk.nextBlock = blockIdx
			}
		}
	}

	delete(pk.peerBlockAssignments, peer)
	delete(pk.peerInflightCount, peer)
}

// OnBlockReceived marks the block DONE, clears owners, and returns a list of
// duplicate owners to cancel (engame) plus whether the piece completed.
func (pk *Picker) OnBlockReceived(
	peer netip.AddrPort,
	pieceIdx, begin int,
) (bool, []Cancel) {
	pk.mut.Lock()
	defer pk.mut.Unlock()

	if pieceIdx < 0 || pieceIdx >= pk.PieceCount {
		return false, nil
	}

	ps := pk.pieces[pieceIdx]
	bi := begin / pk.BlockLength
	if bi < 0 || bi >= ps.blockCount {
		return false, nil
	}

	var cancels []Cancel

	for other := range ps.blocks[bi].owners {
		if other != peer {
			cancels = append(
				cancels,
				Cancel{
					Peer:  other,
					Piece: pieceIdx,
					Begin: begin,
				},
			)
		}

		delete(pk.peerBlockAssignments[other], packKey(pieceIdx, bi))
		pk.peerInflightCount[other]--
		if pk.peerInflightCount[other] < 0 {
			pk.peerInflightCount[other] = 0
		}
	}
	ps.blocks[bi].owners = make(map[netip.AddrPort]*ownerMeta)

	if ps.blocks[bi].status != blockDone {
		ps.blocks[bi].status = blockDone
		ps.doneBlocks++
		pk.remainingBlocks--
	}

	return ps.doneBlocks == ps.blockCount, cancels
}

// NextForPeer returns atmost ONE request for this peer and registers ownership.
func (pk *Picker) NextForPeer(pv *PeerView) []*Request {
	if !pv.Unchoked {
		return nil
	}

	pk.mut.Lock()
	defer pk.mut.Unlock()

	limit := pk.cfg.MaxInflightRequests - pv.Pipelined
	if limit < 0 {
		limit = 0
	}

	switch pk.cfg.DownloadStrategy {
	case StrategySequential:
		return pk.selectSequentialPiecesToDownload(
			pv.Peer,
			pv.Has,
			limit,
		)
	case StrategyRarestFirst:
		return pk.selectRarestPiecesForDownload(pv.Peer, pv.Has, limit)
	default:
		return pk.selectRandomFirstPiecesForDownload(
			pv.Peer,
			pv.Has,
			limit,
		)
	}
}

// OnTimeout reclaims a single block for a given peer if they owned it.
func (pk *Picker) OnTimeout(peer netip.AddrPort, pieceIdx, begin int) {
	pk.mut.Lock()
	defer pk.mut.Unlock()

	if pieceIdx < 0 || pieceIdx >= pk.PieceCount {
		return
	}

	ps := pk.pieces[pieceIdx]
	bi := begin / pk.BlockLength
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

func (pk *Picker) OnPeerBitfield(peer netip.AddrPort, bf bitfield.Bitfield) {
	pk.mut.Lock()
	defer pk.mut.Unlock()

	for i := 0; i < pk.PieceCount; i++ {
		if !bf.Has(i) {
			continue
		}

		pk.updatePieceAvailability(i, 1)
	}
}

func (pk *Picker) OnPeerHave(peer netip.AddrPort, pieceIdx int) {
	pk.mut.Lock()
	defer pk.mut.Unlock()

	pk.updatePieceAvailability(pieceIdx, 1)
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
	pk.mut.RLock()
	defer pk.mut.RUnlock()

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

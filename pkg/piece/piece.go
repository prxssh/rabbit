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

// BlockSize is the wire-level request granularity.
//
// All blocks are BlockLength bytes except the final block of a piece, which
// maybe shorter.
const BlockLength = 16 * 1024 // 16KiB

// Strategy enumerates high-level peice selection policies the picker can apply.
//
// The current code builds the state in a strategy agnostic manner; your
// selection method can switch on this value to implement different behaviours.
type Strategy uint8

const (
	// StrategyRarestFirst prioritizes pieces with the lowest Availability,
	// improving swarm health and resilience.
	StrategyRarestFirst Strategy = iota

	// StrategySequential downloads pieces in ascending index order. Great
	// for simplicity and streaming/locality; not ideal for swarm health.
	StrategySequential

	// StrategyPriority uses a per-piece Priority field (lower = more
	// important) to bias selection.
	StrategyPriority

	// StrategyRandomFirst randomly samples among eligible pieces (often
	// used only for the first few pieces to reduce clumping), then hands
	// over to another strategy.
	StrategyRandomFirst
)

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

// blockState tracks the lifecycle of an individual block inside a piece.
type blockState uint8

const (
	// blockWant: not yet requested by anyone — eligible for assignment.
	blockWant blockState = iota

	// blockInflight: requested and waiting for data. In endgame, there may
	// be multiple owners (see PieceState.Owners) fetching the same block.
	blockInflight

	// blockDone: fully received and written. Done blocks must never have
	// owners.
	blockDone
)

// ownerMeta tracks per-owner details for a single block.
type ownerMeta struct {
	sentAt  time.Time
	retries uint8
}

type block struct {
	// pendingRequests tracks how many peers are currently downloading this
	// block.
	pendingRequests int

	// status is this block's current lifecycle state
	// (pending/in-flight/complete).
	status blockState

	// owners tracks, the set of peers currently assigned this block to
	// fetch it. Each entry is a map from peer address to OwnerMeta.
	owners map[netip.AddrPort]*ownerMeta
}

// pieceState describes one piece’s static metadata and dynamic progress.
type pieceState struct {
	// index is the zero-based piece index within the torrent.
	index int

	// length is the exact byte length of this piece. For all pieces except
	// the last, it will equal the torrent's piece length; the last may be
	// shorter.
	length int

	// blockCount is the number of requestable blocks in this piece. All
	// blocks
	// except the last are BlockSize long; see LastBlock.
	blockCount int

	// lastBlock is the byte size of the final block in this piece. If
	// Blocks==1, LastBlock == Length. Otherwise LastBlock == Length -
	// (Blocks-1)*BlockSize.
	lastBlock int

	// isLastPiece is true for the last piece of the torrent (useful for
	// edge cases).
	isLastPiece bool

	// sha is the expected SHA-1 of the *piece* (20 bytes from the
	// metainfo).
	sha [sha1.Size]byte

	// availability is the rarity counter: how many connected peers
	// currently advertise this piece. Maintained by your bitfield/HAVE
	// event handlers.
	availability int

	// priority is an optional bias (lower value = more important). Useful
	// for StrategyPriority or user-driven file selection/streaming.
	priority int

	// doneBlocks is a fast counter of how many blocks have reached
	// BlockDone. When DoneBlocks == Blocks the piece is byte-complete and
	// ready to verify.
	doneBlocks int

	// verified is true once the piece has been hashed and matched SHA. A
	// verified piece should have all State==BlockDone and no Owners for any
	// block.
	verified bool

	// blocks holds all blocks in this piece, indexed by block offset.
	blocks []*block
}

// assignment represents a single block assigned to a peer.
type assignment struct {
	pieceIdx uint32
	blockIdx uint32
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

func (pk *Picker) PiceHash(idx int) [sha1.Size]byte {
	pk.mut.RLock()
	defer pk.mut.RUnlock()

	return pk.pieces[idx].sha
}

// CurrentPieceIndex returns the first piece that is not yet verified.
func (pk *Picker) CurrentPieceIndex() (int, bool) {
	pk.mut.RLock()
	defer pk.mut.RUnlock()

	for i := 0; i < pk.PieceCount; i++ {
		if !pk.pieces[i].verified {
			return i, true
		}
	}

	return 0, false
}

// CapacityForPeer returns remaining assignment slots for this peer based on
// picker-owned pipeline accounting and config limits.
func (pk *Picker) CapacityForPeer(peer netip.AddrPort) int {
	pk.mut.RLock()
	defer pk.mut.RUnlock()

	left := pk.cfg.MaxInflightRequests - len(pk.peerBlockAssignments[peer])
	if left < 0 {
		return 0
	}
	return left
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
		pk.peerInflightCount[peer] = 0
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
	pk.peerInflightCount[peer] = 0
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
				Cancel{Peer: other, Piece: pieceIdx},
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

// MarkPieceVerified stamps the current sequential piece as verified on success,
// or resets its blocks to WANT on failure (so it is re-downloaded).
func (pk *Picker) MarkPieceVerified(ok bool) {
	pk.mut.Lock()
	defer pk.mut.Unlock()

	idx := -1
	for i := 0; i < pk.PieceCount; i++ {
		if !pk.pieces[i].verified {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}

	ps := pk.pieces[idx]
	if ok {
		ps.verified = true
		if pk.nextPiece == idx {
			pk.nextPiece++
			pk.nextBlock = 0
		}
		return
	}

	// Bad hash: revert piece to WANT (owners must already be empty when
	// DONE).
	for b := 0; b < ps.blockCount; b++ {
		if ps.blocks[b].status == blockDone {
			pk.remainingBlocks++
		}

		ps.blocks[b].status = blockWant
		ps.blocks[b].owners = make(map[netip.AddrPort]*ownerMeta)
	}
}

// NextForPeer returns atmost ONE request for this peer and registers ownership.
func (pk *Picker) NextForPeer(pv *PeerView) []*Request {
	if !pv.Unchoked {
		return nil
	}

	pk.mut.RLock()
	defer pk.mut.RUnlock()

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

// packKey encodes (piece, block) into a compact uint64 for reverse indexing.
func packKey(pieceIdx, blockIdx int) uint64 {
	return (uint64(uint32(pieceIdx)) << 32) | uint64(uint32(blockIdx))
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

func (pk *Picker) selectSequentialPiecesToDownload(
	peer netip.AddrPort,
	bf bitfield.Bitfield,
	limit int,
) []*Request {
	for pk.nextPiece < pk.PieceCount && pk.pieces[pk.nextPiece].verified {
		pk.nextPiece++
		pk.nextBlock = 0
	}
	if pk.nextPiece >= pk.PieceCount {
		return nil
	}

	ps := pk.pieces[pk.nextPiece]
	if pk.wanted != nil && !pk.wanted[ps.index] {
		return nil
	}
	if !bf.Has(ps.index) {
		return nil
	}

	requests := make([]*Request, 0, limit)
	bi := pk.nextBlock

	for len(requests) < limit && bi < ps.blockCount {
		blk := ps.blocks[bi]
		if blk.status != blockWant ||
			blk.pendingRequests >= pk.cfg.MaxRequestsPerBlocks {
			bi++
			continue
		}

		begin := bi * pk.BlockLength
		length := pk.BlockLength
		if bi == ps.blockCount-1 {
			length = ps.lastBlock
		}

		ps.blocks[bi].status = blockInflight
		ps.blocks[bi].pendingRequests++
		ps.blocks[bi].owners[peer] = &ownerMeta{sentAt: time.Now()}

		key := packKey(ps.index, bi)
		if pk.peerBlockAssignments[peer] == nil {
			pk.peerBlockAssignments[peer] = make(
				map[uint64]struct{},
			)
		}
		pk.peerBlockAssignments[peer][key] = struct{}{}
		pk.peerInflightCount[peer]++
		pk.nextBlock = bi + 1

		requests = append(requests, &Request{
			Peer:   peer,
			Piece:  ps.index,
			Begin:  begin,
			Length: length,
		})
	}

	return requests
}

func (pk *Picker) selectRarestPiecesForDownload(
	peer netip.AddrPort,
	bf bitfield.Bitfield,
	limit int,
) []*Request {
	n := len(pk.availabilityBuckets[0])
	requests := make([]*Request, 0, limit)

	for i := 0; i < n && len(requests) < limit; i++ {
		bucket, exists := pk.availabilityBuckets[i]
		if !exists || len(bucket) == 0 {
			continue
		}

		for pieceIdx := range bucket {
			if len(requests) >= limit {
				break
			}

			ps := pk.pieces[pieceIdx]
			if ps.verified {
				continue
			}
			if !bf.Has(pieceIdx) {
				continue
			}
			if pk.wanted != nil && !pk.wanted[pieceIdx] {
				continue
			}

			for bi := 0; bi < ps.blockCount && len(requests) < limit; bi++ {
				blk := ps.blocks[bi]
				if blk.status != blockWant {
					continue
				}
				if blk.pendingRequests >= pk.cfg.MaxRequestsPerBlocks {
					continue
				}
				begin := bi * pk.BlockLength
				length := pk.BlockLength
				if bi == ps.blockCount-1 {
					length = ps.lastBlock
				}

				ps.blocks[bi].status = blockInflight
				ps.blocks[bi].pendingRequests++
				ps.blocks[bi].owners[peer] = &ownerMeta{
					sentAt: time.Now(),
				}

				key := packKey(ps.index, bi)
				if pk.peerBlockAssignments[peer] == nil {
					pk.peerBlockAssignments[peer] = make(
						map[uint64]struct{},
					)
				}
				pk.peerBlockAssignments[peer][key] = struct{}{}
				pk.peerInflightCount[peer]++
				pk.nextBlock = bi + 1

				requests = append(requests, &Request{
					Peer:   peer,
					Piece:  ps.index,
					Begin:  begin,
					Length: length,
				})
			}
		}
	}

	return requests
}

func (pk *Picker) selectRandomFirstPiecesForDownload(
	peer netip.AddrPort,
	bf bitfield.Bitfield,
	limit int,
) []*Request {
	eligiblePieces := make([]int, 0, pk.PieceCount)
	for i := 0; i < pk.PieceCount; i++ {
		ps := pk.pieces[i]
		if ps.verified {
			continue
		}
		if !bf.Has(i) {
			continue
		}
		if pk.wanted != nil && !pk.wanted[i] {
			continue
		}

		eligiblePieces = append(eligiblePieces, i)
	}

	pk.rng.Shuffle(len(eligiblePieces), func(i, j int) {
		eligiblePieces[i], eligiblePieces[j] = eligiblePieces[j], eligiblePieces[i]
	})

	requests := make([]*Request, 0, limit)

	for _, pieceIdx := range eligiblePieces {
		if len(requests) >= limit {
			break
		}

		ps := pk.pieces[pieceIdx]
		for bi := 0; bi < ps.blockCount && len(requests) < limit; bi++ {
			blk := ps.blocks[bi]
			if blk.status != blockWant {
				continue
			}
			if blk.pendingRequests >= pk.cfg.MaxRequestsPerBlocks {
				continue
			}
			begin := bi * pk.BlockLength
			length := pk.BlockLength
			if bi == ps.blockCount-1 {
				length = ps.lastBlock
			}

			ps.blocks[bi].status = blockInflight
			ps.blocks[bi].pendingRequests++
			ps.blocks[bi].owners[peer] = &ownerMeta{
				sentAt: time.Now(),
			}

			key := packKey(ps.index, bi)
			if pk.peerBlockAssignments[peer] == nil {
				pk.peerBlockAssignments[peer] = make(
					map[uint64]struct{},
				)
			}
			pk.peerBlockAssignments[peer][key] = struct{}{}
			pk.peerInflightCount[peer]++
			pk.nextBlock = bi + 1

			requests = append(requests, &Request{
				Peer:   peer,
				Piece:  ps.index,
				Begin:  begin,
				Length: length,
			})

		}
	}

	return requests
}

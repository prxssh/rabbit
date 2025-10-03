package piece

import (
	"crypto/sha1"
	"math/rand"
	"net/netip"
	"sync"
	"time"

	"github.com/prxssh/rabbit/pkg/utils/bitfield"
)

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
}

func withDefaultConfig() Config {
	return Config{
		DownloadStrategy:    StrategySequential,
		MaxInflightRequests: 20,
		RequestTimeout:      30 * time.Second,
		EndgameDupPerBlock:  2,
	}
}

// BlockState tracks the lifecycle of an individual block inside a piece.
type BlockState uint8

const (
	// BlockWant: not yet requested by anyone — eligible for assignment.
	BlockWant BlockState = iota

	// BlockInflight: requested and waiting for data. In endgame, there may
	// be multiple owners (see PieceState.Owners) fetching the same block.
	BlockInflight

	// BlockDone: fully received and written. Done blocks must never have
	// owners.
	BlockDone
)

// OwnerMeta tracks per-owner details for a single block.
type OwnerMeta struct {
	SentAt  time.Time
	Retries uint8
}

// PieceState describes one piece’s static metadata and dynamic progress.
type PieceState struct {
	// Index is the zero-based piece index within the torrent.
	Index int

	// Length is the exact byte length of this piece. For all pieces except
	// the last, it will equal the torrent's piece length; the last may be
	// shorter.
	Length int

	// Blocks is the number of requestable blocks in this piece. All blocks
	// except the last are BlockSize long; see LastBlock.
	Blocks int

	// LastBlock is the byte size of the final block in this piece. If
	// Blocks==1, LastBlock == Length. Otherwise LastBlock == Length -
	// (Blocks-1)*BlockSize.
	LastBlock int

	// IsLastPiece is true for the last piece of the torrent (useful for
	// edge cases).
	IsLastPiece bool

	// Availability is the rarity counter: how many connected peers
	// currently advertise this piece. Maintained by your bitfield/HAVE
	// event handlers.
	Availability int

	// Priority is an optional bias (lower value = more important). Useful
	// for StrategyPriority or user-driven file selection/streaming.
	Priority int

	// DoneBlocks is a fast counter of how many blocks have reached
	// BlockDone. When DoneBlocks == Blocks the piece is byte-complete and
	// ready to verify.
	DoneBlocks int

	// Verified is true once the piece has been hashed and matched SHA. A
	// verified piece should have all State==BlockDone and no Owners for any
	// block.
	Verified bool

	// State holds the per-block lifecycle (want/inflight/done).
	// len(State)==Blocks.
	State []BlockState

	// Owners tracks, per block, the set of peers currently assigned to
	// fetch it. Each entry is a map from peer address to OwnerMeta (send
	// time, retries, etc.). In normal mode the set is empty or size 1. In
	// endgame it may contain up to Cfg.EndgameDupPerBlock distinct peers.
	// When State == BlockDone, this map
	// MUST be empty.
	Owners []map[netip.AddrPort]*OwnerMeta

	// SentAt records the last time a request for this block was sent. Used
	// for timeouts and adaptive retry logic. Zero time means "never sent".
	SentAt []time.Time

	// Retries tracks how many times this specific block has been
	// re-assigned due to timeout/failure. Can backoff or penalize peers
	// using this data.
	Retries []uint8

	// SHA is the expected SHA-1 of the *piece* (20 bytes from the
	// metainfo).
	SHA [sha1.Size]byte
}

// Picker is the global download planner/state holder for a single torrent.
type Picker struct {
	// Cfg holds strategy and timeout knobs.
	Cfg Config

	// BlockLength mirrors BlockSize for clarity; kept as a field in case
	// you want to support different block sizes (e.g., for testing).
	BlockLength int

	// LastPieceLen caches the exact byte length of the final piece (handy
	// when emitting block sizes for that piece).
	LastPieceLen int

	// PieceCount = len(Pieces).
	PieceCount int

	// Pieces is the complete set of per-piece states, indexed by piece
	// index.
	Pieces []*PieceState

	// NextPiece and NextBlock act as cursors for StrategySequential. They
	// can be ignored by other strategies that compute candidates
	// differently.
	NextPiece int
	NextBlock int

	// Wanted is an optional "selective download" filter. If non-nil, only
	// pieces with Wanted[index]==true are eligible. If nil, all pieces are
	// eligible.
	Wanted map[int]bool

	// Endgame toggles duplication of remaining not-done blocks. When
	// enabled, you should allow multiple Owners per block up to
	// Cfg.EndgameDupPerBlock and cancel losers as soon as the first copy
	// arrives.
	Endgame bool

	// RemainingBlocks is a global counter of blocks whose State !=
	// BlockDone. Once it drops below a small threshold (e.g., 32), you
	// typically enable Endgame. When it reaches 0, the torrent is
	// byte-complete.
	RemainingBlocks int

	// rng is used for StrategyRandomFirst and tie-breaking when multiple
	// pieces have identical rank (e.g., same rarity and priority).
	rng *rand.Rand
	mut sync.RWMutex

	ownersByPeer map[netip.AddrPort]map[uint64]struct{}
	outByPeer    map[netip.AddrPort]int
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
	pieces := make([]*PieceState, n)

	for i := 0; i < n; i++ {
		plen := int(pieceLength)
		if i == n-1 {
			plen = int(lastPieceLen)
		}

		blocks := (plen + BlockLength - 1) / BlockLength
		last := plen - (blocks-1)*BlockLength
		if blocks == 1 {
			last = plen
		}

		pieces[i] = &PieceState{
			Index:       i,
			Length:      int(plen),
			Blocks:      blocks,
			LastBlock:   last,
			IsLastPiece: i == n-1,
			State:       make([]BlockState, blocks),
			Owners: make(
				[]map[netip.AddrPort]*OwnerMeta,
				blocks,
			),
			SentAt:   make([]time.Time, blocks),
			Retries:  make([]uint8, blocks),
			SHA:      pieceHashes[i],
			Verified: false,
		}

		for b := 0; b < blocks; b++ {
			pieces[i].Owners[b] = make(
				map[netip.AddrPort]*OwnerMeta,
			)
		}

		totalBlocks += blocks
	}

	return &Picker{
		Cfg:             c,
		PieceCount:      n,
		Pieces:          pieces,
		LastPieceLen:    lastPieceLen,
		BlockLength:     BlockLength,
		NextPiece:       0,
		NextBlock:       0,
		Wanted:          nil,
		Endgame:         false,
		rng:             rng,
		RemainingBlocks: totalBlocks,
		ownersByPeer:    make(map[netip.AddrPort]map[uint64]struct{}),
		outByPeer:       make(map[netip.AddrPort]int),
	}
}

func (pk *Picker) PiceHash(idx int) [sha1.Size]byte {
	pk.mut.RLock()
	defer pk.mut.RUnlock()

	return pk.Pieces[idx].SHA
}

// CurrentPieceIndex returns the first piece that is not yet verified.
func (pk *Picker) CurrentPieceIndex() (int, bool) {
	pk.mut.RLock()
	defer pk.mut.RUnlock()

	for i := 0; i < pk.PieceCount; i++ {
		if !pk.Pieces[i].Verified {
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

	left := pk.Cfg.MaxInflightRequests - pk.outByPeer[peer]
	if left < 0 {
		return 0
	}
	return left
}

// OnPeerGone: drop this peer from every block’s owner-set and reclaim any
// blocks that now have no owners (i.e., nobody is fetching them).
func (pk *Picker) OnPeerGone(peer netip.AddrPort) {
	pk.mut.Lock()
	defer pk.mut.Unlock()

	keys := pk.ownersByPeer[peer]
	if len(keys) == 0 {
		delete(pk.ownersByPeer, peer)
		pk.outByPeer[peer] = 0
		return
	}
	for key := range keys {
		pieceIdx := int(uint32(key >> 32))
		blockIdx := int(uint32(key))
		if pieceIdx < 0 || pieceIdx >= pk.PieceCount {
			continue
		}

		ps := pk.Pieces[pieceIdx]
		if blockIdx < 0 || blockIdx >= ps.Blocks {
			continue
		}
		delete(ps.Owners[blockIdx], peer)

		if ps.State[blockIdx] == BlockInflight &&
			len(ps.Owners[blockIdx]) == 0 {
			ps.State[blockIdx] = BlockWant
			if pieceIdx == pk.NextPiece && blockIdx < pk.NextBlock {
				pk.NextBlock = blockIdx
			}
		}
	}

	delete(pk.ownersByPeer, peer)
	pk.outByPeer[peer] = 0
}

type Cancel struct {
	Peer  netip.AddrPort
	Piece int
	Begin int
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

	ps := pk.Pieces[pieceIdx]
	bi := begin / pk.BlockLength
	if bi < 0 || bi >= ps.Blocks {
		return false, nil
	}

	var cancels []Cancel

	for other := range ps.Owners[bi] {
		if other != peer {
			cancels = append(
				cancels,
				Cancel{Peer: other, Piece: pieceIdx},
			)
		}

		delete(pk.ownersByPeer[other], packKey(pieceIdx, bi))
		pk.outByPeer[other]--
		if pk.outByPeer[other] < 0 {
			pk.outByPeer[other] = 0
		}
	}
	ps.Owners[bi] = make(map[netip.AddrPort]*OwnerMeta)

	if ps.State[bi] != BlockDone {
		ps.State[bi] = BlockDone
		ps.DoneBlocks++
		pk.RemainingBlocks--
	}

	return ps.DoneBlocks == ps.Blocks, cancels
}

// MarkPieceVerified stamps the current sequential piece as verified on success,
// or resets its blocks to WANT on failure (so it is re-downloaded).
func (pk *Picker) MarkPieceVerified(ok bool) {
	pk.mut.Lock()
	defer pk.mut.Unlock()

	idx := -1
	for i := 0; i < pk.PieceCount; i++ {
		if !pk.Pieces[i].Verified {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}

	ps := pk.Pieces[idx]
	if ok {
		ps.Verified = true
		if pk.NextPiece == idx {
			pk.NextPiece++
			pk.NextBlock = 0
		}
		return
	}

	// Bad hash: revert piece to WANT (owners must already be empty when
	// DONE).
	for b := 0; b < ps.Blocks; b++ {
		if ps.State[b] == BlockDone {
			pk.RemainingBlocks++
		}

		ps.State[b] = BlockWant
		ps.SentAt[b] = time.Time{}
		ps.Retries[b] = 0
		ps.Owners[b] = make(map[netip.AddrPort]*OwnerMeta)
	}
}

// NextForPeer returns atmost ONE request for this peer and registers ownership.
func (pk *Picker) NextForPeer(pv *PeerView) *Request {
	if !pv.Unchoked {
		return nil
	}

	pk.mut.Lock()
	defer pk.mut.Unlock()

	if pk.outByPeer[pv.Peer] >= pk.Cfg.MaxInflightRequests {
		return nil
	}

	for pk.NextPiece < pk.PieceCount && pk.Pieces[pk.NextPiece].Verified {
		pk.NextPiece++
		pk.NextBlock = 0
	}
	if pk.NextPiece >= pk.PieceCount {
		return nil
	}
	ps := pk.Pieces[pk.NextPiece]

	if pk.Wanted != nil && !pk.Wanted[ps.Index] {
		return nil
	}

	if !pv.Has.Has(ps.Index) {
		return nil
	}

	bi := pk.NextBlock
	for bi < ps.Blocks && ps.State[bi] != BlockWant {
		bi++
	}
	if bi >= ps.Blocks {
		return nil
	}

	begin := bi * pk.BlockLength
	length := pk.BlockLength
	if bi == ps.Blocks-1 {
		length = ps.LastBlock
	}

	ps.State[bi] = BlockInflight
	ps.Retries[bi]++
	om := &OwnerMeta{SentAt: time.Now(), Retries: ps.Retries[bi]}
	ps.Owners[bi][pv.Peer] = om

	key := packKey(ps.Index, bi)
	if pk.ownersByPeer[pv.Peer] == nil {
		pk.ownersByPeer[pv.Peer] = make(map[uint64]struct{})
	}
	pk.ownersByPeer[pv.Peer][key] = struct{}{}
	pk.outByPeer[pv.Peer]++
	pk.NextBlock = bi + 1

	return &Request{
		Peer:   pv.Peer,
		Piece:  ps.Index,
		Begin:  begin,
		Length: length,
	}
}

// OnTimeout reclaims a single block for a given peer if they owned it.
func (pk *Picker) OnTimeout(peer netip.AddrPort, pieceIdx, begin int) {
	pk.mut.Lock()
	defer pk.mut.Unlock()

	if pieceIdx < 0 || pieceIdx >= pk.PieceCount {
		return
	}

	ps := pk.Pieces[pieceIdx]
	bi := begin / pk.BlockLength
	if bi < 0 || bi >= ps.Blocks {
		return
	}

	if _, had := ps.Owners[bi][peer]; !had {
		return
	}
	delete(ps.Owners[bi], peer)
	delete(pk.ownersByPeer[peer], packKey(pieceIdx, bi))
	pk.outByPeer[peer]--
	if pk.outByPeer[peer] < 0 {
		pk.outByPeer[peer] = 0
	}

	// If nobody else is fetching it, return to WANT.
	if ps.State[bi] == BlockInflight && len(ps.Owners[bi]) == 0 {
		ps.State[bi] = BlockWant
		// Pull back sequential cursor to retry sooner.
		if pieceIdx == pk.NextPiece {
			if b := bi; b < pk.NextBlock {
				pk.NextBlock = b
			}
		}
	}
}

// packKey encodes (piece, block) into a compact uint64 for reverse indexing.
func packKey(pieceIdx, blockIdx int) uint64 {
	return (uint64(uint32(pieceIdx)) << 32) | uint64(uint32(blockIdx))
}

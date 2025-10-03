package piece

import (
	"crypto/sha1"
	"math/rand"
	"sync"
	"time"
)

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

	// Owners counts how many peers currently "own" (are fetching) this
	// block. In normal mode this is 0 or 1; in endgame it can be up to
	// EndgameDupPerBlock. When State==BlockDone, Owners must be 0.
	Owners []int

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
			Owners:      make([]int, blocks),
			SentAt:      make([]time.Time, blocks),
			Retries:     make([]uint8, blocks),
			SHA:         pieceHashes[i],
			Verified:    false,
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
	}
}

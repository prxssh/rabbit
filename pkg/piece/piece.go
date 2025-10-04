package piece

import (
	"crypto/sha1"
	"net/netip"
	"time"
)

// BlockSize is the wire-level request granularity.
//
// All blocks are BlockLength bytes except the final block of a piece, which
// maybe shorter.
const BlockLength = 16 * 1024 // 16KiB

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

func (pk *Picker) PieceHash(idx int) [sha1.Size]byte {
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
		avail := ps.availability
		delete(pk.availabilityBuckets[avail], idx)
		if len(pk.availabilityBuckets[avail]) == 0 {
			delete(pk.availabilityBuckets, avail)
		}

		if pk.nextPiece == idx {
			pk.nextPiece++
			pk.nextBlock = 0
		}

		return
	}

	// Bad hash: revert piece to WANT
	for b := 0; b < ps.blockCount; b++ {
		if ps.blocks[b].status == blockDone {
			pk.remainingBlocks++
		}

		ps.blocks[b].status = blockWant
		ps.blocks[b].owners = make(map[netip.AddrPort]*ownerMeta)
	}
	ps.doneBlocks = 0
}

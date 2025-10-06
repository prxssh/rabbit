package piece

import (
	"crypto/sha1"
	"fmt"
	"net/netip"
	"time"
)

// PieceCount returns how many pieces are needed to cover totalSize bytes, given
// a fixed pieceLength (except the last piece which may be shorter).
func PieceCount(totalSize, pieceLength int64) int {
	if totalSize <= 0 || pieceLength <= 0 {
		return 0
	}

	return int((totalSize + pieceLength - 1) / pieceLength)
}

// LastPieceLength returns the exact byte length of the last piece.
// For totals that are an exact multiple of pieceLength, this equals
// pieceLength.
func LastPieceLength(totalSize, pieceLength int64) int {
	if totalSize <= 0 || pieceLength <= 0 {
		return 0
	}

	rem := int(totalSize % pieceLength)
	if rem == 0 {
		return int(pieceLength)
	}

	return rem
}

// PieceLengthAt returns the piece length for a specific piece index.
// All pieces but the last are pieceLength; the last may be shorter.
func PieceLengthAt(index int, totalSize, pieceLength int64) (int, error) {
	pc := PieceCount(totalSize, pieceLength)
	if index < 0 || index >= pc {
		return 0, fmt.Errorf(
			"piece index out of range: %d (count=%d)",
			index,
			pc,
		)
	}

	if index == pc-1 {
		return LastPieceLength(totalSize, pieceLength), nil
	}
	return int(pieceLength), nil
}

// PieceOffsetBounds returns [start,end) byte offsets in the global stream for a
// piece.
func PieceOffsetBounds(
	index int,
	totalSize, pieceLength int64,
) (start int64, end int64, err error) {
	pl, err := PieceLengthAt(index, totalSize, pieceLength)
	if err != nil {
		return 0, 0, err
	}

	start = int64(index) * pieceLength
	end = start + int64(pl)
	return start, end, nil
}

// PieceIndexForOffset maps a stream byte offset to its piece index.
// Returns -1 when offset is out of range.
func PieceIndexForOffset(offset, totalSize, pieceLength int64) int {
	if offset < 0 || offset >= totalSize || pieceLength <= 0 {
		return -1
	}
	return int(offset / pieceLength)
}

// BlockCountForPiece returns how many blocks compose a piece of length
// pieceLen, given a fixed blockLen (except the last block which may be
// shorter).
func BlockCountForPiece(pieceLen, blockLen int) int {
	if pieceLen <= 0 || blockLen <= 0 {
		return 0
	}

	n := pieceLen / blockLen
	if pieceLen%blockLen != 0 {
		n++
	}

	return n
}

// LastBlockLength returns the exact byte length of the final block in a piece.
func LastBlockLength(pieceLen, blockLen int) int {
	if pieceLen <= 0 || blockLen <= 0 {
		return 0
	}

	rem := pieceLen % blockLen
	if rem == 0 {
		return blockLen
	}

	return rem
}

// BlockOffsetBounds returns the block's [begin,length] within a piece, where
// begin is the byte offset from the start of the piece.
func BlockOffsetBounds(
	pieceLen, blockLen, blockIdx int,
) (begin int, length int, err error) {
	bc := BlockCountForPiece(pieceLen, blockLen)
	if blockIdx < 0 || blockIdx >= bc {
		return 0, 0, fmt.Errorf(
			"block index out of range: %d (count=%d)",
			blockIdx,
			bc,
		)
	}

	begin = blockIdx * blockLen
	length = blockLen
	if blockIdx == bc-1 {
		length = LastBlockLength(pieceLen, blockLen)
	}

	return begin, length, nil
}

// BlockIndexForBegin returns the block index inside a piece for a given byte
// offset 'begin' within that piece. Returns -1 when out of range.
func BlockIndexForBegin(begin, pieceLen, blockLen int) int {
	if begin < 0 || begin >= pieceLen || blockLen <= 0 {
		return -1
	}

	return begin / blockLen
}

// BlocksInPiece uses the package-wide BlockLength.
func BlocksInPiece(pieceLen int) int {
	return BlockCountForPiece(pieceLen, BlockLength)
}

// LastBlockInPiece uses the package-wide BlockLength.
func LastBlockInPiece(pieceLen int) int {
	return LastBlockLength(pieceLen, BlockLength)
}

// BlockBounds uses the package-wide BlockLength.
func BlockBounds(pieceLen, blockIdx int) (begin int, length int, err error) {
	return BlockOffsetBounds(pieceLen, BlockLength, blockIdx)
}

// StreamToPieceBlock maps a stream offset to
// (pieceIdx, blockIdx, beginWithinPiece). Returns (-1,-1,-1) on invalid input.
func StreamToPieceBlock(
	offset, totalSize, pieceLength int64,
	blockLen int,
) (pieceIdx int, blockIdx int, begin int) {
	pieceIdx = PieceIndexForOffset(offset, totalSize, pieceLength)
	if pieceIdx < 0 {
		return -1, -1, -1
	}

	start, _, err := PieceOffsetBounds(pieceIdx, totalSize, pieceLength)
	if err != nil {
		return -1, -1, -1
	}

	begin = int(offset - start) // begin within piece
	pl, _ := PieceLengthAt(pieceIdx, totalSize, pieceLength)
	blockIdx = BlockIndexForBegin(begin, pl, blockLen)
	if blockIdx < 0 {
		return -1, -1, -1
	}

	return pieceIdx, blockIdx, begin
}

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
	// blocks except the last are BlockSize long; see LastBlock.
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
	pk.mu.RLock()
	defer pk.mu.RUnlock()

	return pk.pieces[idx].sha
}

// CurrentPieceIndex returns the first piece that is not yet verified.
func (pk *Picker) CurrentPieceIndex() (int, bool) {
	pk.mu.RLock()
	defer pk.mu.RUnlock()

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
	pk.mu.RLock()
	defer pk.mu.RUnlock()

	left := pk.cfg.MaxInflightRequests - len(pk.peerBlockAssignments[peer])
	if left < 0 {
		return 0
	}
	return left
}

// MarkPieceVerified stamps the current sequential piece as verified on success,
// or resets its blocks to WANT on failure (so it is re-downloaded).
func (pk *Picker) MarkPieceVerified(ok bool) {
	pk.mu.Lock()
	defer pk.mu.Unlock()

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
		pk.bitfield.Set(idx)
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

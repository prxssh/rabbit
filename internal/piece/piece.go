package piece

import (
	"crypto/sha1"
	"fmt"
	"net/netip"
	"time"
)

const BlockLength = 16 * 1024 // 16KiB

type blockStatus uint8

const (
	blockWant blockStatus = iota
	blockInflight
	blockDone
)

type blockOwner struct {
	addr        netip.AddrPort
	requestedAt time.Time
}

type block struct {
	pendingRequests int
	status          blockStatus
	owner           *blockOwner
}

// pieceState describes one piece’s static metadata and dynamic progress.
type pieceState struct {
	// index is the zero-based piece index within the torrent.
	index int

	// length is the exact byte length of this piece. For all pieces except
	// the last, it will equal the torrent's piece length; the last may be
	// shorter.
	length int32

	// blockCount is the number of requestable blocks in this piece. All
	// blocks except the last are BlockSize long; see LastBlock.
	blockCount int

	// lastBlock is the byte size of the final block in this piece. If
	// Blocks==1, LastBlock == Length. Otherwise LastBlock == Length -
	// (Blocks-1)*BlockSize.
	lastBlock int32

	// isLastPiece is true for the last piece of the torrent (useful for
	// edge cases).
	isLastPiece bool

	// sha is the expected SHA-1 of the *piece* (20 bytes from the
	// metainfo).
	sha [sha1.Size]byte

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

func (pk *Picker) MarkPieceVerified(idx int, ok bool) {
	if idx < 0 || idx >= pk.PieceCount {
		return
	}

	pk.mu.Lock()
	defer pk.mu.Unlock()

	ps := pk.pieces[idx]
	if ok {
		ps.verified = true
		pk.bitfield.Set(idx)

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

// PieceCount returns how many pieces are needed to cover `size` bytes.
func PieceCount(size int64, pieceLen int32) int {
	if size <= 0 || pieceLen <= 0 {
		return 0
	}
	return int((size + int64(pieceLen) - 1) / int64(pieceLen))
}

// LastPieceLength returns the exact length of the final piece in bytes.
func LastPieceLength(size int64, pieceLen int32) int32 {
	if size <= 0 || pieceLen <= 0 {
		return 0
	}

	rem := size % int64(pieceLen)
	if rem == 0 {
		return pieceLen
	}
	return int32(rem)
}

// PieceLengthAt returns the length of piece `index`.
func PieceLengthAt(index int, size int64, pieceLen int32) (int32, error) {
	pc := PieceCount(size, pieceLen)
	if index < 0 || index >= pc {
		return 0, fmt.Errorf("piece index out of range: %d (count=%d)", index, pc)
	}

	if index == pc-1 {
		return LastPieceLength(size, pieceLen), nil
	}
	return pieceLen, nil
}

// PieceOffsetBounds returns the [start,end) offsets for a piece.
func PieceOffsetBounds(index int, size int64, pieceLen int32) (start, end int64, err error) {
	pl, err := PieceLengthAt(index, size, pieceLen)
	if err != nil {
		return 0, 0, err
	}

	start = int64(index) * int64(pieceLen)
	end = start + int64(pl)
	return start, end, nil
}

// PieceIndexForOffset maps a stream offset to its piece index.
// Returns -1 if out of range.
func PieceIndexForOffset(offset, size int64, pieceLen int32) int {
	if offset < 0 || offset >= size || pieceLen <= 0 {
		return -1
	}
	return int(offset / int64(pieceLen))
}

// BlockCountForPiece returns the number of blocks in a piece.
func BlockCountForPiece(pieceLen, blockLen int32) int {
	if pieceLen <= 0 || blockLen <= 0 {
		return 0
	}
	return int((pieceLen + blockLen - 1) / blockLen)
}

// LastBlockLength returns the exact byte length of the final block.
func LastBlockLength(pieceLen, blockLen int32) int32 {
	if pieceLen <= 0 || blockLen <= 0 {
		return 0
	}

	rem := pieceLen % blockLen
	if rem == 0 {
		return blockLen
	}
	return rem
}

// BlockOffsetBounds returns the [begin,length] of a block within a piece.
func BlockOffsetBounds(pieceLen, blockLen int32, blockIdx int) (begin, length int32, err error) {
	bc := BlockCountForPiece(pieceLen, blockLen)
	if blockIdx < 0 || blockIdx >= bc {
		return 0, 0, fmt.Errorf("block index out of range: %d (count=%d)", blockIdx, bc)
	}

	begin = int32(blockIdx) * blockLen
	length = blockLen
	if blockIdx == bc-1 {
		length = LastBlockLength(pieceLen, blockLen)
	}
	return begin, length, nil
}

// BlockIndexForBegin returns the block index for a byte offset within a piece.
func BlockIndexForBegin(begin, pieceLen, blockLen int) int {
	if begin < 0 || begin >= pieceLen || blockLen <= 0 {
		return -1
	}
	return int(begin / blockLen)
}

func BlocksInPiece(pieceLen int32) int {
	return BlockCountForPiece(pieceLen, BlockLength)
}

func LastBlockInPiece(pieceLen int32) int32 {
	return LastBlockLength(pieceLen, BlockLength)
}

func BlockBounds(pieceLen int32, blockIdx int) (int32, int32, error) {
	return BlockOffsetBounds(pieceLen, BlockLength, blockIdx)
}

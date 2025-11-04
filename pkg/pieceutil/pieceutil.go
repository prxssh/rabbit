package pieceutil

import "fmt"

const MaxBlockLength = 16 * 1024

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
func BlockIndexForBegin(begin, pieceLen int) int {
	if begin < 0 || begin >= pieceLen || MaxBlockLength <= 0 {
		return -1
	}
	return int(begin / MaxBlockLength)
}

func BlocksInPiece(pieceLen int32) int {
	return BlockCountForPiece(pieceLen, MaxBlockLength)
}

func LastBlockInPiece(pieceLen int32) int32 {
	return LastBlockLength(pieceLen, MaxBlockLength)
}

func BlockBounds(pieceLen int32, blockIdx int) (int32, int32, error) {
	return BlockOffsetBounds(pieceLen, MaxBlockLength, blockIdx)
}

package piece

// PieceCount returns how many pieces are needed to cover `size` bytes.
func PieceCount(size uint64, pieceLen uint32) (uint32, bool) {
	if size <= 0 || pieceLen <= 0 {
		return 0, false
	}

	return uint32((size + uint64(pieceLen) - 1) / uint64(pieceLen)), true
}

// LastPieceLength returns the exact length of the final piece in bytes.
//
// If the total size is a perfect multiple of pieceLen, this returns pieceLen.
func LastPieceLength(size uint64, pieceLen uint32) (uint32, bool) {
	if size <= 0 || pieceLen <= 0 {
		return 0, false
	}

	rem := size % uint64(pieceLen)
	if rem == 0 {
		return pieceLen, true
	}

	return uint32(rem), true
}

// PieceLengthAt returns the length of piece `index`.
//
// All pieces are `pieceLen` long, except for the last piece, which may be shorter.
func PieceLengthAt(index uint32, size uint64, pieceLen uint32) (uint32, bool) {
	if index < 0 || size <= 0 || pieceLen <= 0 {
		return 0, false
	}

	count, ok := PieceCount(size, pieceLen)
	if !ok {
		return 0, false
	}
	if index >= count {
		return 0, false
	}

	if index == count-1 {
		return LastPieceLength(size, pieceLen)
	}

	return pieceLen, true
}

// PieceOffsetBounds returns the [start,end) byte offsets for a piece.
func PieceOffsetBounds(index uint32, size uint64, pieceLen uint32) (uint32, uint32, bool) {
	indexPieceLen, ok := PieceLengthAt(index, size, pieceLen)
	if !ok {
		return 0, 0, false
	}

	start := index * pieceLen
	end := start + indexPieceLen
	return start, end, true
}

// PieceIndexForOffset maps a stream offset to its piece index.
func PieceIndexForOffset(offset uint32, size uint64, pieceLen uint32) (uint32, bool) {
	if offset < 0 || uint64(offset) >= size || pieceLen <= 0 {
		return 0, false
	}

	return offset / pieceLen, true
}

// BlockCountForPiece returns the number of blocks in a piece.
func BlockCountForPiece(pieceLen, blockLen uint32) (uint32, bool) {
	if pieceLen <= 0 || blockLen <= 0 {
		return 0, false
	}

	return (pieceLen + blockLen - 1) / blockLen, true
}

// LastBlockLength returns the exact byte length of the final block.
func LastBlockLength(pieceLen, blockLen uint32) (uint32, bool) {
	if pieceLen <= 0 || blockLen <= 0 {
		return 0, false
	}

	rem := pieceLen % blockLen
	if rem == 0 {
		return blockLen, true
	}

	return rem, true
}

// BlockOffsetBounds returns the [begin,length] of a block within a piece.
//
// `begin` is the byte offset within the piece, and `length` is the byte length of that specific
// block.
func BlockOffsetBounds(pieceLen, blockLen, blockIdx uint32) (begin, length uint32, ok bool) {
	bc, ok := BlockCountForPiece(pieceLen, blockLen)
	if !ok {
		return 0, 0, false
	}

	begin = blockIdx * blockLen
	length = blockLen

	if blockIdx == bc-1 {
		length, _ = LastBlockLength(pieceLen, blockLen)
	}

	return begin, length, true
}

// BlockIndexForBegin returns the block index for a byte offset `begin` within a piece.
func BlockIndexForBegin(begin uint32, pieceLen uint32) (uint32, bool) {
	if begin < 0 || begin >= pieceLen || MaxBlockLength <= 0 {
		return 0, false
	}

	return begin / MaxBlockLength, true
}

// BlocksInPiece returns the number of standard `MaxBlockLength` blocks in a piece.
func BlocksInPiece(pieceLen uint32) (uint32, bool) {
	return BlockCountForPiece(pieceLen, MaxBlockLength)
}

// LastBlockInPiece returns the length of the last standard block in a piece.
func LastBlockInPiece(pieceLen uint32) (uint32, bool) {
	return LastBlockLength(pieceLen, MaxBlockLength)
}

// BlockBounds returns the [begin,length] of a block.
func BlockBounds(pieceLen, blockIdx uint32) (uint32, uint32, bool) {
	return BlockOffsetBounds(pieceLen, MaxBlockLength, blockIdx)
}

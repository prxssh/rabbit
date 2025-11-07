package piece

import "testing"

func TestPieceCount(t *testing.T) {
	tests := []struct {
		name       string
		size       uint64
		pieceLen   uint32
		want_count uint32
		want_ok    bool
	}{
		{"zero size", 0, 1024, 0, false},
		{"zero pieceLen", 1024, 0, 0, false},
		{"exact fit", 2048, 1024, 2, true},
		{"one extra byte", 2049, 1024, 3, true},
		{"less than one piece", 512, 1024, 1, true},
		{"large size", 1 << 30, 1 << 20, 1024, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_count, got_ok := PieceCount(tt.size, tt.pieceLen)
			if got_count != tt.want_count || got_ok != tt.want_ok {
				t.Errorf(
					"PieceCount() = (%v, %v), want (%v, %v)",
					got_count,
					got_ok,
					tt.want_count,
					tt.want_ok,
				)
			}
		})
	}
}

func TestLastPieceLength(t *testing.T) {
	tests := []struct {
		name     string
		size     uint64
		pieceLen uint32
		want_len uint32
		want_ok  bool
	}{
		{"zero size", 0, 1024, 0, false},
		{"zero pieceLen", 1024, 0, 0, false},
		{"exact fit", 2048, 1024, 1024, true},
		{"one extra byte", 2049, 1024, 1, true},
		{"less than one piece", 512, 1024, 512, true},
		{"large size", (1 << 30) + 123, 1 << 20, 123, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_len, got_ok := LastPieceLength(tt.size, tt.pieceLen)
			if got_len != tt.want_len || got_ok != tt.want_ok {
				t.Errorf(
					"LastPieceLength() = (%v, %v), want (%v, %v)",
					got_len,
					got_ok,
					tt.want_len,
					tt.want_ok,
				)
			}
		})
	}
}

func TestPieceLengthAt(t *testing.T) {
	tests := []struct {
		name     string
		index    uint32
		size     uint64
		pieceLen uint32
		want_len uint32
		want_ok  bool
	}{
		{"zero size", 0, 0, 1024, 0, false},
		{"zero pieceLen", 0, 1024, 0, 0, false},
		{"first piece", 0, 2048, 1024, 1024, true},
		{"last piece", 1, 2048, 1024, 1024, true},
		{"out of bounds", 2, 2048, 1024, 0, false},
		{"last piece (not exact)", 2, 2049, 1024, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_len, got_ok := PieceLengthAt(tt.index, tt.size, tt.pieceLen)
			if got_len != tt.want_len || got_ok != tt.want_ok {
				t.Errorf(
					"PieceLengthAt() = (%v, %v), want (%v, %v)",
					got_len,
					got_ok,
					tt.want_len,
					tt.want_ok,
				)
			}
		})
	}
}

func TestPieceOffsetBounds(t *testing.T) {
	tests := []struct {
		name       string
		index      uint32
		size       uint64
		pieceLen   uint32
		want_start uint32
		want_end   uint32
		want_ok    bool
	}{
		{"zero size", 0, 0, 1024, 0, 0, false},
		{"first piece", 0, 2048, 1024, 0, 1024, true},
		{"second piece", 1, 2048, 1024, 1024, 2048, true},
		{"last piece (not exact)", 2, 2049, 1024, 2048, 2049, true},
		{"out of bounds", 3, 2049, 1024, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_start, got_end, got_ok := PieceOffsetBounds(
				tt.index,
				tt.size,
				tt.pieceLen,
			)
			if got_start != tt.want_start || got_end != tt.want_end ||
				got_ok != tt.want_ok {
				t.Errorf(
					"PieceOffsetBounds() = (%v, %v, %v), want (%v, %v, %v)",
					got_start,
					got_end,
					got_ok,
					tt.want_start,
					tt.want_end,
					tt.want_ok,
				)
			}
		})
	}
}

func TestPieceIndexForOffset(t *testing.T) {
	tests := []struct {
		name       string
		offset     uint32
		size       uint64
		pieceLen   uint32
		want_index uint32
		want_ok    bool
	}{
		{"zero offset", 0, 2048, 1024, 0, true},
		{"in first piece", 512, 2048, 1024, 0, true},
		{"at boundary", 1024, 2048, 1024, 1, true},
		{"in second piece", 1536, 2048, 1024, 1, true},
		{"out of bounds", 2048, 2048, 1024, 0, false},
		{"zero pieceLen", 1024, 2048, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_index, got_ok := PieceIndexForOffset(tt.offset, tt.size, tt.pieceLen)
			if got_index != tt.want_index || got_ok != tt.want_ok {
				t.Errorf(
					"PieceIndexForOffset() = (%v, %v), want (%v, %v)",
					got_index,
					got_ok,
					tt.want_index,
					tt.want_ok,
				)
			}
		})
	}
}

func TestBlockCountForPiece(t *testing.T) {
	tests := []struct {
		name       string
		pieceLen   uint32
		blockLen   uint32
		want_count uint32
		want_ok    bool
	}{
		{"zero pieceLen", 0, 16384, 0, false},
		{"zero blockLen", 1024, 0, 0, false},
		{"exact fit", 32768, 16384, 2, true},
		{"one extra byte", 32769, 16384, 3, true},
		{"less than one block", 8192, 16384, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_count, got_ok := BlockCountForPiece(tt.pieceLen, tt.blockLen)
			if got_count != tt.want_count || got_ok != tt.want_ok {
				t.Errorf(
					"BlockCountForPiece() = (%v, %v), want (%v, %v)",
					got_count,
					got_ok,
					tt.want_count,
					tt.want_ok,
				)
			}
		})
	}
}

func TestLastBlockLength(t *testing.T) {
	tests := []struct {
		name     string
		pieceLen uint32
		blockLen uint32
		want_len uint32
		want_ok  bool
	}{
		{"zero pieceLen", 0, 16384, 0, false},
		{"zero blockLen", 1024, 0, 0, false},
		{"exact fit", 32768, 16384, 16384, true},
		{"one extra byte", 32769, 16384, 1, true},
		{"less than one block", 8192, 16384, 8192, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_len, got_ok := LastBlockLength(tt.pieceLen, tt.blockLen)
			if got_len != tt.want_len || got_ok != tt.want_ok {
				t.Errorf(
					"LastBlockLength() = (%v, %v), want (%v, %v)",
					got_len,
					got_ok,
					tt.want_len,
					tt.want_ok,
				)
			}
		})
	}
}

func TestBlockOffsetBounds(t *testing.T) {
	tests := []struct {
		name        string
		pieceLen    uint32
		blockLen    uint32
		blockIdx    uint32
		want_begin  uint32
		want_length uint32
		want_ok     bool
	}{
		{"zero pieceLen", 0, 16384, 0, 0, 0, false},
		{"first block", 32768, 16384, 0, 0, 16384, true},
		{"second block", 32768, 16384, 1, 16384, 16384, true},
		{"last block (not exact)", 32769, 16384, 2, 32768, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_begin, got_length, got_ok := BlockOffsetBounds(
				tt.pieceLen,
				tt.blockLen,
				tt.blockIdx,
			)
			if got_begin != tt.want_begin || got_length != tt.want_length ||
				got_ok != tt.want_ok {
				t.Errorf(
					"BlockOffsetBounds() = (%v, %v, %v), want (%v, %v, %v)",
					got_begin,
					got_length,
					got_ok,
					tt.want_begin,
					tt.want_length,
					tt.want_ok,
				)
			}
		})
	}
}

func TestBlockIndexForBegin(t *testing.T) {
	tests := []struct {
		name       string
		begin      uint32
		pieceLen   uint32
		want_index uint32
		want_ok    bool
	}{
		{"zero begin", 0, 32768, 0, true},
		{"in first block", 8192, 32768, 0, true},
		{"at boundary", 16384, 32768, 1, true},
		{"in second block", 24576, 32768, 1, true},
		{"out of bounds", 32768, 32768, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_index, got_ok := BlockIndexForBegin(tt.begin, tt.pieceLen)
			if got_index != tt.want_index || got_ok != tt.want_ok {
				t.Errorf(
					"BlockIndexForBegin() = (%v, %v), want (%v, %v)",
					got_index,
					got_ok,
					tt.want_index,
					tt.want_ok,
				)
			}
		})
	}
}

func TestBlocksInPiece(t *testing.T) {
	tests := []struct {
		name       string
		pieceLen   uint32
		want_count uint32
		want_ok    bool
	}{
		{"zero pieceLen", 0, 0, false},
		{"exact fit", 32768, 2, true},
		{"one extra byte", 32769, 3, true},
		{"less than one block", 8192, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_count, got_ok := BlocksInPiece(tt.pieceLen)
			if got_count != tt.want_count || got_ok != tt.want_ok {
				t.Errorf(
					"BlocksInPiece() = (%v, %v), want (%v, %v)",
					got_count,
					got_ok,
					tt.want_count,
					tt.want_ok,
				)
			}
		})
	}
}

func TestLastBlockInPiece(t *testing.T) {
	tests := []struct {
		name     string
		pieceLen uint32
		want_len uint32
		want_ok  bool
	}{
		{"zero pieceLen", 0, 0, false},
		{"exact fit", 32768, 16384, true},
		{"one extra byte", 32769, 1, true},
		{"less than one block", 8192, 8192, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_len, got_ok := LastBlockInPiece(tt.pieceLen)
			if got_len != tt.want_len || got_ok != tt.want_ok {
				t.Errorf(
					"LastBlockInPiece() = (%v, %v), want (%v, %v)",
					got_len,
					got_ok,
					tt.want_len,
					tt.want_ok,
				)
			}
		})
	}
}

func TestBlockBounds(t *testing.T) {
	tests := []struct {
		name        string
		pieceLen    uint32
		blockIdx    uint32
		want_begin  uint32
		want_length uint32
		want_ok     bool
	}{
		{"zero pieceLen", 0, 0, 0, 0, false},
		{"first block", 32768, 0, 0, 16384, true},
		{"second block", 32768, 1, 16384, 16384, true},
		{"last block (not exact)", 32769, 2, 32768, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got_begin, got_length, got_ok := BlockBounds(tt.pieceLen, tt.blockIdx)
			if got_begin != tt.want_begin || got_length != tt.want_length ||
				got_ok != tt.want_ok {
				t.Errorf(
					"BlockBounds() = (%v, %v, %v), want (%v, %v, %v)",
					got_begin,
					got_length,
					got_ok,
					tt.want_begin,
					tt.want_length,
					tt.want_ok,
				)
			}
		})
	}
}

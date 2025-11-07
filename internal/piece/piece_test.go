package piece

import (
	"crypto/sha1"
	"net/netip"
	"reflect"
	"testing"

	"github.com/prxssh/rabbit/pkg/bitfield"
)

func TestNewManager(t *testing.T) {
	tests := []struct {
		name          string
		pieceHashes   [][sha1.Size]byte
		pieceLen      uint32
		size          uint64
		expectedErr   bool
		expectedCount uint32
	}{
		{
			name: "valid arguments",
			pieceHashes: [][sha1.Size]byte{
				{},
				{},
			},
			pieceLen:      16384,
			size:          32768,
			expectedErr:   false,
			expectedCount: 2,
		},
		{
			name:          "invalid size",
			pieceHashes:   [][sha1.Size]byte{},
			pieceLen:      16384,
			size:          0,
			expectedErr:   true,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewManager(tt.pieceHashes, tt.pieceLen, tt.size)
			if (err != nil) != tt.expectedErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.expectedErr)
				return
			}
			if err == nil && mgr.PieceCount() != tt.expectedCount {
				t.Errorf(
					"NewManager() piece count = %v, want %v",
					mgr.PieceCount(),
					tt.expectedCount,
				)
			}
		})
	}
}

func TestPieceManager_PieceLength(t *testing.T) {
	pieceHashes := [][sha1.Size]byte{{0x1}}
	pieceLen := uint32(16384)
	size := uint64(16384)
	mgr, _ := NewManager(pieceHashes, pieceLen, size)

	if length := mgr.PieceLength(0); length != pieceLen {
		t.Errorf("PieceLength(0) = %v, want %v", length, pieceLen)
	}
}

func TestPieceManager_PieceHash(t *testing.T) {
	pieceHashes := [][sha1.Size]byte{{0x1}, {0x2}}
	pieceLen := uint32(16384)
	size := uint64(32768)
	mgr, _ := NewManager(pieceHashes, pieceLen, size)

	if hash := mgr.PieceHash(1); hash != pieceHashes[1] {
		t.Errorf("PieceHash(1) = %v, want %v", hash, pieceHashes[1])
	}
}

func TestPieceManager_PieceComplete(t *testing.T) {
	pieceHashes := [][sha1.Size]byte{{0x1}}
	pieceLen := uint32(16384)
	size := uint64(16384)
	mgr, _ := NewManager(pieceHashes, pieceLen, size)

	if complete := mgr.PieceComplete(0); complete {
		t.Errorf("PieceComplete(0) should be false initially")
	}
}

func TestPieceManager_MarkBlockComplete(t *testing.T) {
	pieceHashes := [][sha1.Size]byte{{0x1}}
	pieceLen := uint32(16384)
	size := uint64(16384)
	mgr, _ := NewManager(pieceHashes, pieceLen, size)
	peer := netip.MustParseAddrPort("1.2.3.4:5678")

	redundantPeers := mgr.MarkBlockComplete(peer, 0, 0)
	if redundantPeers != nil {
		t.Errorf("MarkBlockComplete should not return redundant peers initially")
	}

	piece := mgr.pieces[0]
	if piece.blocks[0].status != StatusDone {
		t.Errorf("Block status should be StatusDone")
	}
	if piece.doneBlocks != 1 {
		t.Errorf("doneBlocks should be 1")
	}
}

func TestPieceManager_MarkPieceVerified(t *testing.T) {
	pieceHashes := [][sha1.Size]byte{{0x1}}
	pieceLen := uint32(16384)
	size := uint64(16384)
	mgr, _ := NewManager(pieceHashes, pieceLen, size)

	mgr.MarkPieceVerified(0, true)
	piece := mgr.pieces[0]
	if !piece.verified {
		t.Errorf("Piece should be verified")
	}
	if piece.status != StatusDone {
		t.Errorf("Piece status should be StatusDone")
	}

	// Test re-verification
	mgr.MarkPieceVerified(0, false)
	if !piece.verified {
		t.Errorf("Piece should remain verified")
	}
}

func TestPieceManager_AssignAndUnassignBlock(t *testing.T) {
	pieceHashes := [][sha1.Size]byte{{0x1}}
	pieceLen := uint32(16384)
	size := uint64(16384)
	mgr, _ := NewManager(pieceHashes, pieceLen, size)
	peer := netip.MustParseAddrPort("5.6.7.8:1234")

	assigned := mgr.AssignBlock(peer, 0, 0)
	if !assigned {
		t.Errorf("AssignBlock should return true")
	}

	piece := mgr.pieces[0]
	block := piece.blocks[0]
	if block.status != StatusInflight {
		t.Errorf("Block status should be StatusInflight")
	}
	if len(block.owners) != 1 {
		t.Errorf("Block should have one owner")
	}

	mgr.UnassignBlock(peer, 0, 0)
	if block.status != StatusWant {
		t.Errorf("Block status should be StatusWant after unassign")
	}
	if len(block.owners) != 0 {
		t.Errorf("Block should have no owners after unassign")
	}
}

func TestPieceStatus(t *testing.T) {
	pieceHashes := [][sha1.Size]byte{{}, {}, {}}
	pieceLen := uint32(16384)
	size := uint64(49152)

	mgr, _ := NewManager(pieceHashes, pieceLen, size)
	mgr.pieces[0].status = StatusDone
	mgr.pieces[1].status = StatusInflight

	expectedStatus := []Status{StatusDone, StatusInflight, StatusWant}
	if !reflect.DeepEqual(mgr.PieceStatus(), expectedStatus) {
		t.Errorf("PieceStatus() = %v, want %v", mgr.PieceStatus(), expectedStatus)
	}
}

func TestAssignSequentialBlocks(t *testing.T) {
	pieceHashes := [][sha1.Size]byte{{0x1}, {0x2}, {0x3}}
	pieceLen := uint32(16384)
	size := uint64(49152)
	peer := netip.MustParseAddrPort("1.2.3.4:5678")
	bf := bitfield.New(3)
	bf.Set(0)
	bf.Set(1)
	bf.Set(2)
	mgr, _ := NewManager(pieceHashes, pieceLen, size)

	blocks, capacity := mgr.AssignSequentialBlocks(peer, bf, 5)
	if capacity != 4 {
		t.Errorf("Expected capacity to be 4, got %d", capacity)
	}
	if len(blocks) != 1 {
		t.Errorf("Expected to assign 1 block, got %d", len(blocks))
	}
	if blocks[0].PieceIdx != 0 {
		t.Errorf("Expected piece index to be 0, got %d", blocks[0].PieceIdx)
	}
	if owner := mgr.pieces[0].blocks[0].owners[0].peer; owner != peer {
		t.Errorf("Expected owner to be %v, got %v", peer, owner)
	}
}

func TestAssignInProgressBlocks(t *testing.T) {
	pieceHashes := [][sha1.Size]byte{{0x1}, {0x2}, {0x3}}
	pieceLen := uint32(16384)
	size := uint64(49152)
	peer := netip.MustParseAddrPort("1.2.3.4:5678")
	bf := bitfield.New(3)
	bf.Set(0)
	bf.Set(1)
	bf.Set(2)
	mgr, _ := NewManager(pieceHashes, pieceLen, size)
	mgr.pieces[0].doneBlocks = 1 // Mark one block as done to make the piece "in progress"

	blocks, capacity := mgr.AssignInProgressBlocks(peer, bf, 5)
	if capacity != 4 {
		t.Errorf("Expected capacity to be 4, got %d", capacity)
	}
	if len(blocks) != 1 {
		t.Errorf("Expected to assign 1 block, got %d", len(blocks))
	}
	if blocks[0].PieceIdx != 0 {
		t.Errorf("Expected piece index to be 0, got %d", blocks[0].PieceIdx)
	}
	if owner := mgr.pieces[0].blocks[0].owners[0].peer; owner != peer {
		t.Errorf("Expected owner to be %v, got %v", peer, owner)
	}
}

func TestAssignEndgameBlocks(t *testing.T) {
	pieceHashes := [][sha1.Size]byte{{0x1}, {0x2}, {0x3}}
	pieceLen := uint32(16384)
	size := uint64(49152)
	peer1 := netip.MustParseAddrPort("1.2.3.4:5678")
	peer2 := netip.MustParseAddrPort("1.2.3.4:5679")
	bf := bitfield.New(3)
	bf.Set(0)
	bf.Set(1)
	bf.Set(2)
	mgr, _ := NewManager(pieceHashes, pieceLen, size)

	blocks, capacity := mgr.AssignEndgameBlocks(peer1, bf, 5, 2)
	if capacity != 2 {
		t.Errorf("Expected capacity to be 2, got %d", capacity)
	}
	if len(blocks) != 3 {
		t.Errorf("Expected to assign 3 blocks, got %d", len(blocks))
	}
	for i := 0; i < 3; i++ {
		if owner := mgr.pieces[i].blocks[0].owners[0].peer; owner != peer1 {
			t.Errorf("Expected owner to be %v, got %v", peer1, owner)
		}
	}

	blocks, capacity = mgr.AssignEndgameBlocks(peer2, bf, 5, 2)
	if capacity != 2 {
		t.Errorf("Expected capacity to be 2, got %d", capacity)
	}
	if len(blocks) != 3 {
		t.Errorf("Expected to assign 3 blocks, got %d", len(blocks))
	}

	for i := 0; i < 3; i++ {
		if len(mgr.pieces[i].blocks[0].owners) != 2 {
			t.Errorf(
				"Expected owners to be 2, got %d",
				len(mgr.pieces[i].blocks[0].owners),
			)
		}
	}
}

func TestAssignBlocksFromList(t *testing.T) {
	pieceHashes := [][sha1.Size]byte{{0x1}, {0x2}, {0x3}}
	pieceLen := uint32(16384)
	size := uint64(49152)
	peer := netip.MustParseAddrPort("1.2.3.4:5678")
	mgr, _ := NewManager(pieceHashes, pieceLen, size)

	blocks, capacity := mgr.AssignBlocksFromList(peer, []uint32{1, 2}, 5)
	if capacity != 3 {
		t.Errorf("Expected capacity to be 3, got %d", capacity)
	}
	if len(blocks) != 2 {
		t.Errorf("Expected to assign 2 blocks, got %d", len(blocks))
	}
	if blocks[0].PieceIdx != 1 {
		t.Errorf("Expected piece index to be 1, got %d", blocks[0].PieceIdx)
	}
	if owner := mgr.pieces[1].blocks[0].owners[0].peer; owner != peer {
		t.Errorf("Expected owner to be %v, got %v", peer, owner)
	}
	if owner := mgr.pieces[2].blocks[0].owners[0].peer; owner != peer {
		t.Errorf("Expected owner to be %v, got %v", peer, owner)
	}
}

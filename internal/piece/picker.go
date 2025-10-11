package piece

import (
	"crypto/sha1"
	"net/netip"
	"sync"
	"time"

	"github.com/prxssh/rabbit/internal/config"
	"github.com/prxssh/rabbit/internal/utils/bitfield"
)

type Cancel struct {
	Peer   netip.AddrPort
	Piece  int
	Begin  int
	Length int
}

type PeerView struct {
	Addr     netip.AddrPort
	Bitfield bitfield.Bitfield
	Unchoked bool
}

type Request struct {
	Piece  int
	Begin  int
	Length int
}

type PieceState int

const (
	PieceStateNotStarted PieceState = 0
	PieceStateInProgress PieceState = 1
	PieceStateCompleted  PieceState = 2
)

func (pk *Picker) PieceStates() []PieceState {
	pk.mu.RLock()
	defer pk.mu.RUnlock()

	states := make([]PieceState, pk.PieceCount)
	for i, p := range pk.pieces {
		if p.verified {
			states[i] = PieceStateCompleted
		} else if p.doneBlocks > 0 {
			states[i] = PieceStateInProgress
		} else {
			states[i] = PieceStateNotStarted
		}
	}
	return states
}

type Picker struct {
	mu               sync.RWMutex
	LastPieceLen     int32
	PieceCount       int
	pieces           []*pieceState
	availability     *availabilityBucket
	nextPiece        int
	nextBlock        int
	endgame          bool
	remainingBlocks  int
	bitfield         bitfield.Bitfield
	inflightRequests int

	peerMu               sync.RWMutex
	peerInflightCount    map[netip.AddrPort]int
	peerBitfields        map[netip.AddrPort]bitfield.Bitfield
	peerBlockAssignments map[netip.AddrPort]map[uint64]struct{}
}

func NewPicker(size int64, pieceLength int32, pieceHashes [][sha1.Size]byte) *Picker {
	n := len(pieceHashes)
	availability := newAvailabilityBucket(n)

	totalBlocks := 0
	lastPieceLen := LastPieceLength(size, pieceLength)
	pieces := make([]*pieceState, n)

	for i := 0; i < n; i++ {
		plen, _ := PieceLengthAt(i, size, pieceLength)
		blockCount := BlocksInPiece(plen)
		totalBlocks += blockCount
		blocks := make([]*block, blockCount)

		for j := 0; j < blockCount; j++ {
			blocks[j] = &block{status: blockWant}
		}

		pieces[i] = &pieceState{
			index:       i,
			doneBlocks:  0,
			length:      plen,
			verified:    false,
			blocks:      blocks,
			isLastPiece: i == n-1,
			blockCount:  blockCount,
			sha:         pieceHashes[i],
			lastBlock:   LastBlockInPiece(plen),
		}
	}

	return &Picker{
		nextPiece:            0,
		nextBlock:            0,
		PieceCount:           n,
		endgame:              false,
		pieces:               pieces,
		remainingBlocks:      totalBlocks,
		availability:         availability,
		LastPieceLen:         lastPieceLen,
		bitfield:             bitfield.New(n),
		peerInflightCount:    make(map[netip.AddrPort]int),
		peerBitfields:        make(map[netip.AddrPort]bitfield.Bitfield),
		peerBlockAssignments: make(map[netip.AddrPort]map[uint64]struct{}),
	}
}

func (pk *Picker) OnPeerBitfield(peer netip.AddrPort, bf bitfield.Bitfield) {
	pk.peerMu.Lock()
	pk.peerBitfields[peer] = bf
	pk.peerMu.Unlock()

	pk.updatePieceAvailability(bf, 1)
}

func (pk *Picker) OnPeerHave(peer netip.AddrPort, piece int) {
	if piece < 0 || piece >= pk.PieceCount {
		return
	}

	pk.peerMu.Lock()
	defer pk.peerMu.Unlock()

	peerBF, ok := pk.peerBitfields[peer]
	if !ok {
		peerBF = bitfield.New(pk.PieceCount)
	}
	if peerBF.Has(piece) {
		return
	}

	peerBF.Set(piece)
	pk.peerBitfields[peer] = peerBF
	pk.availability.Move(piece, 1)
}

func (pk *Picker) OnPeerGone(peer netip.AddrPort) {
	pk.peerMu.Lock()
	peerBF, ok := pk.peerBitfields[peer]
	if ok {
		peerBF = peerBF.Clone()
	}

	assignments := pk.peerBlockAssignments[peer]
	keys := make([]uint64, 0, len(assignments))

	for k := range assignments {
		keys = append(keys, k)
	}
	pk.peerMu.Unlock()

	for _, key := range keys {
		piece := int(key >> 32)
		begin := int(key & 0xFFFFFFFF)

		pk.mu.Lock()
		ps := pk.pieces[piece]
		blockIdx := BlockIndexForBegin(begin, int(ps.length), BlockLength)
		pk.resetBlockToWant(piece, blockIdx)
		pk.mu.Unlock()
	}

	if ok {
		pk.updatePieceAvailability(peerBF, -1)
	}
	pk.cleanupPeerState(peer)
}

func (pk *Picker) CheckTimeouts(timeout time.Duration) []*Request {
	pk.mu.Lock()
	defer pk.mu.Unlock()

	var timeouts []*Request
	now := time.Now()

	for _, p := range pk.pieces {
		if p.verified {
			continue
		}

		for bi, blk := range p.blocks {
			if blk.status != blockInflight || blk.owner == nil ||
				blk.owner.requestedAt.IsZero() {
				continue
			}

			if now.Sub(blk.owner.requestedAt) <= timeout {
				continue
			}

			begin, length := getBlockInfo(p, bi)
			timeouts = append(
				timeouts,
				&Request{Piece: p.index, Begin: begin, Length: length},
			)

			pk.unassignBlockFromPeer(blk.owner.addr, p.index, begin)
			blk.owner = nil
			blk.status = blockWant
			pk.inflightRequests--
		}
	}

	return timeouts
}

func (pk *Picker) OnBlockReceived(peer netip.AddrPort, piece, begin int) {
	pk.unassignBlockFromPeer(peer, piece, begin)

	pk.mu.Lock()
	defer pk.mu.Unlock()

	if piece < 0 || piece >= pk.PieceCount {
		return
	}

	ps := pk.pieces[piece]
	blockIdx := BlockIndexForBegin(begin, int(ps.length), BlockLength)
	if blockIdx < 0 || blockIdx >= ps.blockCount {
		return
	}

	blk := ps.blocks[blockIdx]
	if blk.status == blockInflight {
		blk.status = blockDone
		ps.doneBlocks++
		if pk.inflightRequests > 0 {
			pk.inflightRequests--
		}
		if pk.remainingBlocks > 0 {
			pk.remainingBlocks--
		}
	}

	if pk.remainingBlocks <= config.Load().EndgameThreshold {
		pk.endgame = true
	}
}

func (pk *Picker) findAvailableBlock(piece *pieceState, peer netip.AddrPort) (int, bool) {
	for j := 0; j < piece.blockCount; j++ {
		blk := piece.blocks[j]
		begin := j * BlockLength

		if blk.status == blockWant && !pk.isBlockAssignedtoPeer(peer, piece.index, begin) {
			return j, true
		}
	}
	return -1, false
}

func (pk *Picker) resetBlockToWant(piece int, blockIdx int) {
	if !pk.isValidPiece(piece) {
		return
	}

	p := pk.pieces[piece]
	if blockIdx >= 0 && blockIdx < len(p.blocks) {
		block := p.blocks[blockIdx]
		if block.status == blockInflight {
			block.status = blockWant
			pk.inflightRequests--
		}
	}
}

func (pk *Picker) assignBlockToPeer(peer netip.AddrPort, piece, begin int) {
	key := blockKey(piece, begin)

	pk.peerMu.Lock()
	defer pk.peerMu.Unlock()

	if pk.peerBlockAssignments[peer] == nil {
		pk.peerBlockAssignments[peer] = make(map[uint64]struct{})
	}

	pk.peerBlockAssignments[peer][key] = struct{}{}
	pk.peerInflightCount[peer]++
}

func (pk *Picker) unassignBlockFromPeer(peer netip.AddrPort, piece, begin int) {
	key := blockKey(piece, begin)

	pk.peerMu.Lock()
	defer pk.peerMu.Unlock()

	if assignments, ok := pk.peerBlockAssignments[peer]; ok {
		delete(assignments, key)
		if len(assignments) == 0 {
			delete(pk.peerBlockAssignments, peer)
		}
	}

	if count, ok := pk.peerInflightCount[peer]; ok {
		if count > 0 {
			pk.peerInflightCount[peer]--
		}
		if pk.peerInflightCount[peer] == 0 {
			delete(pk.peerInflightCount, peer)
		}
	}
}

func (pk *Picker) getPeerInflightCount(peer netip.AddrPort) int {
	pk.peerMu.RLock()
	defer pk.peerMu.RUnlock()

	return pk.peerInflightCount[peer]
}

func (pk *Picker) isBlockAssignedtoPeer(peer netip.AddrPort, piece, begin int) bool {
	key := blockKey(piece, begin)

	pk.peerMu.RLock()
	defer pk.peerMu.RUnlock()

	if assignments, ok := pk.peerBlockAssignments[peer]; ok {
		_, assigned := assignments[key]
		return assigned
	}

	return false
}

func (pk *Picker) cleanupPeerState(peer netip.AddrPort) {
	pk.peerMu.Lock()
	defer pk.peerMu.Unlock()

	delete(pk.peerBlockAssignments, peer)
	delete(pk.peerInflightCount, peer)
	delete(pk.peerBitfields, peer)
}

func (pk *Picker) updatePieceAvailability(peerBF bitfield.Bitfield, delta int) {
	pk.mu.RLock()
	weHave := pk.bitfield.Clone()
	pk.mu.RUnlock()

	for i := 0; i < pk.PieceCount; i++ {
		if peerBF.Has(i) && !weHave.Has(i) {
			pk.availability.Move(i, delta)
		}
	}
}

// isValidPiece checks if a piece index is valid and not already completed
func (pk *Picker) isValidPiece(piece int) bool {
	if piece < 0 || piece >= pk.PieceCount {
		return false
	}

	return !pk.bitfield.Has(piece) && !pk.pieces[piece].verified
}

func (pk *Picker) createRequest(peer netip.AddrPort, p *pieceState, blockIdx int) *Request {
	begin, length := getBlockInfo(p, blockIdx)

	if p.blocks[blockIdx].status == blockWant {
		p.blocks[blockIdx].status = blockInflight
		p.blocks[blockIdx].owner = &blockOwner{addr: peer, requestedAt: time.Now()}
		pk.inflightRequests++
	}

	pk.assignBlockToPeer(peer, p.index, begin)
	return &Request{Piece: p.index, Begin: begin, Length: length}
}

func (pk *Picker) peerCapacity(peer netip.AddrPort) int {
	pk.peerMu.RLock()
	used := pk.peerInflightCount[peer]
	pk.peerMu.RUnlock()

	return max(0, config.Load().MaxInflightRequestsPerPeer-used)
}

// blockKey generates a unique key for a block (piece, begin)
func blockKey(piece, begin int) uint64 {
	return uint64(piece)<<32 | uint64(begin)
}

func getBlockInfo(piece *pieceState, blockIdx int) (begin, length int) {
	begin = blockIdx * BlockLength
	length = BlockLength
	if blockIdx == piece.blockCount-1 && piece.lastBlock > 0 {
		length = int(piece.lastBlock)
	}

	return
}

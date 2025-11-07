package piece

import (
	"crypto/sha1"
	"errors"
	"net/netip"
	"sync"
	"time"
)

const MaxBlockLength = 16 * 1024 // 16KB

type Status uint8

const (
	StatusWant Status = iota
	StatusInflight
	StatusDone
)

type blockOwner struct {
	peer        netip.AddrPort
	requestedAt time.Time
}

type block struct {
	requests uint32
	status   Status
	owners   []*blockOwner
}

type piece struct {
	index         uint32
	status        Status
	length        uint32
	blockCount    uint32
	lastBlockSize uint32
	doneBlocks    uint32
	verified      bool
	blocks        []*block
	hash          [sha1.Size]byte
}

type Manager struct {
	mut             sync.RWMutex
	pieces          []*piece
	pieceCount      uint32
	nextPiece       uint32
	nextBlock       uint32
	remainingBlocks uint32
	lastPieceLength uint32
	blockCount      uint32
}

func NewManager(pieceHashes [][sha1.Size]byte, pieceLen uint32, size uint64) (*Manager, error) {
	lastPieceLen, ok := LastPieceLength(size, pieceLen)
	if !ok {
		return nil, errors.New("out of bounds")
	}

	n := len(pieceHashes)
	pieces := make([]*piece, n)
	totalBlocks := uint32(0)

	for i := 0; i < n; i++ {
		currPieceLen, _ := PieceLengthAt(uint32(i), size, pieceLen)
		blockCount, _ := BlocksInPiece(currPieceLen)
		blocks := make([]*block, blockCount)
		totalBlocks += blockCount

		for j := 0; j < int(blockCount); j++ {
			blocks[j] = &block{
				status: StatusWant,
				owners: make([]*blockOwner, 0, 2),
			}
		}

		lastBlockLen, _ := LastBlockInPiece(currPieceLen)

		pieces[i] = &piece{
			index:         uint32(i),
			doneBlocks:    0,
			status:        StatusWant,
			length:        currPieceLen,
			verified:      false,
			blocks:        blocks,
			blockCount:    blockCount,
			hash:          pieceHashes[i],
			lastBlockSize: lastBlockLen,
		}
	}

	return &Manager{
		pieces:          pieces,
		nextPiece:       0,
		nextBlock:       0,
		pieceCount:      uint32(n),
		remainingBlocks: totalBlocks,
		lastPieceLength: lastPieceLen,
	}, nil
}

func (m *Manager) PieceCount() uint32 {
	m.mut.RLock()
	defer m.mut.RUnlock()

	return m.pieceCount
}

func (m *Manager) PieceLength(pieceIdx uint32) uint32 {
	m.mut.RLock()
	defer m.mut.RLock()

	return m.pieces[pieceIdx].length
}

func (m *Manager) PieceHash(pieceIdx uint32) [sha1.Size]byte {
	m.mut.RLock()
	defer m.mut.RUnlock()

	return m.pieces[pieceIdx].hash
}

func (m *Manager) PieceStatus() []Status {
	m.mut.RLock()
	defer m.mut.RUnlock()

	states := make([]Status, m.pieceCount)
	for i, piece := range m.pieces {
		states[i] = piece.status
	}

	return states
}

func (m *Manager) MarkBlockComplete(peer netip.AddrPort, pieceIdx, begin uint32) []netip.AddrPort {
	m.mut.Lock()
	defer m.mut.Unlock()

	piece := m.pieces[pieceIdx]
	blockIdx, _ := BlockIndexForBegin(begin, piece.length)
	block := piece.blocks[blockIdx]
	if block.status == StatusDone {
		return nil
	}
	block.status = StatusDone
	piece.doneBlocks++

	redundantPeers := make([]netip.AddrPort, 0, len(block.owners))
	for i := range block.owners {
		if block.owners[i].peer != peer {
			redundantPeers = append(redundantPeers, block.owners[i].peer)
		}
	}
	block.owners = nil

	return redundantPeers
}

func (m *Manager) MarkPieceVerified(pieceIdx uint32, ok bool) {
	m.mut.Lock()
	defer m.mut.Unlock()

	piece := m.pieces[pieceIdx]
	if piece.verified {
		return
	}

	if ok {
		piece.verified = true
		piece.status = StatusDone

		if m.nextPiece == pieceIdx {
			m.nextPiece++
			m.nextBlock = 0
		}

		return
	}

	for b := 0; b < int(piece.blockCount); b++ {
		if piece.blocks[b].status == StatusDone {
			m.remainingBlocks++
		}

		piece.blocks[b].status = StatusWant
		piece.blocks[b].owners = nil
	}

	piece.doneBlocks = 0
	piece.status = StatusWant
}

func (m *Manager) AssignBlock(peer netip.AddrPort, pieceIdx, blockIdx int32) {
	m.mut.Lock()
	defer m.mut.Unlock()

	piece := m.pieces[pieceIdx]
	block := piece.blocks[blockIdx]

	block.status = StatusInflight
	block.owners = append(block.owners, &blockOwner{
		peer:        peer,
		requestedAt: time.Now(),
	})

	m.remainingBlocks--
}

func (m *Manager) UnassignBlock(peer netip.AddrPort, pieceIdx, begin uint32) {
	m.mut.Lock()
	defer m.mut.Unlock()

	if pieceIdx >= m.pieceCount {
		return
	}

	piece := m.pieces[pieceIdx]
	blockIdx, ok := BlockIndexForBegin(begin, piece.length)
	if !ok {
		return
	}
	block := piece.blocks[blockIdx]
	n := len(block.owners)

	for i := 0; i < n; i++ {
		if block.owners[i].peer == peer {
			block.owners[i] = block.owners[n-1]
			block.owners = block.owners[:n-1]

			m.remainingBlocks++
			break
		}
	}

	if len(block.owners) == 0 && block.status != StatusDone {
		block.status = StatusWant
	}
}

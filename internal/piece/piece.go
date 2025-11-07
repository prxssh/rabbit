package piece

import (
	"crypto/sha1"
	"errors"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/prxssh/rabbit/pkg/bitfield"
)

const MaxBlockLength = 16 * 1024 // 16KB

type BlockInfo struct {
	PieceIdx uint32
	Begin    uint32
	Length   uint32
}

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
	logger          *slog.Logger
	mut             sync.RWMutex
	pieces          []*piece
	pieceCount      uint32
	nextPiece       uint32
	nextBlock       uint32
	remainingBlocks uint32
	lastPieceLength uint32
	blockCount      uint32
}

// TODO: check timeouts and free blocks
func NewManager(
	pieceHashes [][sha1.Size]byte,
	pieceLen uint32,
	size uint64,
	logger *slog.Logger,
) (*Manager, error) {
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
		logger:          logger,
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

func (m *Manager) ResetSequentialState() {
	m.mut.Lock()
	defer m.mut.Unlock()

	m.nextPiece = 0
	m.nextBlock = 0

	for m.nextPiece < m.pieceCount && m.pieces[m.nextPiece].verified {
		m.nextPiece++
	}
}

func (m *Manager) PieceLength(pieceIdx uint32) uint32 {
	m.mut.RLock()
	defer m.mut.RUnlock()

	return m.pieces[pieceIdx].length
}

func (m *Manager) PieceHash(pieceIdx uint32) [sha1.Size]byte {
	m.mut.RLock()
	defer m.mut.RUnlock()

	return m.pieces[pieceIdx].hash
}

func (m *Manager) PieceComplete(pieceIdx uint32) bool {
	m.mut.Lock()
	defer m.mut.Unlock()

	piece := m.pieces[pieceIdx]
	return piece.doneBlocks == piece.blockCount
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

	var redundantPeers []netip.AddrPort
	for i := range block.owners {
		if block.owners[i].peer != peer {
			redundantPeers = append(redundantPeers, block.owners[i].peer)
		}
	}
	block.owners = nil

	return redundantPeers
}

func (m *Manager) MarkPieceVerified(pieceIdx uint32, ok bool) {
	m.logger.Debug("mark piece verified called", "piece", pieceIdx)

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

func (m *Manager) AssignBlock(peer netip.AddrPort, pieceIdx, blockIdx uint32) bool {
	m.mut.Lock()
	defer m.mut.Unlock()

	_, ok := m.safeAssignBlock(peer, pieceIdx, blockIdx, 1)
	return ok
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

func (m *Manager) AssignInProgressBlocks(
	peer netip.AddrPort,
	peerBF bitfield.Bitfield,
	capacity uint32,
) ([]*BlockInfo, uint32) {
	m.mut.Lock()
	defer m.mut.Unlock()

	assigned := make([]*BlockInfo, 0, capacity)

	for i := uint32(0); i < m.pieceCount && capacity > 0; i++ {
		piece := m.pieces[i]
		if piece.verified || piece.doneBlocks == 0 || !peerBF.Has(int(piece.index)) {
			continue
		}

		for j := uint32(0); j < piece.blockCount && capacity > 0; j++ {
			if piece.blocks[j].status != StatusWant {
				continue
			}

			if block, ok := m.safeAssignBlock(peer, i, j, 1); ok {
				assigned = append(assigned, block)
				capacity--
			}

			break
		}
	}

	return assigned, capacity
}

func (m *Manager) AssignEndgameBlocks(
	peer netip.AddrPort,
	peerBF bitfield.Bitfield,
	capacity, duplicateLimit uint32,
) ([]*BlockInfo, uint32) {
	m.mut.Lock()
	defer m.mut.Unlock()

	assigned := make([]*BlockInfo, 0, capacity)

	for i := 0; i < int(m.pieceCount) && capacity > 0; i++ {
		piece := m.pieces[i]
		if piece.verified || !peerBF.Has(i) {
			continue
		}

		for j := 0; j < int(piece.blockCount) && capacity > 0; j++ {
			if piece.blocks[j].status == StatusDone {
				continue
			}

			if block, ok := m.safeAssignBlock(peer, uint32(i), uint32(j), duplicateLimit); ok {
				assigned = append(assigned, block)
				capacity--
			}
		}
	}

	return assigned, capacity
}

func (m *Manager) AssignSequentialBlocks(
	peer netip.AddrPort,
	peerBF bitfield.Bitfield,
	capacity uint32,
) ([]*BlockInfo, uint32) {
	m.mut.Lock()
	defer m.mut.Unlock()

	assigned := make([]*BlockInfo, 0, capacity)

	for m.nextPiece < m.pieceCount && capacity > 0 {
		// Skip verified pieces
		for m.nextPiece < m.pieceCount && m.pieces[m.nextPiece].verified {
			m.nextPiece++
			m.nextBlock = 0
		}

		if m.nextPiece >= m.pieceCount {
			break
		}

		if !peerBF.Has(int(m.nextPiece)) {
			m.nextPiece++
			m.nextBlock = 0
			continue
		}

		piece := m.pieces[m.nextPiece]
		for bi := m.nextBlock; bi < piece.blockCount && capacity > 0; bi++ {
			block, ok := m.safeAssignBlock(peer, piece.index, bi, 1)
			if ok {
				assigned = append(assigned, block)
				capacity--
				m.nextBlock = bi + 1
			}
		}

		if m.nextBlock >= piece.blockCount {
			m.nextPiece++
			m.nextBlock = 0
		}

		break
	}

	return assigned, capacity
}

func (m *Manager) AssignBlocksFromList(
	peer netip.AddrPort,
	pieceIndices []uint32,
	capacity uint32,
) ([]*BlockInfo, uint32) {
	m.mut.Lock()
	defer m.mut.Unlock()

	assigned := make([]*BlockInfo, 0, capacity)

	for _, pieceIdx := range pieceIndices {
		if capacity < 1 {
			break
		}

		if pieceIdx >= m.pieceCount || m.pieces[pieceIdx].verified {
			continue
		}

		piece := m.pieces[pieceIdx]

		for blockIdx := uint32(0); blockIdx < piece.blockCount; blockIdx++ {
			block, ok := m.safeAssignBlock(peer, piece.index, blockIdx, 1)
			if ok {
				assigned = append(assigned, block)
				capacity--
				break
			}
		}
	}

	return assigned, capacity
}

func (m *Manager) safeAssignBlock(
	peer netip.AddrPort,
	pieceIdx, blockIdx uint32,
	duplicateLimit uint32,
) (*BlockInfo, bool) {
	piece := m.pieces[pieceIdx]
	block := piece.blocks[blockIdx]

	begin, length, ok := BlockBounds(piece.length, blockIdx)
	if !ok {
		return nil, false
	}

	if len(block.owners) >= int(duplicateLimit) {
		return nil, false
	}

	piece.status = StatusInflight
	block.status = StatusInflight
	block.owners = append(block.owners, &blockOwner{
		peer:        peer,
		requestedAt: time.Now(),
	})
	m.remainingBlocks--

	return &BlockInfo{
		PieceIdx: pieceIdx,
		Begin:    begin,
		Length:   length,
	}, true
}

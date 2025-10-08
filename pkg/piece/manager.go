package piece

import (
	"crypto/sha1"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/prxssh/rabbit/pkg/config"
	"github.com/prxssh/rabbit/pkg/utils/bitfield"
)

// Manager coordinates between the piece picker (which decides what to download
// next) and the storage layer (which handles verified writes to disk).
//
// The manager is responsible for piece lifecycle management—buffering blocks,
// verifying hashes, committing data to disk, and exposing high-level controls
// like pause, resume, or recheck.
type Manager struct {
	picker      *Picker
	store       *Store
	torrentSize int64
	log         *slog.Logger
}

// Timeout represents a single inflight block that exceeded the timeout.
type Timeout struct {
	Peer  netip.AddrPort
	Piece int
	Begin int
}

// NewPieceManager creates a Manager that coordinates piece picking and disk
// I/O.
func NewPieceManager(
	torrentName string,
	torrentSize, pieceLength int64,
	pieceHashes [][sha1.Size]byte,
	paths [][]string,
	lens []int64,
	log *slog.Logger,
) (*Manager, error) {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("src", "piece_manager")

	store, err := NewStore(
		config.Load().DefaultDownloadDir,
		torrentName,
		paths,
		lens,
		pieceLength,
		log,
	)
	if err != nil {
		log.Error("failed to create store", "error", err)
		return nil, err
	}

	picker := NewPicker(torrentSize, pieceLength, pieceHashes)

	log.Info(
		"piece manager initialized",
		"pieces", len(pieceHashes),
		"piece_length", pieceLength,
		"total_size", torrentSize,
	)

	return &Manager{
		picker:      picker,
		store:       store,
		torrentSize: torrentSize,
		log:         log,
	}, nil
}

func (m *Manager) Close() error {
	return m.store.Close()
}

func (m *Manager) Bitfield() bitfield.Bitfield {
	return m.picker.bitfield
}

// OnBlockReceived handles the complete workflow when a block arrives:
//
// 1. Notify picker (get cancellations if endgame)
// 2. Buffer the block in store
// 3. If piece is complete, verify and flush to disk
// 4. Update picker with verification result
func (m *Manager) OnBlockReceived(
	peer netip.AddrPort,
	pieceIdx, begin int,
	data []byte,
) (pieceComplete bool, cancels []Cancel, err error) {
	complete, cancels := m.picker.OnBlockReceived(peer, pieceIdx, begin)

	blockIdx := BlockIndexForBegin(
		begin,
		m.picker.pieces[pieceIdx].length,
		m.picker.BlockLength,
	)
	m.store.BufferBlock(data, BlockInfo{
		PieceIndex:  pieceIdx,
		BlockIndex:  blockIdx,
		PieceLength: m.picker.pieces[pieceIdx].length,
		BlockLength: m.picker.BlockLength,
		IsLastPiece: m.picker.pieces[pieceIdx].isLastPiece,
		TotalSize:   m.torrentSize,
	})

	if !complete {
		return false, cancels, nil
	}

	hash := m.picker.PieceHash(pieceIdx)
	ok, err := m.store.FlushPiece(pieceIdx, hash)
	if err != nil {
		m.log.Error(
			"piece flush failed",
			"piece", pieceIdx,
			"error", err,
		)
		return true, cancels, err
	}

	if ok {
		m.log.Info(
			"piece verified",
			"piece", pieceIdx,
			"peer", peer.String(),
		)
	} else {
		m.log.Warn(
			"piece verification failed",
			"piece", pieceIdx,
			"peer", peer.String(),
		)
	}

	m.picker.MarkPieceVerified(pieceIdx, ok)

	return true, cancels, nil
}

func (m *Manager) NextForPeerN(pv *PeerView, count int) []*Request {
	return m.picker.NextForPeerN(pv, count)
}

func (m *Manager) HasAnyWantedPiece(bf bitfield.Bitfield) bool {
	return m.picker.HasAnyWantedPiece(bf)
}

func (m *Manager) OnPeerGone(peer netip.AddrPort, bf bitfield.Bitfield) {
	m.log.Debug(
		"peer disconnected",
		"peer", peer.String(),
		"pieces_had", bf.Count(),
	)
	m.picker.OnPeerGone(peer, bf)
}

func (m *Manager) OnPeerBitfield(peer netip.AddrPort, bf bitfield.Bitfield) {
	m.picker.OnPeerBitfield(peer, bf)
}

func (m *Manager) OnPeerHave(peer netip.AddrPort, piece uint32) {
	m.picker.OnPeerHave(peer, int(piece))
}

func (m *Manager) OnTimeout(peer netip.AddrPort, pieceIdx, begin int) {
	m.picker.OnTimeout(peer, pieceIdx, begin)
}

func (m *Manager) PieceStates() []PieceState {
	return m.picker.PieceStates()
}

func (m *Manager) CurrentPieceIndex() (int, bool) {
	return m.picker.CurrentPieceIndex()
}

func (m *Manager) ReadPiece(index, begin, length int) ([]byte, error) {
	pieceLen, err := PieceLengthAt(
		index,
		m.store.totalBytes,
		m.store.pieceLength,
	)
	if err != nil {
		return nil, err
	}

	if begin < 0 || length <= 0 || begin+length > pieceLen {
		return nil, fmt.Errorf(
			"invalid request: index=%d begin=%d length=%d pieceLen=%d",
			index,
			begin,
			length,
			pieceLen,
		)
	}

	if bi := BlockIndexForBegin(begin, pieceLen, BlockLength); bi >= 0 {
		expBegin, expLen, _ := BlockOffsetBounds(
			pieceLen,
			BlockLength,
			bi,
		)
		finalBlock := bi == BlockCountForPiece(pieceLen, BlockLength)-1
		if !finalBlock && (begin != expBegin || length != expLen) {
			return nil, fmt.Errorf(
				"non-canonical block; want begin=%d len=%d for block=%d",
				expBegin,
				expLen,
				bi,
			)
		}
	}

	start, _, _ := PieceOffsetBounds(
		index,
		m.store.totalBytes,
		m.store.pieceLength,
	)
	streamOff := start + int64(begin)

	buf := make([]byte, length)
	if err := m.store.readStreamAt(buf, streamOff); err != nil {
		m.log.Error(
			"failed to read piece",
			"piece", index,
			"begin", begin,
			"length", length,
			"error", err,
		)
		return nil, err
	}
	return buf, nil
}

func (m *Manager) CapacityForPeer(peer netip.AddrPort) int {
	return m.picker.CapacityForPeer(peer)
}

// ScanAndReclaimTimedOutBlocks checks for blocks that have been inflight too
// long and reclaims them so they can be reassigned to other peers.
func (m *Manager) ScanAndReclaimTimedOutBlocks(timeout time.Duration) []Timeout {
	timedOut := m.picker.ScanTimedOutBlocks(timeout)

	if len(timedOut) > 0 {
		m.log.Info("timed out blocks", "timed_out_blocks_count", len(timedOut))
	}

	for _, to := range timedOut {
		m.log.Debug(
			"block timeout, reclaiming",
			"peer", to.Peer.String(),
			"piece", to.Piece,
			"begin", to.Begin,
		)
		m.picker.OnTimeout(to.Peer, to.Piece, to.Begin)
	}

	outs := make([]Timeout, 0, len(timedOut))
	for _, to := range timedOut {
		outs = append(outs, Timeout{Peer: to.Peer, Piece: to.Piece, Begin: to.Begin})
	}
	return outs
}

func (m *Manager) OnCancel(peer netip.AddrPort, pieceIdx, begin int) {
	m.picker.OnTimeout(peer, pieceIdx, begin)
}

func (m *Manager) Unassign(peer netip.AddrPort, pieceIdx, begin int) {
	m.picker.Unassign(peer, pieceIdx, begin)
}

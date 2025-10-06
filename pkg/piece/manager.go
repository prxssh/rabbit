package piece

import (
	"crypto/sha1"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"

	"github.com/prxssh/rabbit/pkg/utils/bitfield"
)

// Config defines runtime options for the piece manager.
//
// It includes picker behavior (via PickerConfig) and the root directory where
// torrent data is stored on disk. The manager uses these settings to control
// piece selection, verification, and file layout.
type Config struct {
	*PickerConfig

	// DownloadDir represents the base directory where all the torrent data
	// is saved.
	DownloadDir string
}

func DefaultConfig() Config {
	rootDir := "."

	home, err := os.UserHomeDir()
	if err != nil {
		if cwd, err := os.Getwd(); err != nil {
			rootDir = filepath.Join(cwd, "downloads")
		}
	}

	switch os := runtime.GOOS; os {
	case "windows":
		rootDir = filepath.Join(home, "Downloads", "rabbit")
	default:
		rootDir = filepath.Join(
			home,
			".local",
			"share",
			"rabbit",
			"downloads",
		)
	}

	pickerCfg := DefaultPickerConfig()
	return Config{PickerConfig: &pickerCfg, DownloadDir: rootDir}
}

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
}

// NewPieceManager creates a Manager that coordinates piece picking and disk
// I/O.
func NewPieceManager(
	torrentName string,
	torrentSize, pieceLength int64,
	pieceHashes [][sha1.Size]byte,
	paths [][]string,
	lens []int64,
	cfg *Config,
) (*Manager, error) {
	if cfg == nil {
		temp := DefaultConfig()
		cfg = &temp
	}

	store, err := NewStore(
		cfg.DownloadDir,
		torrentName,
		paths,
		lens,
		pieceLength,
	)
	if err != nil {
		return nil, err
	}

	picker := NewPicker(
		torrentSize,
		pieceLength,
		pieceHashes,
		cfg.PickerConfig,
	)

	return &Manager{
		picker:      picker,
		store:       store,
		torrentSize: torrentSize,
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
		return true, cancels, err
	}

	m.picker.MarkPieceVerified(ok)

	return true, cancels, nil
}

func (m *Manager) NextForPeer(pv *PeerView) []*Request {
	return m.picker.NextForPeer(pv)
}

func (m *Manager) OnPeerGone(peer netip.AddrPort, bf bitfield.Bitfield) {
	m.picker.OnPeerGone(peer, bf)
}

func (m *Manager) OnPeerBitfield(peer netip.AddrPort, bf bitfield.Bitfield) {
	m.picker.OnPeerBitfield(peer, bf)
}

func (m *Manager) OnPeerHave(peer netip.AddrPort, pieceIdx int) {
	m.picker.OnPeerHave(peer, pieceIdx)
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
		return nil, err
	}
	return buf, nil
}

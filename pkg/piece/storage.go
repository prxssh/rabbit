package piece

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Datafile on disk that belongs to the torrent.
//
// Each file occupies a contiguous byte range (Offset..Offset+Length) within the
// concatenated torrent byte stream.
type DataFile struct {
	Path   string // absolute path on disk
	Length int64  // file size in bytes
	Offset int64  // starting offset within the full torrent stream
	f      *os.File
}

// PieceStore coordinates verified piece I/O across all files of a torrent.
//
// Concept:
//
//	BitTorrent treats all files as one continuous stream of bytes.
//	Each piece index refers to a range within that stream.
//	PieceStore maps those ranges to real files/offsets and performs I/O.
//
// Typical flow:
//  1. BufferBlock() as blocks arrive
//  2. FlushPiece() when a piece is complete (verify SHA-1, write to disk)
//  3. RecheckPiece() for resume/recheck flows
type Store struct {
	files       []DataFile           // ordered files in the torrent
	totalBytes  int64                // total concatenated length
	pieceLength int64                // nominal piece length (except the final piece)
	mu          sync.RWMutex         // protects buffers
	buffers     map[int]*PieceBuffer // in-memory piece buffers keyed by piece index
}

// PieceBuffer holds all blocks for a piece until it’s verified.
type PieceBuffer struct {
	blocks      map[int][]byte // blockIndex → raw data
	blockCount  int            // total expected blocks for this piece
	pieceLength int            // full piece size (may differ for final piece)
	lastBlock   int            // size of the final block in this piece
}

// BlockInfo describes a single block’s position inside a piece/stream.
type BlockInfo struct {
	PieceIndex  int   // which piece this block belongs to
	BlockIndex  int   // index within the piece (0-based)
	PieceLength int   // nominal piece length from metadata
	BlockLength int   // block length used by your requester
	IsLastPiece bool  // whether this is the torrent’s final piece
	TotalSize   int64 // total torrent length in bytes
}

// NewStore prepares directories, opens/truncates files, and precomputes stream
// offsets for each file.
//
// Parameters:
//
//	rootDir:      destination directory (e.g., downloads folder)
//	torrentName:  top-level directory/file name (info.name)
//	paths:        per-file relative paths (each is a path split into segments)
//	lens:         per-file lengths (bytes) aligned with lens
//	pieceLength:  piece length in bytes
//
// Layout on disk:
//
//	<rootDir>/<torrentName>/...  (multi-file)
//	<rootDir>/<torrentName>      (single-file: filePaths = [[name]])
func NewStore(
	rootDir string,
	torrentName string,
	paths [][]string,
	lens []int64,
	pieceLength int64,
) (*Store, error) {
	if len(paths) != len(lens) {
		return nil, fmt.Errorf("paths/lengths mismatch")
	}
	if pieceLength <= 0 {
		return nil, fmt.Errorf("invalid piece length: %d", pieceLength)
	}

	var (
		files  []DataFile
		offset int64
	)
	root := filepath.Join(rootDir, torrentName)

	for i := range paths {
		rel := filepath.Join(paths[i]...)
		fullPath := filepath.Join(root, rel)

		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir: %w", err)
		}

		f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", fullPath, err)
		}
		if err := f.Truncate(lens[i]); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("truncate %s: %w", fullPath, err)
		}

		files = append(
			files,
			DataFile{
				Path:   fullPath,
				Length: lens[i],
				Offset: offset,
				f:      f,
			},
		)
		offset += lens[i]
	}

	return &Store{
		files:       files,
		totalBytes:  offset,
		pieceLength: pieceLength,
		buffers:     make(map[int]*PieceBuffer),
	}, nil
}

// Close closes all files owned by the store.
func (s *Store) Close() error {
	var err error

	for i := range s.files {
		if e := s.files[i].f.Close(); e != nil && err == nil {
			err = e
		}
	}

	return err
}

// BufferBlock stores a downloaded block in memory for its piece.
func (s *Store) BufferBlock(data []byte, bi BlockInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.buffers[bi.PieceIndex]; !ok {

		pieceSize := bi.PieceLength
		if bi.IsLastPiece {
			pieceSize = LastPieceLength(
				bi.TotalSize,
				int64(bi.PieceLength),
			)
		}

		blockCount := BlockCountForPiece(pieceSize, bi.BlockLength)
		lastBlock := LastBlockLength(pieceSize, bi.BlockLength)

		s.buffers[bi.PieceIndex] = &PieceBuffer{
			blocks:      make(map[int][]byte),
			blockCount:  blockCount,
			pieceLength: pieceSize,
			lastBlock:   lastBlock,
		}
	}

	s.buffers[bi.PieceIndex].blocks[bi.BlockIndex] = data
}

// FlushPiece assembles the buffered piece, verifies its SHA-1, and writes the
// piece bytes to their correct files if valid.
//
// Returns (true, nil) on success, (false, nil) on hash mismatch.
func (s *Store) FlushPiece(
	pieceIdx int,
	expectedHash [sha1.Size]byte,
) (bool, error) {
	s.mu.RLock()
	pb, ok := s.buffers[pieceIdx]
	s.mu.RUnlock()

	if !ok {
		return false, fmt.Errorf("piece %d not buffered", pieceIdx)
	}
	if len(pb.blocks) != pb.blockCount {
		return false, fmt.Errorf(
			"piece %d incomplete: have %d/%d blocks",
			pieceIdx,
			len(pb.blocks),
			pb.blockCount,
		)
	}

	pieceData := make([]byte, 0, pb.pieceLength)
	for bi := 0; bi < pb.blockCount; bi++ {
		chunk, ok := pb.blocks[bi]
		if !ok {
			return false, fmt.Errorf(
				"piece %d missing block %d",
				pieceIdx,
				bi,
			)
		}

		pieceData = append(pieceData, chunk...)
	}

	if sum := sha1.Sum(pieceData); sum != expectedHash {
		s.mu.Lock()
		delete(s.buffers, pieceIdx)
		s.mu.Unlock()

		return false, nil
	}

	pieceStart := int64(pieceIdx) * s.pieceLength
	if err := s.writeStreamAt(pieceData, pieceStart); err != nil {
		return false, fmt.Errorf("write piece %d: %w", pieceIdx, err)
	}

	s.mu.Lock()
	delete(s.buffers, pieceIdx)
	s.mu.Unlock()

	return false, nil
}

// RecheckPiece reads a piece back from disk and verifies its SHA-1.
//
// Use this for resume or manual recheck operations.
func (s *Store) RecheckPiece(
	pieceIdx int,
	actualLen int,
	expectedHash [sha1.Size]byte,
) (bool, error) {
	if actualLen <= 0 {
		pl, err := PieceLengthAt(pieceIdx, s.totalBytes, s.pieceLength)
		if err != nil {
			return false, err
		}
		actualLen = pl
	}

	buf := make([]byte, actualLen)
	pieceStart := int64(pieceIdx) * s.pieceLength

	if err := s.readStreamAt(buf, pieceStart); err != nil {
		return false, fmt.Errorf("read piece %d: %w", pieceIdx, err)
	}

	sum := sha1.Sum(buf)
	return sum == expectedHash, nil
}

// BufferedBytes reports total bytes currently buffered in memory.
//
// Helpful for backpressure/metrics.
func (s *Store) BufferedBytes() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total int64
	for _, pb := range s.buffers {
		for _, data := range pb.blocks {
			total += int64(len(data))
		}
	}

	return total
}

// writeStreamAt writes 'p' into the logical torrent stream at 'streamOff',
// automatically splitting the write across underlying files. It assumes 'p' is
// already verified.
func (s *Store) writeStreamAt(p []byte, streamOff int64) error {
	if len(p) == 0 {
		return nil
	}
	end := streamOff + int64(len(p))

	for i := range s.files {
		f := &s.files[i]

		// no overlap (buffer ends before this file starts)
		if end <= f.Offset {
			break
		}
		// no overlap (buffer starts after this file ends)
		if streamOff >= f.Offset+f.Length {
		}

		// overlap bounds within file
		fileStart := max64(streamOff, f.Offset)
		fileEnd := min64(end, f.Offset+f.Length)
		n := fileEnd - fileStart
		if n <= 0 {
			continue
		}

		pStart := fileStart - streamOff
		pEnd := pStart + n
		fileOff := fileStart - f.Offset

		if _, err := f.f.WriteAt(p[pStart:pEnd], fileOff); err != nil {
			return fmt.Errorf(
				"write %s@%d: %w",
				f.Path,
				fileOff,
				err,
			)
		}
	}

	return nil
}

// readStreamAt reads into 'p' from the logical torrent stream at 'streamOff',
// automatically spanning across multiple files as needed.
func (s *Store) readStreamAt(p []byte, streamOff int64) error {
	if len(p) == 0 {
		return nil
	}
	end := streamOff + int64(len(p))

	for i := range s.files {
		f := &s.files[i]

		// no overlap (buffer ends before this file starts)
		if end <= f.Offset {
			break
		}
		// no overlap (buffer starts after this file ends)
		if streamOff >= f.Offset+f.Length {
		}

		// overlap bounds within file
		fileStart := max64(streamOff, f.Offset)
		fileEnd := min64(end, f.Offset+f.Length)
		n := fileEnd - fileStart
		if n <= 0 {
			continue
		}

		pStart := fileStart - streamOff
		pEnd := pStart + n
		fileOff := fileStart - f.Offset

		if _, err := f.f.ReadAt(p[pStart:pEnd], fileOff); err != nil {
			return fmt.Errorf(
				"write %s@%d: %w",
				f.Path,
				fileOff,
				err,
			)
		}
	}

	return nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

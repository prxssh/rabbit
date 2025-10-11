package storage

import (
	"crypto/sha1"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/prxssh/rabbit/internal/config"
	"github.com/prxssh/rabbit/internal/meta"
	"github.com/prxssh/rabbit/internal/piece"
)

// Datafile is a file on disk that belongs to a torrent.
//
// Each file occupies a contiguous byte range [offset, offset+length] within the
// concatenated torrent byte stream.
type Datafile struct {
	Path   string   // absolute path on disk
	Length int64    // file size in bytes
	Offset int64    // starting offset
	f      *os.File // file
}

// PieceBuffer holds all blocks for a piece until its verified.
type PieceBuffer struct {
	data []byte
	size int32
}

// Store coordinates verified piece I/O across all files of a torrent.
//
// Concept:
//
//	BitTorrent treats all files as one continuous stream of bytes.
//	Each piece index refers to a range within that stream.
//	Store maps those ranges to real files/offsets and performs I/O.
//
// Typical flow:
//  1. BufferBlock() as blocks arrive
//  2. FlushPiece() when a piece is complete (verify SHA-1, write to disk)
//  3. RecheckPiece() for resume/recheck flows
type Store struct {
	files       []*Datafile          // ordered files in the torrent
	size        int64                // total concatenated length
	pieceLength int32                // nominal piece length (except the final piece)
	mu          sync.RWMutex         // protects buffer
	buffers     map[int]*PieceBuffer // in-memory piece buffer
	log         *slog.Logger
}

// BlockInfo describes a single block’s position inside a piece/stream.
type BlockInfo struct {
	PieceIndex  int   // which piece this block belongs to
	BlockIndex  int   // index within the piece (0-based)
	PieceLength int32 // nominal piece length from metadata
	BlockLength int32 // block length used by your requester
	IsLastPiece bool  // whether this is the torrent’s final piece
	Size        int64 // total torrent length in bytes
}

// NewStore prepares directories, opens/truncates files, and precomputes stream
// offsets for each file.
//
// Parameters:
//
//	torrentName:  top-level directory/file name (info.name)
//	files:        torrent files
//	pieceLength:  piece length in bytes
//
// Layout on disk:
//
//	<rootDir>/<torrentName>/...  (multi-file)
//	<rootDir>/<torrentName>      (single-file: filePaths = [[name]])
func NewStore(
	torrentName string,
	files []*meta.File,
	pieceLen int32,
	log *slog.Logger,
) (*Store, error) {
	l := log.With("src", "store")

	var (
		dataFiles []*Datafile
		offset    int64
	)
	root := filepath.Join(config.Load().DefaultDownloadDir, torrentName)

	l.Debug("creating storage", "root", root, "files", len(files))

	for _, file := range files {
		relative := filepath.Join(file.Path...)
		fullpath := filepath.Join(root, relative)
		dir := filepath.Dir(fullpath)

		if err := os.MkdirAll(dir, 0o755); err != nil {
			l.Error("create directory failed", "error", err.Error(), "path", dir)
			return nil, err
		}

		f, err := os.OpenFile(fullpath, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			l.Error("failed to open file", "path", fullpath, "error", err.Error())
			return nil, err
		}
		if err := f.Truncate(int64(file.Length)); err != nil {
			l.Error(
				"failed to truncate file",
				"path", fullpath,
				"size", file.Length,
				"error", err.Error(),
			)

			_ = f.Close()
			return nil, err
		}

		dataFiles = append(
			dataFiles,
			&Datafile{Path: fullpath, Length: file.Length, Offset: offset, f: f},
		)
		offset += file.Length
	}

	l.Debug("storage initialized", "size", offset)

	return &Store{
		files:       dataFiles,
		size:        offset,
		pieceLength: pieceLen,
		buffers:     make(map[int]*PieceBuffer),
		log:         l,
	}, nil
}

func (s *Store) Close() {
	for _, file := range s.files {
		if err := file.f.Close(); err != nil {
			s.log.Warn("close file failed", "error", err.Error(), "path", file.Path)
		}
	}
}

// BufferBlock stores a downloaded block in memory for its piece.
func (s *Store) BufferBlock(data []byte, bi BlockInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	buf, ok := s.buffers[bi.PieceIndex]
	if !ok {
		size := bi.PieceLength
		if bi.IsLastPiece {
			size = piece.LastPieceLength(bi.Size, bi.PieceLength)
		}

		buf = &PieceBuffer{data: make([]byte, size), size: size}
		s.buffers[bi.PieceIndex] = buf
	}

	begin := bi.BlockIndex * int(bi.BlockLength)
	end := begin + len(data)
	copy(buf.data[begin:end], data)
}

// FlushPiece assembles the buffered piece, verifies its SHA-1, and writes the
// piece bytes to their correct files if valid.
func (s *Store) FlushPiece(piece int, expectedHash [sha1.Size]byte) error {
	s.mu.RLock()
	buf, ok := s.buffers[piece]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("piece %d not buffered", piece)
	}
	data := buf.data

	sum := sha1.Sum(data)
	if sum != expectedHash {
		return fmt.Errorf("piece %d: has mismatch", piece)
	}

	offset := int64(piece) * int64(s.pieceLength)
	if err := s.writeStreamAt(data, offset); err != nil {
		return fmt.Errorf("piece %d write error %w", piece, err)
	}

	s.mu.Lock()
	delete(s.buffers, piece)
	s.mu.Unlock()

	return nil
}

// RecheckPiece reads a piece back from disk and verifies its SHA-1.
func (s *Store) RecheckPiece(piece, length int, expectedHash [sha1.Size]byte) error {
	buf := make([]byte, length)
	offset := int64(piece) * int64(s.pieceLength)
	if err := s.readStreamAt(buf, offset); err != nil {
		return err
	}

	sum := sha1.Sum(buf)
	if sum != expectedHash {
		return fmt.Errorf("piece %d: verification failed", piece)
	}

	return nil
}

// BufferedBytes reports total bytes currently buffered in memory.
func (s *Store) BufferedBytes() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var size int64
	for _, buf := range s.buffers {
		size += int64(len(buf.data))
	}

	return size
}

// writeStreamAt writes 'p' into the logical torrent stream at 'off',
// automatically splitting the write across underlying files. It assumes 'p' is
// already verified.
func (s *Store) writeStreamAt(p []byte, off int64) error {
	remain := p
	offset := off

	for _, file := range s.files {
		if offset >= file.Offset+file.Length {
			continue
		}
		if len(remain) == 0 {
			break
		}

		fileOff := max(0, offset-file.Offset)
		canWrite := file.Length - fileOff

		n := min(canWrite, int64(len(remain)))
		if _, err := file.f.WriteAt(remain[:n], fileOff); err != nil {
			return err
		}

		offset += n
		remain = remain[n:]
	}

	if len(remain) > 0 {
		return io.ErrShortWrite
	}
	return nil
}

// readStreamAt reads into 'p' from the logical torrent stream at 'streamOff',
// automatically spanning across multiple files as needed.
func (s *Store) readStreamAt(p []byte, off int64) error {
	remain := p
	offset := off

	for _, file := range s.files {
		if offset >= file.Offset+file.Length {
			continue
		}
		if len(remain) == 0 {
			break
		}

		fileOff := max(0, offset-file.Offset)
		canRead := file.Length - fileOff

		n := min(canRead, int64(len(remain)))
		if _, err := file.f.ReadAt(remain[:n], fileOff); err != nil {
			return err
		}

		offset += n
		remain = remain[n:]
	}

	if len(remain) > 0 {
		return io.ErrShortWrite
	}
	return nil
}

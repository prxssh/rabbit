package storage

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/prxssh/rabbit/internal/meta"
	"github.com/prxssh/rabbit/internal/scheduler"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sync/errgroup"
)

type Config struct {
	DownloadDir    string
	PieceQueueSize int
	DiskQueueSize  int
}

func WithDefaultConfig() *Config {
	return &Config{
		DownloadDir:    getDefaultDownloadDir(),
		PieceQueueSize: 200,
		DiskQueueSize:  100,
	}
}

func getDefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		if cwd, err := os.Getwd(); err == nil {
			return filepath.Join(cwd, "downloads")
		}
		return "./downloads"
	}

	switch runtime.Environment(context.Background()).Platform {
	case "windows":
		return filepath.Join(home, "Downloads", "rabbit")
	case "darwin":
		return filepath.Join(home, "Downloads", "rabbit")
	default: // linux, bsd, etc.
		return filepath.Join(home, ".local", "share", "rabbit", "downloads")
	}
}

type Store struct {
	cfg              *Config
	log              *slog.Logger
	pieceBufferMut   sync.RWMutex
	pieceBuffers     map[int]*pieceBuffer
	pieceHashes      [][sha1.Size]byte
	PieceQueue       chan *scheduler.BlockData
	diskWriteQueue   chan *completePiece
	PieceResultQueue chan *scheduler.PieceResult
	pieceLen         int32
	files            []*datafile
	totalSize        int64
}

type pieceBuffer struct {
	index    int
	blocks   map[int][]byte
	size     int
	received int
	mut      sync.Mutex
}

type datafile struct {
	f      *os.File
	offset int64
	length int64
	path   string
}

type completePiece struct {
	index int
	data  []byte
}

func NewStorage(metainfo *meta.Metainfo, cfg *Config, log *slog.Logger) (*Store, error) {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "storage")

	if cfg == nil {
		cfg = WithDefaultConfig()
	}

	files, err := setupFiles(metainfo, cfg.DownloadDir)
	if err != nil {
		return nil, fmt.Errorf("setup files: %w", err)
	}

	s := &Store{
		cfg:              cfg,
		log:              log,
		files:            files,
		pieceHashes:      metainfo.Info.Pieces,
		pieceLen:         metainfo.Info.PieceLength,
		pieceBuffers:     make(map[int]*pieceBuffer),
		PieceResultQueue: make(chan *scheduler.PieceResult, cfg.DiskQueueSize),
		diskWriteQueue:   make(chan *completePiece, cfg.DiskQueueSize),
		PieceQueue:       make(chan *scheduler.BlockData, cfg.PieceQueueSize),
	}

	return s, nil
}

func (s *Store) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error { return s.processPiecesLoop(gctx) })
	g.Go(func() error { return s.writeToDiskLoop(gctx) })

	s.log.Info("workers started")

	return g.Wait()
}

func (s *Store) processPiecesLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		case piece, ok := <-s.PieceQueue:
			if !ok {
				return nil
			}

			if err := s.handlePieceBlock(piece); err != nil {
				s.log.Error("handle piece failed", "error", err.Error())
			}
		}
	}
}

func (s *Store) handlePieceBlock(block *scheduler.BlockData) error {
	s.pieceBufferMut.Lock()
	buf, exists := s.pieceBuffers[block.PieceIdx]
	if !exists {
		buf = &pieceBuffer{
			index:  block.PieceIdx,
			blocks: make(map[int][]byte),
			size:   block.PieceLen,
		}
		s.pieceBuffers[block.PieceIdx] = buf
	}
	s.pieceBufferMut.Unlock()

	buf.mut.Lock()

	if _, exists := buf.blocks[block.BlockIdx]; exists {
		buf.mut.Unlock()
		s.log.Debug(
			"received duplicate block",
			"piece", block.PieceIdx,
			"block", block.BlockIdx,
		)
		return nil
	}

	buf.blocks[block.BlockIdx] = block.Data
	buf.received += len(block.Data)

	if buf.received != buf.size {
		buf.mut.Unlock()
		return nil
	}

	completeData := make([]byte, buf.size)
	for offset, block := range buf.blocks {
		copy(completeData[offset:], block)
	}

	buf.mut.Unlock()

	hash := sha1.Sum(completeData)
	if hash != s.pieceHashes[block.PieceIdx] {
		s.log.Warn("piece hash mismatch, discarding", "piece", block.PieceIdx)

		buf.mut.Lock()
		buf.blocks = make(map[int][]byte)
		buf.received = 0
		buf.mut.Unlock()

		s.PieceResultQueue <- &scheduler.PieceResult{Piece: block.PieceIdx, Success: false}

		return fmt.Errorf("piece %d: hash mismatch", block.PieceIdx)
	}

	s.diskWriteQueue <- &completePiece{index: block.PieceIdx, data: completeData}

	s.pieceBufferMut.Lock()
	delete(s.pieceBuffers, block.PieceIdx)
	s.pieceBufferMut.Unlock()

	return nil
}

func (s *Store) writeToDiskLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		case piece, ok := <-s.diskWriteQueue:
			if !ok {
				return nil
			}

			success := true

			if err := s.writePiece(piece); err != nil {
				s.log.Error("failed to write piece to disk",
					"index", piece.index,
					"error", err.Error(),
				)

				success = false
			}

			s.PieceResultQueue <- &scheduler.PieceResult{Piece: piece.index, Success: success}
		}
	}
}

func (s *Store) writePiece(piece *completePiece) error {
	pieceAbsStart := int64(piece.index) * int64(s.pieceLen)
	pieceAbsEnd := pieceAbsStart + int64(len(piece.data))

	for _, file := range s.files {
		fileAbsStart := file.offset
		fileAbsEnd := fileAbsStart + file.length

		overlapStart := max(pieceAbsStart, fileAbsStart)
		overlapEnd := min(pieceAbsEnd, fileAbsEnd)

		if overlapStart >= overlapEnd {
			continue
		}

		writeLen := overlapEnd - overlapStart
		offsetInFile := overlapStart - fileAbsStart
		offsetInData := overlapStart - pieceAbsStart

		n, err := file.f.WriteAt(
			piece.data[offsetInData:offsetInData+writeLen],
			offsetInFile,
		)
		if err != nil {
			return fmt.Errorf("file write error for %s: %w", file.path, err)
		}
		if int64(n) != writeLen {
			return fmt.Errorf(
				"incomplete write to file %s: wrote %d, expected %d",
				file.path,
				n,
				writeLen,
			)
		}
	}

	return nil
}

func (s *Store) readPiece(index int, data []byte) error {
	pieceAbsStart := int64(index) * int64(s.pieceLen)
	pieceAbsEnd := pieceAbsStart + int64(len(data))

	for _, file := range s.files {
		fileAbsStart := file.offset
		fileAbsEnd := file.offset + file.length

		overlapStart := max(pieceAbsStart, fileAbsStart)
		overlapEnd := min(pieceAbsEnd, fileAbsEnd)

		if overlapStart >= overlapEnd {
			continue
		}

		readLen := overlapEnd - overlapStart
		offsetInFile := overlapStart - fileAbsStart
		offsetInData := overlapStart - pieceAbsStart

		n, err := file.f.ReadAt(data[offsetInData:offsetInData+readLen], offsetInFile)
		if err != nil {
			return fmt.Errorf("file read error for %s: %w", file.path, err)
		}
		if int64(n) != readLen {
			return fmt.Errorf(
				"incomplete read from file %s: read %d, expected %d",
				file.path,
				n,
				readLen,
			)
		}
	}

	return nil
}

func setupFiles(metainfo *meta.Metainfo, downloadDir string) ([]*datafile, error) {
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return nil, err
	}

	var (
		currentOffset int64
		datafiles     []*datafile
	)

	if metainfo.Info.Length > 0 {
		fp := filepath.Join(downloadDir, metainfo.Info.Name)
		mapping, err := createFileMapping(fp, metainfo.Info.Length, currentOffset)
		if err != nil {
			return nil, err
		}

		datafiles = append(datafiles, mapping)
		return datafiles, nil
	}

	for _, file := range metainfo.Info.Files {
		fp := filepath.Join(downloadDir, metainfo.Info.Name)
		for _, pathPart := range file.Path {
			fp = filepath.Join(fp, pathPart)
		}

		mapping, err := createFileMapping(fp, file.Length, currentOffset)
		if err != nil {
			return nil, err
		}

		datafiles = append(datafiles, mapping)
		currentOffset += file.Length
	}

	return datafiles, nil
}

func createFileMapping(path string, size, offset int64) (*datafile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}

	if err := file.Truncate(size); err != nil {
		file.Close()
		return nil, err
	}

	return &datafile{path: path, length: size, offset: offset, f: file}, nil
}

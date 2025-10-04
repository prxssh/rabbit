package storage

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"sync"
)

// Disk manages file I/O with piece-level buffering for a torrent download.
//
// Blocks are buffered in memory until an entire piece is downloaded and
// verified. Only verified pieces are written to disk, preventing corruption
// from bad data.
type Disk struct {
	f    *os.File
	size int64

	mu     sync.RWMutex
	buffer map[int]*buffer
}

type buffer struct {
	blocks      map[int][]byte // blockIdx -> data
	blockCount  int
	pieceLength int
	lastBlock   int // size of final block
}

// BlockMetadata describes the piece/block structure for buffering.
type BlockMetadata struct {
	PieceIdx    int
	BlockIdx    int
	PieceLength int
	BlockLength int
	IsLastPiece bool
	TotalSize   int64
}

// OpenSingleFile creates or opens a file for torrent storage with the specified
// size. The file is pre-allocated to avoid fragmentation.
func OpenSingleFile(path string, size int64) (*Disk, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	if err := f.Truncate(size); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("allocated file: %w", err)
	}

	return &Disk{
		f:      f,
		size:   size,
		buffer: make(map[int]*buffer),
	}, nil
}

// Close flushes any remaining buffered data and closes the file.
func (d *Disk) Close() error {
	return d.f.Close()
}

// AddBlock buffers a block in memory. Storage handles all buffering logic.
func (d *Disk) AddBlock(data []byte, meta BlockMetadata) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.buffer[meta.PieceIdx] == nil {
		plen := meta.PieceLength
		if meta.IsLastPiece {
			plen = int(
				meta.TotalSize,
			) - (meta.PieceIdx * meta.PieceLength)
		}

		blockCount := (plen + meta.BlockLength - 1) / meta.BlockLength
		lastBlock := plen - (blockCount-1)*meta.BlockLength
		if blockCount == 1 {
			lastBlock = plen
		}

		d.buffer[meta.PieceIdx] = &buffer{
			blocks:      make(map[int][]byte),
			blockCount:  blockCount,
			pieceLength: plen,
			lastBlock:   lastBlock,
		}
	}

	d.buffer[meta.PieceIdx].blocks[meta.BlockIdx] = data
}

// VerifyAndFlushPiece verifies a buffered piece and writes it to disk if valid.
// Returns true if piece was valid and written, false if hash mismatch.
func (d *Disk) VerifyAndFlushPiece(
	pieceIdx, pieceLength int,
	expectedHash [sha1.Size]byte,
) (bool, error) {
	d.mu.Lock()
	pb := d.buffer[pieceIdx]
	d.mu.Unlock()

	if pb == nil {
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
	for blockIdx := 0; blockIdx < pb.blockCount; blockIdx++ {
		data, ok := pb.blocks[blockIdx]
		if !ok {
			return false, fmt.Errorf("missing block %d", blockIdx)
		}
		pieceData = append(pieceData, data...)
	}

	hash := sha1.Sum(pieceData)
	if hash != expectedHash {
		d.mu.Lock()
		delete(d.buffer, pieceIdx)
		d.mu.Unlock()
		return false, nil
	}

	offset := int64(pieceIdx) * int64(pieceLength)
	if _, err := d.f.WriteAt(pieceData, offset); err != nil {
		return false, fmt.Errorf("write: %w", err)
	}

	if err := d.f.Sync(); err != nil {
		return false, fmt.Errorf("sync: %w", err)
	}

	d.mu.Lock()
	delete(d.buffer, pieceIdx)
	d.mu.Unlock()

	return true, nil
}

// VerifyPiece reads a piece from disk and verifies its SHA-1 hash.
// Used for checking already-downloaded pieces on startup.
func (d *Disk) VerifyPiece(
	pieceIdx, pieceLength, actualLength int,
	expectedHash [sha1.Size]byte,
) (bool, error) {
	data := make([]byte, actualLength)
	offset := int64(pieceIdx) * int64(pieceLength)

	n, err := d.f.ReadAt(data, offset)
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read piece: %w", err)
	}
	if n != actualLength {
		return false, fmt.Errorf(
			"read %d bytes, expected %d",
			n,
			actualLength,
		)
	}

	hash := sha1.Sum(data)
	return hash == expectedHash, nil
}

// BufferedBytes returns total bytes currently buffered in memory.
func (d *Disk) BufferedBytes() int64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var total int64
	for _, pb := range d.buffer {
		for _, data := range pb.blocks {
			total += int64(len(data))
		}
	}
	return total
}

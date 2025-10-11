package storage

import (
	"crypto/sha1"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/prxssh/rabbit/internal/config"
	"github.com/prxssh/rabbit/internal/meta"
	"github.com/prxssh/rabbit/internal/piece"
)

func TestMain(m *testing.M) {
	config.Swap(config.Config{DefaultDownloadDir: "."})
	code := m.Run()
	os.Exit(code)
}

func TestStore_TableDrivenEdgeCases(t *testing.T) {
	type fileSpec struct {
		path   []string
		length int64
	}

	mkFiles := func(specs []fileSpec) []*meta.File {
		out := make([]*meta.File, 0, len(specs))
		for _, s := range specs {
			out = append(out, &meta.File{Path: s.path, Length: s.length})
		}
		return out
	}

	// deterministic data pattern for repeatability
	genStream := func(n int64) []byte {
		b := make([]byte, n)
		for i := int64(0); i < n; i++ {
			b[i] = byte((i*7 + 3) % 256)
		}
		return b
	}

	pieceHashes := func(stream []byte, pieceLen int32) [][sha1.Size]byte {
		var hashes [][sha1.Size]byte
		size := int64(len(stream))
		pc := piece.PieceCount(size, pieceLen)
		for i := 0; i < pc; i++ {
			start, end, err := piece.PieceOffsetBounds(i, size, pieceLen)
			if err != nil {
				t.Fatalf("piece bounds: %v", err)
			}
			h := sha1.Sum(stream[start:end])
			hashes = append(hashes, h)
		}
		return hashes
	}

	// helper to write all pieces using a chosen block length
	writeAllPieces := func(t *testing.T, s *Store, blockLen int32, stream []byte, pieceLen int32) [][sha1.Size]byte {
		t.Helper()
		size := int64(len(stream))
		pc := piece.PieceCount(size, pieceLen)
		hashes := pieceHashes(stream, pieceLen)

		for i := 0; i < pc; i++ {
			pl, err := piece.PieceLengthAt(i, size, pieceLen)
			if err != nil {
				t.Fatalf("piece len at %d: %v", i, err)
			}

			// buffer blocks for this piece
			bc := piece.BlockCountForPiece(pl, blockLen)
			for bidx := 0; bidx < bc; bidx++ {
				begin, blen, err := piece.BlockOffsetBounds(pl, blockLen, bidx)
				if err != nil {
					t.Fatalf("block bounds p=%d b=%d: %v", i, bidx, err)
				}

				// compute offsets into the full stream
				pStart, _, err := piece.PieceOffsetBounds(i, size, pieceLen)
				if err != nil {
					t.Fatalf("piece bounds p=%d: %v", i, err)
				}
				seg := make([]byte, blen)
				copy(
					seg,
					stream[pStart+int64(begin):pStart+int64(begin)+int64(blen)],
				)

				bi := BlockInfo{
					PieceIndex:  i,
					BlockIndex:  bidx,
					PieceLength: pieceLen,
					BlockLength: blockLen,
					IsLastPiece: i == pc-1,
					Size:        size,
				}
				s.BufferBlock(seg, bi)
			}

			if err := s.FlushPiece(i, hashes[i]); err != nil {
				t.Fatalf("flush piece %d: %v", i, err)
			}
		}

		return hashes
	}

	tests := []struct {
		name     string
		tname    string
		files    []fileSpec
		pieceLen int32
		blockLen int32
	}{
		{
			name:     "single-file exact pieces",
			tname:    "single_exact",
			files:    []fileSpec{{path: []string{"single_exact"}, length: 64}},
			pieceLen: 16,
			blockLen: 16,
		},
		{
			name:     "single-file last piece short",
			tname:    "single_short",
			files:    []fileSpec{{path: []string{"single_short"}, length: 30}},
			pieceLen: 16,
			blockLen: 32, // block larger than last piece
		},
		{
			name:  "multi-file crossing boundaries",
			tname: "multi_cross",
			files: []fileSpec{
				{path: []string{"a.bin"}, length: 5},
				{path: []string{"b.bin"}, length: 7},
				{path: []string{"c.bin"}, length: 3},
			},
			pieceLen: 8,
			blockLen: 3, // odd tiny blocks to misalign
		},
		{
			name:  "tiny blocks (1 byte)",
			tname: "tiny_blocks",
			files: []fileSpec{
				{path: []string{"tiny1"}, length: 4},
				{path: []string{"tiny2"}, length: 6},
			},
			pieceLen: 5,
			blockLen: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// unique temp root per subtest
			root := t.TempDir()
			config.Update(func(c *config.Config) { c.DefaultDownloadDir = root })

			// logger discards output
			lg := slog.New(slog.NewTextHandler(io.Discard, nil))

			files := mkFiles(tt.files)
			s, err := NewStore(tt.tname, files, tt.pieceLen, lg)
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			defer s.Close()

			// total size and stream data
			var total int64
			for _, f := range files {
				total += f.Length
			}
			stream := genStream(total)

			// write all pieces using the requested block size
			hashes := writeAllPieces(t, s, tt.blockLen, stream, tt.pieceLen)

			// recheck pieces from disk
			pc := piece.PieceCount(total, tt.pieceLen)
			for i := 0; i < pc; i++ {
				plen, err := piece.PieceLengthAt(i, total, tt.pieceLen)
				if err != nil {
					t.Fatalf("PieceLengthAt: %v", err)
				}
				if err := s.RecheckPiece(i, int(plen), hashes[i]); err != nil {
					t.Fatalf("RecheckPiece %d: %v", i, err)
				}
			}

			// finally verify concatenated on-disk bytes equal original stream
			var onDisk []byte
			for _, df := range s.files {
				// confirm file exists under the correct root
				if filepath.Dir(df.Path) != filepath.Join(root, tt.tname) {
					t.Fatalf(
						"file directory mismatch: got %q want %q",
						filepath.Dir(df.Path),
						filepath.Join(root, tt.tname),
					)
				}
				b, rerr := io.ReadAll(io.NewSectionReader(df.f, 0, df.Length))
				if rerr != nil {
					t.Fatalf("read file %s: %v", df.Path, rerr)
				}
				onDisk = append(onDisk, b...)
			}

			if len(onDisk) != len(stream) {
				t.Fatalf(
					"on-disk size mismatch: got=%d want=%d",
					len(onDisk),
					len(stream),
				)
			}
			for i := range stream {
				if onDisk[i] != stream[i] {
					t.Fatalf(
						"byte mismatch at %d: got=%d want=%d",
						i,
						onDisk[i],
						stream[i],
					)
				}
			}
		})
	}
}

// ensure FlushPiece rejects wrong hash and leaves buffer intact.
func TestStore_FlushPieceRejectsWrongHash(t *testing.T) {
	root := t.TempDir()
	config.Update(func(c *config.Config) { c.DefaultDownloadDir = root })

	lg := slog.New(slog.NewTextHandler(io.Discard, nil))

	files := []*meta.File{{Path: []string{"one"}, Length: 10}}
	s, err := NewStore("bad_hash", files, 8, lg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	stream := make([]byte, 10)
	for i := range stream {
		stream[i] = byte(i)
	}

	// buffer first (and only full) piece
	pl, _ := piece.PieceLengthAt(0, int64(len(stream)), 8)
	bc := piece.BlockCountForPiece(pl, 4)
	for bidx := 0; bidx < bc; bidx++ {
		begin, blen, _ := piece.BlockOffsetBounds(pl, 4, bidx)
		seg := make([]byte, blen)
		copy(seg, stream[int(begin):int(begin)+int(blen)])
		s.BufferBlock(seg, BlockInfo{
			PieceIndex:  0,
			BlockIndex:  bidx,
			PieceLength: 8,
			BlockLength: 4,
			IsLastPiece: false,
			Size:        int64(len(stream)),
		})
	}

	var wrong [sha1.Size]byte
	if err := s.FlushPiece(0, wrong); err == nil {
		t.Fatalf("expected hash mismatch error, got nil")
	}
}

// Ensure BufferedBytes accounts for in-memory buffers and drops after flush.
func TestStore_BufferedBytesAndFlush(t *testing.T) {
	root := t.TempDir()
	config.Update(func(c *config.Config) { c.DefaultDownloadDir = root })

	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	files := []*meta.File{{Path: []string{"file"}, Length: 40}}
	s, err := NewStore("buf_bytes", files, 16, lg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	stream := make([]byte, 40)
	for i := range stream {
		stream[i] = byte(i)
	}
	size := int64(len(stream))

	// Buffer two pieces, check buffered bytes grows by ~piece sizes
	for pIdx := 0; pIdx < 2; pIdx++ {
		pl, _ := piece.PieceLengthAt(pIdx, size, 16)
		bc := piece.BlockCountForPiece(pl, 8)
		for bidx := 0; bidx < bc; bidx++ {
			begin, blen, _ := piece.BlockOffsetBounds(pl, 8, bidx)
			pStart, _, _ := piece.PieceOffsetBounds(pIdx, size, 16)
			seg := make([]byte, blen)
			copy(seg, stream[pStart+int64(begin):pStart+int64(begin)+int64(blen)])
			s.BufferBlock(seg, BlockInfo{
				PieceIndex:  pIdx,
				BlockIndex:  bidx,
				PieceLength: 16,
				BlockLength: 8,
				IsLastPiece: false,
				Size:        size,
			})
		}
	}

	got := s.BufferedBytes()
	if got < 32 { // >= two piece buffers of 16 each
		t.Fatalf("BufferedBytes too small: got=%d want>=32", got)
	}

	// Flush first piece then verify BufferedBytes decreases
	h0 := sha1.Sum(stream[0:16])
	if err := s.FlushPiece(0, h0); err != nil {
		t.Fatalf("FlushPiece: %v", err)
	}
	after := s.BufferedBytes()
	if after >= got {
		t.Fatalf("BufferedBytes did not drop after flush: before=%d after=%d", got, after)
	}
}

// FlushPiece should return an error when the piece wasn't buffered.
func TestStore_FlushWithoutBuffer(t *testing.T) {
	root := t.TempDir()
	config.Update(func(c *config.Config) { c.DefaultDownloadDir = root })
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	files := []*meta.File{{Path: []string{"f"}, Length: 10}}
	s, err := NewStore("no_buf", files, 8, lg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	var zero [sha1.Size]byte
	if err := s.FlushPiece(0, zero); err == nil {
		t.Fatalf("expected error for unbuffered piece, got nil")
	}
}

// Directly exercise writeStreamAt/readStreamAt overflows and boundaries.
func TestStore_WriteReadShortSpans(t *testing.T) {
	root := t.TempDir()
	config.Update(func(c *config.Config) { c.DefaultDownloadDir = root })
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	files := []*meta.File{{Path: []string{"a"}, Length: 4}, {Path: []string{"b"}, Length: 6}}
	s, err := NewStore("spans", files, 8, lg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	// Fill some known bytes into the middle using writeStreamAt
	payload := []byte{1, 2, 3, 4}
	if err := s.writeStreamAt(payload, 2); err != nil {
		t.Fatalf("writeStreamAt: %v", err)
	}

	// Read back across file boundary [2, 6)
	buf := make([]byte, 4)
	if err := s.readStreamAt(buf, 2); err != nil {
		t.Fatalf("readStreamAt: %v", err)
	}
	for i := range buf {
		if buf[i] != payload[i] {
			t.Fatalf("span byte %d mismatch: got=%d want=%d", i, buf[i], payload[i])
		}
	}

	// Attempt to write beyond end → expect io.ErrShortWrite
	big := make([]byte, 5)
	if err := s.writeStreamAt(big, 8); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("expected short write, got %v", err)
	}

	// Attempt to read beyond end → expect io.ErrShortWrite
	if err := s.readStreamAt(make([]byte, 5), 8); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("expected short read, got %v", err)
	}
}

// Ensure oversized blockLen for non-last pieces works and writes correct data.
func TestStore_BlockLenLargerThanPiece_NonLast(t *testing.T) {
	root := t.TempDir()
	config.Update(func(c *config.Config) { c.DefaultDownloadDir = root })
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))

	// total 48 bytes -> 3 pieces of 16
	files := []*meta.File{{Path: []string{"only"}, Length: 48}}
	s, err := NewStore("oversized_block", files, 16, lg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	stream := make([]byte, 48)
	for i := range stream {
		stream[i] = byte((i*13 + 5) % 256)
	}

	pc := piece.PieceCount(int64(len(stream)), 16)
	for i := 0; i < pc; i++ {
		pl, _ := piece.PieceLengthAt(i, int64(len(stream)), 16)
		// Use blockLen larger than pieceLen
		begin, blen, _ := piece.BlockOffsetBounds(pl, 64, 0)
		if begin != 0 || blen != pl {
			t.Fatalf(
				"unexpected block bounds: begin=%d blen=%d, want 0 and %d",
				begin,
				blen,
				pl,
			)
		}
		pStart, _, _ := piece.PieceOffsetBounds(i, int64(len(stream)), 16)
		seg := make([]byte, blen)
		copy(seg, stream[pStart:pStart+int64(blen)])
		s.BufferBlock(seg, BlockInfo{
			PieceIndex:  i,
			BlockIndex:  0,
			PieceLength: 16,
			BlockLength: 64,
			IsLastPiece: i == pc-1,
			Size:        int64(len(stream)),
		})
		hash := sha1.Sum(seg)
		if err := s.FlushPiece(i, hash); err != nil {
			t.Fatalf("FlushPiece(%d): %v", i, err)
		}
	}

	// Recheck random piece
	if err := s.RecheckPiece(1, 16, sha1.Sum(stream[16:32])); err != nil {
		t.Fatalf("RecheckPiece: %v", err)
	}
}

// Concurrently buffer disjoint blocks for the same piece and then flush.
func TestStore_ConcurrentBuffering(t *testing.T) {
	root := t.TempDir()
	config.Update(func(c *config.Config) { c.DefaultDownloadDir = root })
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))

	files := []*meta.File{{Path: []string{"f"}, Length: 32}}
	s, err := NewStore("concurrent", files, 16, lg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	stream := make([]byte, 32)
	for i := range stream {
		stream[i] = byte((i*9 + 7) % 256)
	}

	pl, _ := piece.PieceLengthAt(0, 32, 16)
	bc := piece.BlockCountForPiece(pl, 4) // 4 blocks
	var wg sync.WaitGroup
	for bidx := 0; bidx < bc; bidx++ {
		bidx := bidx
		wg.Add(1)
		go func() {
			defer wg.Done()
			begin, blen, _ := piece.BlockOffsetBounds(pl, 4, bidx)
			seg := make([]byte, blen)
			copy(seg, stream[int(begin):int(begin)+int(blen)])
			s.BufferBlock(seg, BlockInfo{
				PieceIndex:  0,
				BlockIndex:  bidx,
				PieceLength: 16,
				BlockLength: 4,
				IsLastPiece: false,
				Size:        32,
			})
		}()
	}
	wg.Wait()

	// Flush and verify
	hash := sha1.Sum(stream[:16])
	if err := s.FlushPiece(0, hash); err != nil {
		t.Fatalf("FlushPiece: %v", err)
	}
	if err := s.RecheckPiece(0, 16, hash); err != nil {
		t.Fatalf("RecheckPiece: %v", err)
	}
}

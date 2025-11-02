package meta

import (
	"bytes"
	"crypto/sha1"
	"reflect"
	"testing"
	"time"

	"github.com/prxssh/rabbit/pkg/bencode"
)

func mkPieces(n int) []byte {
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		buf.Write(bytes.Repeat([]byte{byte('a' + i)}, sha1.Size))
	}
	return buf.Bytes()
}

func TestParseMetainfo_SingleFile_OK(t *testing.T) {
	info := map[string]any{
		"name":         "file.txt",
		"piece length": int64(16384),
		"pieces":       mkPieces(2),
		"length":       int64(1234),
	}

	root := map[string]any{
		"announce":      "http://tracker",
		"creation date": int64(1700000000),
		"created by":    "tester",
		"comment":       "hello",
		"encoding":      "UTF-8",
		"info":          info,
	}

	data, err := bencode.Marshal(root)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	mi, err := ParseMetainfo(data)
	if err != nil {
		t.Fatalf("ParseMetainfo error: %v", err)
	}

	if mi.Announce != "http://tracker" {
		t.Fatalf("announce = %q", mi.Announce)
	}
	if len(mi.AnnounceList) != 0 {
		t.Fatalf("announce-list = %#v, want empty", mi.AnnounceList)
	}

	wantDate := time.Unix(1700000000, 0).UTC()
	if !mi.CreationDate.Equal(wantDate) {
		t.Fatalf("creation date = %v, want %v", mi.CreationDate, wantDate)
	}
	if mi.CreatedBy != "tester" || mi.Comment != "hello" || mi.Encoding != "UTF-8" {
		t.Fatalf("metadata fields mismatch: %#v", mi)
	}

	if mi.Info == nil {
		t.Fatalf("info is nil")
	}
	if mi.Info.Name != "file.txt" {
		t.Fatalf("name = %q", mi.Info.Name)
	}
	if mi.Info.PieceLength != 16384 {
		t.Fatalf("piece length = %d", mi.Info.PieceLength)
	}
	if len(mi.Info.Pieces) != 2 {
		t.Fatalf("pieces len = %d, want 2", len(mi.Info.Pieces))
	}
	if mi.Info.Length != 1234 || len(mi.Info.Files) != 0 {
		t.Fatalf("layout mismatch: length=%d files=%d", mi.Info.Length, len(mi.Info.Files))
	}

	// Verify info hash
	hashed, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}
	wantHash := sha1.Sum(hashed)
	if mi.InfoHash != wantHash {
		t.Fatalf("info hash mismatch")
	}
}

func TestParseMetainfo_MultiFile_OK(t *testing.T) {
	files := []any{
		map[string]any{
			"length": int64(10),
			"path":   []any{"a", "b.txt"},
		},
		map[string]any{"length": int64(20), "path": []any{"c.txt"}},
	}

	info := map[string]any{
		"name":         "dir",
		"piece length": int64(32768),
		"pieces":       mkPieces(1),
		"files":        files,
		"private":      int64(1),
	}

	root := map[string]any{
		"announce": "udp://tracker",
		"info":     info,
	}
	data, err := bencode.Marshal(root)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	mi, err := ParseMetainfo(data)
	if err != nil {
		t.Fatalf("ParseMetainfo error: %v", err)
	}

	if mi.Info == nil || mi.Info.Private != true {
		t.Fatalf("private flag not parsed")
	}
	if mi.Info.Length != 0 || len(mi.Info.Files) != 2 {
		t.Fatalf("files parsed incorrectly: %+v", mi.Info)
	}
	if got := mi.Info.Files[0].Length; got != 10 {
		t.Fatalf("file0 length = %d", got)
	}
	if want := []string{"a", "b.txt"}; !reflect.DeepEqual(mi.Info.Files[0].Path, want) {
		t.Fatalf("file0 path = %#v, want %#v", mi.Info.Files[0].Path, want)
	}
}

func TestParseMetainfo_AnnounceListOnly_OK(t *testing.T) {
	info := map[string]any{
		"name":         "f",
		"piece length": int64(16384),
		"pieces":       mkPieces(1),
		"length":       int64(1),
	}

	tiers := []any{
		[]any{"http://t1", "http://t1b"},
		[]any{"http://t2"},
	}

	root := map[string]any{
		"announce-list": tiers,
		"info":          info,
	}
	data, _ := bencode.Marshal(root)

	mi, err := ParseMetainfo(data)
	if err != nil {
		t.Fatalf("ParseMetainfo error: %v", err)
	}
	if mi.Announce != "" || len(mi.AnnounceList) != 2 {
		t.Fatalf("announce/announce-list mismatch: %#v", mi)
	}
}

func TestParseMetainfo_TopLevelAndRequiredErrors(t *testing.T) {
	// Top-level not a dict
	data, _ := bencode.Marshal([]any{"x"})
	if _, err := ParseMetainfo(data); err == nil ||
		err != ErrTopLevelNotDict {
		t.Fatalf("want ErrTopLevelNotDict, got %v", err)
	}

	// Missing both announce and announce-list
	info := map[string]any{
		"name":         "f",
		"piece length": int64(1),
		"pieces":       mkPieces(1),
		"length":       int64(1),
	}
	root := map[string]any{"info": info}
	data, _ = bencode.Marshal(root)
	if _, err := ParseMetainfo(data); err == nil ||
		err != ErrAnnounceMissing {
		t.Fatalf("want ErrAnnounceMissing, got %v", err)
	}

	// Info missing
	root = map[string]any{"announce": "x"}
	data, _ = bencode.Marshal(root)
	if _, err := ParseMetainfo(data); err == nil || err != ErrInfoMissing {
		t.Fatalf("want ErrInfoMissing, got %v", err)
	}

	// Info not a dict
	root = map[string]any{"announce": "x", "info": "oops"}
	data, _ = bencode.Marshal(root)
	if _, err := ParseMetainfo(data); err == nil || err != ErrInfoNotDict {
		t.Fatalf("want ErrInfoNotDict, got %v", err)
	}
}

func TestParseMetainfo_FieldValidationErrors(t *testing.T) {
	base := map[string]any{
		"name":         "f",
		"piece length": int64(1),
		"pieces":       mkPieces(1),
		"length":       int64(1),
	}

	// creation date invalid
	root := map[string]any{
		"announce":      "x",
		"info":          base,
		"creation date": int64(-1),
	}
	data, _ := bencode.Marshal(root)
	if _, err := ParseMetainfo(data); err == nil || err != ErrCreationDateInvalid {
		t.Fatalf("want ErrCreationDateInvalid, got %v", err)
	}

	// created by wrong type
	root = map[string]any{
		"announce":   "x",
		"info":       base,
		"created by": int64(1),
	}
	data, _ = bencode.Marshal(root)
	if _, err := ParseMetainfo(data); err == nil ||
		!(contains(err.Error(), "expected string") || contains(err.Error(), "not a string")) {
		t.Fatalf("want error about expected string, got %v", err)
	}
}

func TestParseInfo_ValidationErrors(t *testing.T) {
	// Missing piece length
	_, err := parseInfo(
		map[string]any{
			"name":   "f",
			"pieces": mkPieces(1),
			"length": int64(1),
		},
	)
	if err == nil || err != ErrPieceLenMissing {
		t.Fatalf("want ErrPieceLenMissing, got %v", err)
	}

	// Non-positive piece length
	_, err = parseInfo(map[string]any{
		"name":         "f",
		"piece length": int64(0),
		"pieces":       mkPieces(1),
		"length":       int64(1),
	})
	if err == nil || err != ErrPieceLenNonPositive {
		t.Fatalf("want ErrPieceLenNonPositive, got %v", err)
	}

	// Missing pieces
	_, err = parseInfo(map[string]any{
		"name":         "f",
		"piece length": int64(1),
		"length":       int64(1),
	})
	if err == nil || err != ErrPiecesMissing {
		t.Fatalf("want ErrPiecesMissing, got %v", err)
	}

	// Invalid private flag
	_, err = parseInfo(map[string]any{
		"name":         "f",
		"piece length": int64(1),
		"pieces":       mkPieces(1),
		"length":       int64(1),
		"private":      int64(2),
	})
	if err == nil || !contains(err.Error(), "invalid 'private'") {
		t.Fatalf("want invalid private flag, got %v", err)
	}

	// Layout invalid: both length and files
	_, err = parseInfo(map[string]any{
		"name":         "f",
		"piece length": int64(1),
		"pieces":       mkPieces(1),
		"length":       int64(1),
		"files":        []any{map[string]any{"length": int64(1), "path": []any{"a"}}},
	})
	if err == nil || err != ErrLayoutInvalid {
		t.Fatalf("want ErrLayoutInvalid, got %v", err)
	}

	// Layout invalid: neither length nor files
	_, err = parseInfo(map[string]any{
		"name":         "f",
		"piece length": int64(1),
		"pieces":       mkPieces(1),
	})
	if err == nil || err != ErrLayoutInvalid {
		t.Fatalf("want ErrLayoutInvalid, got %v", err)
	}

	// Invalid length
	_, err = parseInfo(map[string]any{
		"name":         "f",
		"piece length": int64(1),
		"pieces":       mkPieces(1),
		"length":       int64(-1),
	})
	if err == nil || !contains(err.Error(), "invalid 'length'") {
		t.Fatalf("want invalid length, got %v", err)
	}
}

func TestParseFiles_Errors(t *testing.T) {
	// Not a list or empty
	if _, err := parseFiles("oops"); err == nil ||
		!contains(err.Error(), "invalid or empty 'files'") {
		t.Fatalf("want invalid files, got %v", err)
	}
	if _, err := parseFiles([]any{}); err == nil ||
		!contains(err.Error(), "invalid or empty 'files'") {
		t.Fatalf("want invalid files, got %v", err)
	}

	// Element not a dict
	if _, err := parseFiles([]any{"x"}); err == nil || !contains(err.Error(), "not a dict") {
		t.Fatalf("want element not dict, got %v", err)
	}

	// Missing length / invalid length
	if _, err := parseFiles([]any{map[string]any{"path": []any{"a"}}}); err == nil ||
		!contains(err.Error(), "length missing") {
		t.Fatalf("want length missing, got %v", err)
	}
	if _, err := parseFiles([]any{map[string]any{"length": int64(-1), "path": []any{"a"}}}); err == nil ||
		!contains(err.Error(), "invalid length") {
		t.Fatalf("want invalid length, got %v", err)
	}

	// Missing path / invalid path
	if _, err := parseFiles([]any{map[string]any{"length": int64(1)}}); err == nil ||
		!contains(err.Error(), "path missing") {
		t.Fatalf("want path missing, got %v", err)
	}
	if _, err := parseFiles([]any{map[string]any{"length": int64(1), "path": []any{}}}); err == nil ||
		!contains(err.Error(), "invalid path") {
		t.Fatalf("want invalid path, got %v", err)
	}
}

func TestParsePieces_Errors(t *testing.T) {
	if _, err := parsePieces(nil); err == nil || err != ErrPiecesMissing {
		t.Fatalf("want ErrPiecesMissing, got %v", err)
	}
	if _, err := parsePieces(123); err == nil || !contains(err.Error(), "'pieces'") {
		t.Fatalf("want pieces type error, got %v", err)
	}
	if _, err := parsePieces([]byte("short")); err == nil || err != ErrPiecesLenInvalid {
		t.Fatalf("want ErrPiecesLenInvalid, got %v", err)
	}
}

func TestInfoHash(t *testing.T) {
	info := map[string]any{
		"name":         "f",
		"piece length": int64(1),
		"pieces":       mkPieces(1),
		"length":       int64(1),
	}

	got, err := infoHash(info)
	if err != nil {
		t.Fatalf("infoHash error: %v", err)
	}
	b, _ := bencode.Marshal(info)
	want := sha1.Sum(b)
	if got != want {
		t.Fatalf("hash mismatch")
	}
}

func TestSize(t *testing.T) {
	// Single-file
	if got := (&Metainfo{Info: &Info{Length: 42}}).Size(); got != 42 {
		t.Fatalf("single-file total = %d, want 42", got)
	}

	// Multi-file
	got := (&Metainfo{Info: &Info{Files: []*File{{Length: 10}, {Length: 5}}}}).Size()
	if got != 15 {
		t.Fatalf("multi-file total = %d, want 15", got)
	}

	// Invalid (neither)
	if got := (&Metainfo{Info: &Info{}}).Size(); got != 0 {
		t.Fatalf("invalid total = %d, want 0", got)
	}
}

// contains is a tiny helper to avoid importing strings everywhere
func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

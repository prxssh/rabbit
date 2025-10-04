package torrent

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"time"

	"github.com/prxssh/rabbit/pkg/bencode"
	"github.com/prxssh/rabbit/pkg/utils/cast"
)

type Metainfo struct {
	Info         *Info      `json:"info"`
	Announce     string     `json:"announce"`
	AnnounceList [][]string `json:"announceList"`
	CreationDate time.Time  `json:"creationDate"`
	CreatedBy    string     `json:"createdBy"`
	Comment      string     `json:"comment"`
	Encoding     string     `json:"encoding"`
	URLs         []string   `json:"urls"`
}

type Info struct {
	Hash        [sha1.Size]byte   `json:"hash"`
	Name        string            `json:"name"`
	PieceLength int64             `json:"pieceLength"`
	Pieces      [][sha1.Size]byte `json:"pieces"`
	Private     bool              `json:"private"`
	Length      int64             `json:"length"`
	Files       []*File           `json:"files"`
}

type File struct {
	Length int64    `json:"length"`
	Path   []string `json:"path"`
}

func (m *Metainfo) Size() int64 {
	if m.Info.Length > 0 {
		return m.Info.Length
	}
	if len(m.Info.Files) == 0 {
		return -1
	}

	var sum int64
	for _, f := range m.Info.Files {
		sum += f.Length
	}

	return sum
}

var (
	ErrTopLevelNotDict = errors.New("metainfo: top-level is not a dict")
	ErrAnnounceMissing = errors.New(
		"metainfo: both announce and announce-list missing",
	)
	ErrInfoMissing     = errors.New("metainfo: 'info' missing")
	ErrInfoNotDict     = errors.New("metainfo: 'info' is not a dict")
	ErrNameMissing     = errors.New("metainfo: 'info' name missing")
	ErrPieceLenMissing = errors.New(
		"metainfo: 'info' piece length missing",
	)
	ErrPieceLenNonPositive = errors.New(
		"metainfo: 'info' piece length must be > 0",
	)
	ErrPiecesMissing    = errors.New("metainfo: 'info' pieces missing")
	ErrPiecesLenInvalid = errors.New(
		"metainfo: 'info' pieces length not multiple of 20",
	)
	ErrLayoutInvalid = errors.New(
		"metainfo: invalid single/multi-file layout",
	)
	ErrCreationDateInvalid = errors.New("metainfo: invalid creation date")
)

func ParseMetainfo(data []byte) (*Metainfo, error) {
	raw, err := bencode.Unmarshal(data)
	if err != nil {
		return nil, err
	}
	root, ok := raw.(map[string]any)
	if !ok {
		return nil, ErrTopLevelNotDict
	}

	announce, err := parseOptionalString(root["announce"])
	if err != nil {
		return nil, err
	}
	announceList, err := parseAnnounceList(root["announce-list"])
	if err != nil {
		return nil, err
	}
	if announce == "" && len(announceList) == 0 {
		return nil, ErrAnnounceMissing
	}

	var creationDate time.Time
	if v, ok := root["creation date"]; ok {
		secs, err := cast.ToInt(v)
		if err != nil || secs < 0 {
			return nil, ErrCreationDateInvalid
		}
		creationDate = time.Unix(secs, 0).UTC()
	}

	createdBy, err := parseOptionalString(root["created by"])
	if err != nil {
		return nil, err
	}
	comment, err := parseOptionalString(root["comment"])
	if err != nil {
		return nil, err
	}
	encoding, err := parseOptionalString(root["encoding"])
	if err != nil {
		return nil, err
	}

	info, err := parseInfo(root["info"])
	if err != nil {
		return nil, err
	}

	return &Metainfo{
		Info:         info,
		Announce:     announce,
		AnnounceList: announceList,
		CreationDate: creationDate,
		CreatedBy:    createdBy,
		Comment:      comment,
		Encoding:     encoding,
	}, nil
}

func parseInfo(anyInfo any) (*Info, error) {
	if anyInfo == nil {
		return nil, ErrInfoMissing
	}
	dict, ok := anyInfo.(map[string]any)
	if !ok {
		return nil, ErrInfoNotDict
	}

	var out Info

	h, err := infoHash(dict)
	if err != nil {
		return nil, fmt.Errorf("metainfo: info hash: %w", err)
	}
	out.Hash = h

	nameVal, ok := dict["name"]
	if !ok {
		return nil, ErrNameMissing
	}
	out.Name, err = cast.ToString(nameVal)
	if err != nil || out.Name == "" {
		return nil, fmt.Errorf("metainfo: invalid 'name': %w", err)
	}

	plVal, ok := dict["piece length"]
	if !ok {
		return nil, ErrPieceLenMissing
	}
	out.PieceLength, err = cast.ToInt(plVal)
	if err != nil || out.PieceLength <= 0 {
		return nil, ErrPieceLenNonPositive
	}

	out.Pieces, err = parsePieces(dict["pieces"])
	if err != nil {
		return nil, err
	}

	if v, ok := dict["private"]; ok {
		privInt, err := cast.ToInt(v)
		if err != nil || (privInt != 0 && privInt != 1) {
			return nil, fmt.Errorf(
				"metainfo: invalid 'private' flag",
			)
		}
		out.Private = privInt == 1
	}

	// Layout: either single-file ('length') or multi-file ('files')
	lengthVal, hasLength := dict["length"]
	filesVal, hasFiles := dict["files"]

	switch {
	case hasLength && !hasFiles:
		out.Length, err = cast.ToInt(lengthVal)
		if err != nil || out.Length < 0 {
			return nil, fmt.Errorf("metainfo: invalid 'length'")
		}
	case hasFiles && !hasLength:
		out.Files, err = parseFiles(filesVal)
		if err != nil {
			return nil, err
		}
	default:
		return nil, ErrLayoutInvalid
	}

	return &out, nil
}

func parseFiles(v any) ([]*File, error) {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil, fmt.Errorf("metainfo: invalid or empty 'files'")
	}

	files := make([]*File, 0, len(arr))
	for i, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, fmt.Errorf(
				"metainfo: files[%d]: not a dict",
				i,
			)
		}

		fl, ok := m["length"]
		if !ok {
			return nil, fmt.Errorf(
				"metainfo: files[%d]: length missing",
				i,
			)
		}
		ln, err := cast.ToInt(fl)
		if err != nil || ln < 0 {
			return nil, fmt.Errorf(
				"metainfo: files[%d]: invalid length",
				i,
			)
		}

		rawPath, ok := m["path"]
		if !ok {
			return nil, fmt.Errorf(
				"metainfo: files[%d]: path missing",
				i,
			)
		}
		segments, err := cast.ToStringSlice(rawPath)
		if err != nil || len(segments) == 0 {
			return nil, fmt.Errorf(
				"metainfo: files[%d]: invalid path",
				i,
			)
		}

		files = append(files, &File{Length: ln, Path: segments})
	}

	return files, nil
}

func parseAnnounceList(v any) ([][]string, error) {
	if v == nil {
		return [][]string{}, nil
	}
	raw, ok := v.([]any)
	if !ok {
		return [][]string{}, fmt.Errorf(
			"metainfo: invalid announce-list",
		)
	}
	tiered, err := cast.ToTieredStrings(raw)
	if err != nil {
		return [][]string{}, fmt.Errorf(
			"metainfo: invalid announce-list: %w",
			err,
		)
	}

	out := make([][]string, 0, len(tiered))
	for _, tier := range tiered {
		if len(tier) > 0 {
			out = append(out, tier)
		}
	}
	return out, nil
}

func parseOptionalString(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	return cast.ToString(v)
}

func infoHash(info map[string]any) ([sha1.Size]byte, error) {
	buf, err := bencode.Marshal(info)
	if err != nil {
		return [sha1.Size]byte{}, err
	}
	return sha1.Sum(buf), nil
}

func parsePieces(v any) ([][sha1.Size]byte, error) {
	if v == nil {
		return nil, ErrPiecesMissing
	}

	pieceBytes, err := cast.ToBytes(v)
	if err != nil {
		return nil, fmt.Errorf("metainfo: 'pieces': %w", err)
	}
	if len(pieceBytes)%sha1.Size != 0 {
		return nil, ErrPiecesLenInvalid
	}

	n := len(pieceBytes) / sha1.Size
	out := make([][sha1.Size]byte, n)
	for i := 0; i < n; i++ {
		copy(out[i][:], pieceBytes[i*sha1.Size:(i+1)*sha1.Size])
	}

	return out, nil
}

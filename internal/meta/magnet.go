package meta

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

type Magnet struct {
	InfoHash [sha1.Size]byte
	Name     string
	Trackers []string
}

func ParseMagnet(magnetURL string) (*Magnet, error) {
	u, err := url.Parse(magnetURL)
	if err != nil {
		return nil, fmt.Errorf("magnet url parse failed: %w", err)
	}
	if u.Scheme != "magnet" {
		return nil, fmt.Errorf("invalid magnet scheme '%s'", u.Scheme)
	}

	params, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("magnet params parse failed: %w", err)
	}

	magnet := &Magnet{}

	xt, ok := params["xt"]
	if !ok || len(xt) == 0 {
		return nil, fmt.Errorf("magnet url missing 'xt'")
	}
	xtVal := xt[0]
	if !strings.HasPrefix(xtVal, "urn:btih:") {
		return nil, fmt.Errorf("invalid 'xt' value: must be in 'urn:btih:<hash>' format")
	}

	hashString := strings.TrimPrefix(xtVal, "urn:btih:")
	if len(hashString) != sha1.Size*2 { // 20 bytes = 40 hex chars
		return nil, fmt.Errorf("invalid infohash length")
	}
	hashBytes, err := hex.DecodeString(hashString)
	if err != nil {
		return nil, fmt.Errorf("failed to decode infohash: %w", err)
	}
	copy(magnet.InfoHash[:], hashBytes)

	if dn, ok := params["dn"]; ok && len(dn) > 0 {
		magnet.Name = dn[0]
	}

	if tr, ok := params["tr"]; ok {
		magnet.Trackers = tr
	}

	return magnet, nil
}

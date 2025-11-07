package meta

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func mustDecodeInfoHash(s string) [sha1.Size]byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(fmt.Sprintf("test setup failed: bad hex string '%s': %v", s, err))
	}
	var arr [sha1.Size]byte
	copy(arr[:], b)
	return arr
}

func TestParseMagnet(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      *Magnet
		wantErr   bool
		errSubstr string
	}{
		// --- Happy Path Cases ---
		{
			name:  "Full Link (xt, dn, multi-tr)",
			input: "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a&dn=ubuntu-22.04.1-desktop-amd64.iso&tr=udp%3A%2F%2Ftracker.openbittorrent.com%3A80&tr=udp%3A%2F%2Ftracker.publicbt.com%3A80",
			want: &Magnet{
				InfoHash: mustDecodeInfoHash(
					"c12fe1c06bba254a9dc9f519b335aa7c1367a88a",
				),
				Name: "ubuntu-22.04.1-desktop-amd64.iso",
				Trackers: []string{
					"udp://tracker.openbittorrent.com:80",
					"udp://tracker.publicbt.com:80",
				},
			},
			wantErr: false,
		},
		{
			name:  "Minimal Link (xt only)",
			input: "magnet:?xt=urn:btih:0000000000000000000000000000000000000001",
			want: &Magnet{
				InfoHash: mustDecodeInfoHash(
					"0000000000000000000000000000000000000001",
				),
				Name:     "",  // Expect empty string, not nil
				Trackers: nil, // Expect nil slice, not empty slice
			},
			wantErr: false,
		},
		{
			name:  "Link with dn, no tr",
			input: "magnet:?xt=urn:btih:1111111111111111111111111111111111111111&dn=My+File.zip",
			want: &Magnet{
				InfoHash: mustDecodeInfoHash(
					"1111111111111111111111111111111111111111",
				),
				Name:     "My File.zip",
				Trackers: nil,
			},
			wantErr: false,
		},
		{
			name:  "Link with tr, no dn",
			input: "magnet:?xt=urn:btih:2222222222222222222222222222222222222222&tr=http%3A%2F%2Ftracker.example.com",
			want: &Magnet{
				InfoHash: mustDecodeInfoHash(
					"2222222222222222222222222222222222222222",
				),
				Name:     "",
				Trackers: []string{"http://tracker.example.com"},
			},
			wantErr: false,
		},

		// --- Error Cases ---
		{
			name:      "Invalid URL format",
			input:     "://invalid-url",
			wantErr:   true,
			errSubstr: "magnet url parse failed",
		},
		{
			name:      "Wrong scheme",
			input:     "http://example.com/magnet:?xt=urn:btih:1111111111111111111111111111111111111111",
			wantErr:   true,
			errSubstr: "invalid magnet scheme 'http'",
		},
		{
			name:      "Missing xt",
			input:     "magnet:?dn=test.file",
			wantErr:   true,
			errSubstr: "magnet url missing 'xt'",
		},
		{
			name:      "Invalid xt prefix",
			input:     "magnet:?xt=urn:btihh:1111111111111111111111111111111111111111",
			wantErr:   true,
			errSubstr: "invalid 'xt' value",
		},
		{
			name:      "InfoHash too short",
			input:     "magnet:?xt=urn:btih:11111111",
			wantErr:   true,
			errSubstr: "invalid infohash length",
		},
		{
			name:      "InfoHash too long",
			input:     "magnet:?xt=urn:btih:11111111111111111111111111111111111111112222222222",
			wantErr:   true,
			errSubstr: "invalid infohash length",
		},
		{
			name:      "InfoHash not hex",
			input:     "magnet:?xt=urn:btih:ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ", // Z is not a hex char
			wantErr:   true,
			errSubstr: "failed to decode infohash",
		},
		{
			name:      "Invalid query string",
			input:     "magnet:?xt=urn:btih:1111111111111111111111111111111111111111&%=",
			wantErr:   true,
			errSubstr: "magnet params parse failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMagnet(tt.input)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseMagnet() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected an error, but got nil")
				}
				if !strings.Contains(fmt.Sprint(err), tt.errSubstr) {
					t.Errorf(
						"ParseMagnet() error = %v, want error to contain '%s'",
						err,
						tt.errSubstr,
					)
				}
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"ParseMagnet() mismatch:\ngot  = %+v\nwant = %+v",
					got,
					tt.want,
				)
			}
		})
	}
}

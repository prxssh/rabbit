package bencode

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func decodeFromString(t *testing.T, s string) (any, error) {
	t.Helper()

	d := NewDecoder([]byte(s))
	return d.Decode()
}

func wantErrContains(t *testing.T, err error, substr string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error = %v, want contains %q", err, substr)
	}
}

func TestDecode_OK(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want any
	}{
		{"string", "4:spam", any("spam")},
		{"empty-string", "0:", any("")},
		{"int-neg", "i-1e", any(int64(-1))},
		{"int-zero", "i0e", any(int64(0))},
		{"int-pos", "i42e", any(int64(42))},
		{"list-simple", "l4:spami1ee", any([]any{"spam", int64(1)})},
		{
			"list-nested",
			"li1e4:spami0el6:nestedi2eee",
			any([]any{int64(1), "spam", int64(0), []any{"nested", int64(2)}}),
		},
		{
			"dict",
			"d1:ai1e1:bi2e1:cl1:xi3eee",
			any(
				map[string]any{
					"a": int64(1),
					"b": int64(2),
					"c": []any{"x", int64(3)},
				},
			),
		},
		{
			"nested-structures",
			"d8:announce14:http://tracker4:infod6:lengthi1024e4:name10:ubuntu.iso6:piecesl3:abc3:defeee",
			any(
				map[string]any{
					"announce": "http://tracker",
					"info": map[string]any{
						"length": int64(1024),
						"name":   "ubuntu.iso",
						"pieces": []any{"abc", "def"},
					},
				},
			),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := decodeFromString(t, tc.in)
			if err != nil {
				t.Fatalf("Decode error: %v", err)
			}
			if !reflect.DeepEqual(v, tc.want) {
				t.Fatalf("got %#v, want %#v", v, tc.want)
			}
		})
	}
}

func TestDecodeErrors_IntegerFormat(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"leading-zero", "i012e", "invalid integer: leading zero"},
		{"negative-zero", "i-0e", "invalid integer: negative zero"},
		{"empty", "ie", "invalid integer: empty"},
		{"lone-dash", "i-e", "invalid integer:"},
		{
			"too-many-digits",
			"i" + strings.Repeat("1", 21) + "e",
			"invalid integer: too many digits",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeFromString(t, tc.in)
			wantErrContains(t, err, tc.want)
		})
	}
}

func TestDecodeErrors_IntegerTooLong(t *testing.T) {
	_, err := decodeFromString(t, "i"+strings.Repeat("1", 5000))
	wantErrContains(t, err, "integer too long")
}

func TestDecodeErrors_StringLength(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"leading-zero", "01:", "invalid integer: leading zero"},
		{"negative-len", "-1:", "invalid string: length can't be negative"},
		{"truncated-bytes", "5:abc", "read string"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeFromString(t, tc.in)
			wantErrContains(t, err, tc.want)
		})
	}
}

func TestDecodeErrors_TruncatedContainers(t *testing.T) {
	tests := []struct{ name, in string }{
		{"list", "l"},
		{"dict", "d"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeFromString(t, tc.in); err == nil {
				t.Fatalf("expected error for truncated %s, got nil", tc.name)
			}
		})
	}
}

func TestUnmarshal_OK(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want any
	}{
		{"string", []byte("4:spam"), any("spam")},
		{"int", []byte("i42e"), any(int64(42))},
		{"list", []byte("l4:spami1ee"), any([]any{"spam", int64(1)})},
		{"dict", []byte("d1:ai1ee"), any(map[string]any{"a": int64(1)})},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := Unmarshal(tc.in)
			if err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if !reflect.DeepEqual(v, tc.want) {
				t.Fatalf("got %#v, want %#v", v, tc.want)
			}
		})
	}
}

func TestUnmarshal_Errors(t *testing.T) {
	tests := []struct {
		name   string
		in     []byte
		want   string
		wantIs error
	}{
		{
			name: "trailing",
			in:   []byte("i1ei2e"),
			want: "bencoding: trailing data after first value",
		},
		{name: "empty", in: nil, wantIs: io.EOF},
		{name: "decode-error", in: []byte("i-e"), want: "invalid integer:"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Unmarshal(tc.in)

			if tc.wantIs != nil {
				if !errors.Is(err, tc.wantIs) {
					t.Fatalf("want %v, got %v", tc.wantIs, err)
				}
				return
			}

			wantErrContains(t, err, tc.want)
		})
	}
}

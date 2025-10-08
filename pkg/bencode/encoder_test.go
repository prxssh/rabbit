package bencode

import (
	"bytes"
	"strconv"
	"testing"
)

func encodeToString(t *testing.T, v any) string {
	t.Helper()

	var buf bytes.Buffer
	e := NewEncoder(&buf)

	if err := e.Encode(v); err != nil {
		t.Fatalf("Encode(%T) error: %v", v, err)
	}
	return buf.String()
}

func TestEncode_Primitives(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"string", "spam", "4:spam"},
		{"empty-string", "", "0:"},
		{"bytes", []byte("eggs"), "4:eggs"},

		{"bool-true", true, "i1e"},
		{"bool-false", false, "i0e"},

		{"int-1", int(-1), "i-1e"},
		{"int0", int(0), "i0e"},
		{"int42", int(42), "i42e"},
		{"int8-8", int8(-8), "i-8e"},
		{"int16", int16(32000), "i32000e"},
		{"int32", int32(-123456), "i-123456e"},
		{"int64", int64(9007199254740991), "i9007199254740991e"},

		{"uint0", uint(0), "i0e"},
		{"uint42", uint(42), "i42e"},
		{"uint8", uint8(255), "i255e"},
		{"uint16", uint16(65535), "i65535e"},
		{"uint32", uint32(4000000000), "i4000000000e"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeToString(t, tc.in)

			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}

	// uint64 max as a separate case due to construction
	t.Run("uint64-max", func(t *testing.T) {
		max := ^uint64(0)
		got := encodeToString(t, max)

		want := "i" + strconv.FormatUint(max, 10) + "e"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestEncode_Collections(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "slice-nested",
			in:   []any{int64(1), "spam", false, []any{"nested", int(2)}},
			want: "li1e4:spami0el6:nestedi2eee",
		},
		{
			name: "dict-sorted-keys",
			in: map[string]any{
				"b": int(2),
				"a": int(1),
				"c": []any{"x", int(3)},
			},
			want: "d1:ai1e1:bi2e1:cl1:xi3eee",
		},
		{
			name: "nested-structures",
			in: map[string]any{
				"info": map[string]any{
					"name":   "ubuntu.iso",
					"length": int64(1024),
					"pieces": []any{"abc", "def"},
				},
				"announce": "http://tracker",
			},
			want: "d8:announce14:http://tracker4:infod6:lengthi1024e4:name10:ubuntu.iso6:piecesl3:abc3:defeee",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeToString(t, tc.in)

			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMarshal(t *testing.T) {
	tests := []struct {
		name            string
		in              any
		want            string
		wantErrContains string
	}{
		{name: "list", in: []any{"a", int(1)}, want: "l1:ai1ee"},
		{name: "unsupported", in: struct{}{}, wantErrContains: "unsupported datatype"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := Marshal(tc.in)

			if tc.wantErrContains != "" {
				if err == nil {
					t.Fatalf(
						"expected error containing %q, got nil",
						tc.wantErrContains,
					)
				}
				if err != nil &&
					!bytes.Contains(
						[]byte(err.Error()),
						[]byte(tc.wantErrContains),
					) {
					t.Fatalf(
						"error = %v, want contains %q",
						err,
						tc.wantErrContains,
					)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := string(b)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

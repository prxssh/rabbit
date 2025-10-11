package bitfield

import "testing"

func TestNewSizeRounding(t *testing.T) {
	cases := []struct {
		nBits     int
		wantBytes int
	}{
		{0, 0},
		{1, 1},
		{7, 1},
		{8, 1},
		{9, 2},
		{16, 2},
		{17, 3},
	}

	for _, tc := range cases {
		bf := New(tc.nBits)
		if got := len(bf); got != tc.wantBytes {
			t.Fatalf(
				"New(%d) bytes = %d; want %d",
				tc.nBits,
				got,
				tc.wantBytes,
			)
		}
	}
}

func TestSetHasClearAndBounds(t *testing.T) {
	bf := New(10) // 2 bytes

	if bf.Has(-1) || bf.Has(100) {
		t.Fatalf("Has out-of-range should be false")
	}

	// Set bits at 0,7,8,9
	idxs := []int{0, 7, 8, 9}
	for _, i := range idxs {
		bf.Set(i)
	}
	for _, i := range idxs {
		if !bf.Has(i) {
			t.Fatalf("bit %d should be set", i)
		}
	}

	// Clear one and verify
	bf.Clear(7)
	if bf.Has(7) {
		t.Fatalf("bit 7 should be cleared")
	}

	// Out-of-range operations must not panic or affect valid bits
	bf.Set(100)
	bf.Clear(-42)
	for _, i := range []int{0, 8, 9} {
		if !bf.Has(i) {
			t.Fatalf("bit %d unexpectedly cleared by OOB ops", i)
		}
	}
}

func TestFromBytesAndToBytesIndependence(t *testing.T) {
	src := []byte{0xFF, 0x00}
	bf := FromBytes(src)

	// mutate src; bf should be unchanged
	src[0] = 0x00
	if !bf.Equals(Bitfield{0xFF, 0x00}) {
		t.Fatalf("FromBytes must copy input")
	}

	out := bf.Bytes()
	out[1] = 0xAA
	if bf[1] != 0x00 {
		t.Fatalf("Bytes must return a copy, not alias")
	}
}

func TestStringRepresentation(t *testing.T) {
	bf := FromBytes([]byte{0xA5, 0x01}) // 1010 0101 0000 0001
	got := bf.String()
	want := "1010010100000001"
	if got != want {
		t.Fatalf("String() = %q; want %q", got, want)
	}
}

func TestCountAndEquals(t *testing.T) {
	bf := New(10)
	bf.Set(0)
	bf.Set(2)
	bf.Set(3)
	bf.Set(8)

	if got := bf.Count(); got != 4 {
		t.Fatalf("Count() = %d; want %d", got, 4)
	}

	same := FromBytes(bf.Bytes())
	if !bf.Equals(same) {
		t.Fatalf("Equals should report identical contents")
	}

	diff := FromBytes(bf.Bytes())
	diff.Set(9)
	if bf.Equals(diff) {
		t.Fatalf("Equals should detect difference")
	}
}

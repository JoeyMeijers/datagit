package format

import (
	"errors"
	"testing"
)

func TestUvarint(t *testing.T) {
	cases := []struct {
		in   []byte
		want uint64
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x7f}, 127},
		{[]byte{0x80, 0x01}, 128},
		{[]byte{0xac, 0x02}, 300},
	}
	for _, c := range cases {
		r := &thriftReader{buf: c.in}
		got, err := r.uvarint()
		if err != nil || got != c.want {
			t.Errorf("uvarint(%v) = %d,%v want %d", c.in, got, err, c.want)
		}
	}
}

func TestUvarintTruncated(t *testing.T) {
	r := &thriftReader{buf: []byte{0x80}} // continuation bit but no next byte
	if _, err := r.uvarint(); !errors.Is(err, errTruncated) {
		t.Fatalf("err = %v, want errTruncated", err)
	}
}

func TestVarintZigzag(t *testing.T) {
	cases := []struct {
		in   []byte
		want int64
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, -1},
		{[]byte{0x02}, 1},
		{[]byte{0x03}, -2},
		{[]byte{0x04}, 2},
	}
	for _, c := range cases {
		r := &thriftReader{buf: c.in}
		got, err := r.varint()
		if err != nil || got != c.want {
			t.Errorf("varint(%v) = %d,%v want %d", c.in, got, err, c.want)
		}
	}
}

func TestBinaryTruncated(t *testing.T) {
	r := &thriftReader{buf: []byte{0x05, 'a', 'b'}} // claims 5 bytes, has 2
	if _, err := r.binary(); !errors.Is(err, errTruncated) {
		t.Fatalf("err = %v, want errTruncated", err)
	}
}

func TestFieldHeaderDelta(t *testing.T) {
	// Field ID 1 (delta 1), type i64; then field ID 3 (delta 2), type i32.
	r := &thriftReader{buf: []byte{0x16, 0x00, 0x25, 0x00, 0x00}}
	id, typ, done, err := r.fieldHeader(0)
	if err != nil || done || id != 1 || typ != tI64 {
		t.Fatalf("first: id=%d typ=%d done=%v err=%v", id, typ, done, err)
	}
	if err := r.skip(typ); err != nil {
		t.Fatal(err)
	}
	id, typ, done, err = r.fieldHeader(id)
	if err != nil || done || id != 3 || typ != tI32 {
		t.Fatalf("second: id=%d typ=%d done=%v err=%v", id, typ, done, err)
	}
}

func TestSkipUnknownType(t *testing.T) {
	r := &thriftReader{buf: []byte{0x00}}
	if err := r.skip(13); err == nil {
		t.Fatal("skip of invalid type should fail")
	}
}

func TestSkipList(t *testing.T) {
	// List of 2 booleans (one byte each in collections), then a
	// trailing byte we must land on exactly.
	r := &thriftReader{buf: []byte{0x21, 0x01, 0x02, 0xEE}}
	if err := r.skip(tList); err != nil {
		t.Fatal(err)
	}
	b, err := r.byte()
	if err != nil || b != 0xEE {
		t.Fatalf("landed wrong: b=%x err=%v", b, err)
	}
}

func TestSkipMap(t *testing.T) {
	// Map with 1 entry: key binary "ab", value i32 5, then trailing byte.
	r := &thriftReader{buf: []byte{0x01, 0x85, 0x02, 'a', 'b', 0x0A, 0xEE}}
	if err := r.skip(tMap); err != nil {
		t.Fatal(err)
	}
	b, err := r.byte()
	if err != nil || b != 0xEE {
		t.Fatalf("landed wrong: b=%x err=%v", b, err)
	}
}

func TestSkipListHugeSizeRejected(t *testing.T) {
	// Size nibble 15 → varint size 1<<40: must be rejected, not OOM/hang.
	r := &thriftReader{buf: []byte{0xF8, 0x80, 0x80, 0x80, 0x80, 0x80, 0x20}}
	if err := r.skip(tList); err == nil {
		t.Fatal("expected error for oversized list")
	}
}

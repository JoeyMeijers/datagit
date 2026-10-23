package storage

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRawPutRawRoundTrip(t *testing.T) {
	src := roundTrip(t, []byte("a,b\n1,2\n"))
	hashes, err := src.Objects()
	if err != nil || len(hashes) != 1 {
		t.Fatalf("objects=%v err=%v", hashes, err)
	}
	h := hashes[0]

	raw, err := src.OpenRaw(h)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := io.ReadAll(raw)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}

	dst := NewStore(filepath.Join(t.TempDir(), "objects"))
	if err := dst.PutRaw(h, bytes.NewReader(blob)); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := dst.ReadChunk(h, &out); err != nil {
		t.Fatalf("transferred chunk unreadable: %v", err)
	}
	if out.String() != "a,b\n1,2\n" {
		t.Fatalf("content = %q", out.String())
	}
	// Re-putting an existing object is a no-op, even with bad bytes.
	if err := dst.PutRaw(h, strings.NewReader("ignored")); err != nil {
		t.Fatal(err)
	}
	if err := dst.ReadChunk(h, io.Discard); err != nil {
		t.Fatalf("existing object was overwritten: %v", err)
	}
}

func TestPutRawGarbageFailsVerification(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	h := strings.Repeat("ab", 32)
	if err := s.PutRaw(h, strings.NewReader("not zstd")); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadChunk(h, io.Discard); err == nil {
		t.Fatal("garbage object passed verification")
	}
}

func TestRawRejectsInvalidAndMissing(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	if _, err := s.OpenRaw("../x"); err == nil {
		t.Fatal("OpenRaw accepted invalid hash")
	}
	if err := s.PutRaw("../x", strings.NewReader("")); err == nil {
		t.Fatal("PutRaw accepted invalid hash")
	}
	if _, err := s.OpenRaw(strings.Repeat("0", 64)); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("err = %v, want ErrObjectNotFound", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestPutRawPropagatesReadError(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	h := strings.Repeat("cd", 32)
	if err := s.PutRaw(h, failingReader{}); err == nil {
		t.Fatal("read error swallowed")
	}
	if s.Has(h) {
		t.Fatal("partial object left behind after failed PutRaw")
	}
}

func TestPutFilePropagatesReadError(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	if _, _, _, err := s.PutFile(failingReader{}); err == nil {
		t.Fatal("read error swallowed")
	}
}

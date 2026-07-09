package storage

import (
	"bytes"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// testData returns size bytes of deterministic pseudo-random data.
func testData(t *testing.T, size int) []byte {
	t.Helper()
	buf := make([]byte, size)
	rand.New(rand.NewSource(42)).Read(buf)
	return buf
}

func roundTrip(t *testing.T, data []byte) *Store {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	manifest, sum, size, err := s.PutFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	if size != int64(len(data)) {
		t.Fatalf("size = %d, want %d", size, len(data))
	}
	if len(sum) != 64 {
		t.Fatalf("file hash length = %d, want 64", len(sum))
	}
	var out bytes.Buffer
	if err := s.RestoreFile(manifest, &out); err != nil {
		t.Fatalf("RestoreFile: %v", err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Fatal("restored data differs from original")
	}
	return s
}

func TestPutFileEmpty(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	manifest, _, size, err := s.PutFile(bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	if size != 0 || len(manifest.Chunks) != 0 {
		t.Fatalf("empty file: size=%d chunks=%d, want 0/0", size, len(manifest.Chunks))
	}
}

func TestPutFileSingleChunk(t *testing.T) {
	roundTrip(t, []byte("hello,world\n1,2\n"))
}

func TestPutFileMultiChunk(t *testing.T) {
	// 9 MiB spans three 4 MiB chunks (4+4+1).
	data := testData(t, 9<<20)
	s := roundTrip(t, data)

	manifest, _, _, err := s.PutFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("second PutFile: %v", err)
	}
	if got := len(manifest.Chunks); got != 3 {
		t.Fatalf("chunks = %d, want 3", got)
	}
}

func TestDeduplication(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	data := testData(t, 5<<20)
	m1, _, _, err := s.PutFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	m2, _, _, err := s.PutFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(m1.Chunks) != len(m2.Chunks) {
		t.Fatalf("chunk counts differ: %d vs %d", len(m1.Chunks), len(m2.Chunks))
	}
	for i := range m1.Chunks {
		if m1.Chunks[i].Hash != m2.Chunks[i].Hash {
			t.Fatalf("chunk %d hash differs", i)
		}
	}
	// Identical content must not be stored twice.
	var objects int
	err = filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			objects++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if objects != len(m1.Chunks) {
		t.Fatalf("stored objects = %d, want %d (dedup failed)", objects, len(m1.Chunks))
	}
}

func TestReadChunkNotFound(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	hash := "0000000000000000000000000000000000000000000000000000000000000000"
	err := s.ReadChunk(hash, &bytes.Buffer{})
	if !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("err = %v, want ErrObjectNotFound", err)
	}
}

func TestInvalidHashRejected(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	for _, h := range []string{"", "abc", "../../etc/passwd", string(make([]byte, 64))} {
		if err := s.ReadChunk(h, &bytes.Buffer{}); err == nil {
			t.Fatalf("hash %q: expected error, got nil", h)
		}
	}
}

func TestVerifyAllDetectsCorruption(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	manifest, _, _, err := s.PutFile(bytes.NewReader([]byte("some tabular data")))
	if err != nil {
		t.Fatal(err)
	}
	bad, err := s.VerifyAll()
	if err != nil || len(bad) != 0 {
		t.Fatalf("healthy store: bad=%v err=%v", bad, err)
	}
	// Corrupt the object on disk.
	h := manifest.Chunks[0].Hash
	p := filepath.Join(s.root, h[:2], h[2:])
	if err := os.WriteFile(p, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad, err = s.VerifyAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 1 || bad[0] != h {
		t.Fatalf("bad = %v, want [%s]", bad, h)
	}
}

func TestObjectsRemoveSizeOf(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))

	// Empty store lists nothing.
	objs, err := s.Objects()
	if err != nil || len(objs) != 0 {
		t.Fatalf("objs=%v err=%v", objs, err)
	}

	manifest, _, _, err := s.PutFile(bytes.NewReader([]byte("chunk one")))
	if err != nil {
		t.Fatal(err)
	}
	h := manifest.Chunks[0].Hash

	objs, err = s.Objects()
	if err != nil || len(objs) != 1 || objs[0] != h {
		t.Fatalf("objs=%v err=%v", objs, err)
	}

	size, err := s.SizeOf(h)
	if err != nil || size <= 0 {
		t.Fatalf("size=%d err=%v", size, err)
	}

	freed, err := s.Remove(h)
	if err != nil || freed != size {
		t.Fatalf("freed=%d err=%v, want %d", freed, err, size)
	}
	if s.Has(h) {
		t.Fatal("object still present after Remove")
	}

	// Removing again and sizing a missing object both fail cleanly.
	if _, err := s.Remove(h); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("err = %v, want ErrObjectNotFound", err)
	}
	if _, err := s.SizeOf(h); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("err = %v, want ErrObjectNotFound", err)
	}
	// Invalid hashes are rejected before touching the filesystem.
	if _, err := s.Remove("../nope"); err == nil {
		t.Fatal("invalid hash should fail")
	}
}

func TestVerifyAllEmptyStore(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "does-not-exist"))
	bad, err := s.VerifyAll()
	if err != nil || len(bad) != 0 {
		t.Fatalf("empty store: bad=%v err=%v", bad, err)
	}
}

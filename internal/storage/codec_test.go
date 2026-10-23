package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// writeObject stores raw object bytes under hash, bypassing putChunk.
func writeObject(t *testing.T, s *Store, hash string, raw []byte) {
	t.Helper()
	p, err := s.objectPath(hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestReadsObjectsFromStreamingEncoder(t *testing.T) {
	// Repositories created before the shared codec hold objects written
	// by a per-chunk streaming encoder; they must stay readable.
	for _, size := range []int{1, 4 << 20, maxChunk} {
		chunk := testData(t, size)
		var raw bytes.Buffer
		enc, err := zstd.NewWriter(&raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := enc.Write(chunk); err != nil {
			t.Fatal(err)
		}
		if err := enc.Close(); err != nil {
			t.Fatal(err)
		}
		s := NewStore(filepath.Join(t.TempDir(), "objects"))
		writeObject(t, s, hashOf(chunk), raw.Bytes())

		var out bytes.Buffer
		if err := s.ReadChunk(hashOf(chunk), &out); err != nil {
			t.Fatalf("%d bytes: %v", size, err)
		}
		if !bytes.Equal(out.Bytes(), chunk) {
			t.Fatalf("%d bytes: content differs", size)
		}
	}
}

func TestReadChunkRejectsDecompressionBomb(t *testing.T) {
	// 64 MiB of zeros compresses to a few KB: a hostile remote could
	// ship it to exhaust memory. It exceeds any valid chunk, so reject.
	huge := make([]byte, 64<<20)
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := enc.EncodeAll(huge, nil)
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	writeObject(t, s, hashOf(huge), raw)

	err = s.ReadChunk(hashOf(huge), io.Discard)
	if !errors.Is(err, ErrCorruptObject) {
		t.Fatalf("err = %v, want ErrCorruptObject", err)
	}
}

func TestReadChunkRejectsOversizedObject(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	hash := hashOf([]byte("x"))
	writeObject(t, s, hash, make([]byte, maxStoredObject+1))
	if err := s.ReadChunk(hash, io.Discard); err == nil {
		t.Fatal("oversized object accepted")
	}
}

func TestReadChunkWritesNothingWhenCorrupt(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	// Valid zstd, wrong content for the hash it is stored under.
	writeObject(t, s, hashOf([]byte("expected")), enc.EncodeAll([]byte("tampered"), nil))
	var out bytes.Buffer
	if err := s.ReadChunk(hashOf([]byte("expected")), &out); !errors.Is(err, ErrCorruptObject) {
		t.Fatalf("err = %v, want ErrCorruptObject", err)
	}
	if out.Len() != 0 {
		t.Fatalf("corrupt data reached the writer: %q", out.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestReadChunkPropagatesWriteError(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	m, _, _, err := s.PutFile(bytes.NewReader([]byte("a,b\n")))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReadChunk(m.Chunks[0].Hash, failWriter{}); err == nil {
		t.Fatal("write error swallowed")
	}
}

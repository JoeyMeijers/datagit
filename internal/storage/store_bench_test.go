package storage

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"testing"
)

// csvData returns deterministic CSV-like text of roughly size bytes:
// compressible, line-structured, like the datasets datagit targets.
func csvData(size int) []byte {
	var b bytes.Buffer
	b.WriteString("id,region,income,date\n")
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, "%d,region-%02d,%d.%02d,2026-%02d-%02d\n",
			i, i%37, (i*7919)%100000, i%100, i%12+1, i%28+1)
	}
	return b.Bytes()
}

func benchPutFile(b *testing.B, data []byte) {
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		// A fresh store each time so every chunk is really written.
		s := NewStore(filepath.Join(b.TempDir(), "objects"))
		if _, _, _, err := s.PutFile(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPutFileRandom64MiB(b *testing.B) {
	benchPutFile(b, testData(b, 64<<20))
}

func BenchmarkPutFileCSV64MiB(b *testing.B) { benchPutFile(b, csvData(64<<20)) }

func benchRestoreFile(b *testing.B, data []byte) {
	s := NewStore(filepath.Join(b.TempDir(), "objects"))
	m, _, _, err := s.PutFile(bytes.NewReader(data))
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if err := s.RestoreFile(m, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRestoreFileRandom64MiB(b *testing.B) { benchRestoreFile(b, testData(b, 64<<20)) }

func BenchmarkRestoreFileCSV64MiB(b *testing.B) { benchRestoreFile(b, csvData(64<<20)) }

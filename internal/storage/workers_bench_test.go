package storage

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"testing"
)

// BenchmarkWorkers measures add/restore scaling with the worker count,
// to justify maxWorkers (ADR-0012).
func BenchmarkWorkers(b *testing.B) {
	inputs := map[string][]byte{"csv": csvData(64 << 20), "random": testData(b, 64<<20)}
	for _, name := range []string{"csv", "random"} {
		data := inputs[name]
		for _, w := range []int{1, 2, 4, 8} {
			b.Run(fmt.Sprintf("put/%s/w%d", name, w), func(b *testing.B) {
				b.SetBytes(int64(len(data)))
				for b.Loop() {
					s := NewStore(filepath.Join(b.TempDir(), "objects"))
					s.workers = w
					if _, _, _, err := s.PutFile(bytes.NewReader(data)); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run(fmt.Sprintf("restore/%s/w%d", name, w), func(b *testing.B) {
				s := NewStore(filepath.Join(b.TempDir(), "objects"))
				m, _, _, err := s.PutFile(bytes.NewReader(data))
				if err != nil {
					b.Fatal(err)
				}
				s.workers = w
				b.SetBytes(int64(len(data)))
				for b.Loop() {
					if err := s.RestoreFile(m, io.Discard); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

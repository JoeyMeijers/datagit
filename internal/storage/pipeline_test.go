package storage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/joeymeij/datagit/internal/model"
)

// noLeakedGoroutines fails if goroutines started during the test are
// still running shortly after it ends.
func noLeakedGoroutines(t *testing.T) {
	t.Helper()
	before := runtime.NumGoroutine()
	t.Cleanup(func() {
		deadline := time.Now().Add(2 * time.Second)
		for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if n := runtime.NumGoroutine(); n > before {
			t.Errorf("%d goroutines leaked", n-before)
		}
	})
}

func storeWithWorkers(t *testing.T, w int) *Store {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "objects"))
	s.workers = w
	return s
}

func TestPutFileIndependentOfWorkerCount(t *testing.T) {
	for _, size := range []int{0, 1, minChunk, 5 * maxChunk / 2, 40 << 20} {
		data := testData(t, size)
		ref, refSum, refSize, err := storeWithWorkers(t, 1).PutFile(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range []int{2, 4, 16} {
			m, sum, n, err := storeWithWorkers(t, w).PutFile(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(m.Chunks, ref.Chunks) || sum != refSum || n != refSize {
				t.Fatalf("size %d, %d workers: result differs from sequential", size, w)
			}
		}
	}
}

func TestPutFileStopsCleanlyOnStoreError(t *testing.T) {
	noLeakedGoroutines(t)
	// The store root is a file, so creating any object directory fails.
	root := filepath.Join(t.TempDir(), "objects")
	if err := os.WriteFile(root, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, w := range []int{1, 4} {
		s := NewStore(root)
		s.workers = w
		if _, _, _, err := s.PutFile(bytes.NewReader(testData(t, 30<<20))); err == nil {
			t.Fatalf("%d workers: store error swallowed", w)
		}
	}
}

func TestPutFileStopsCleanlyOnReadError(t *testing.T) {
	noLeakedGoroutines(t)
	if _, _, _, err := storeWithWorkers(t, 4).PutFile(failingReader{}); err == nil {
		t.Fatal("read error swallowed")
	}
}

func TestRestoreFileStopsAtFirstFailedChunk(t *testing.T) {
	noLeakedGoroutines(t)
	data := testData(t, 40<<20)
	for _, w := range []int{1, 4} {
		s := storeWithWorkers(t, w)
		m, _, _, err := s.PutFile(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Chunks) < 6 {
			t.Fatalf("need several chunks, got %d", len(m.Chunks))
		}
		const broken = 3
		if _, err := s.Remove(m.Chunks[broken].Hash); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := s.RestoreFile(m, &out); !errors.Is(err, ErrObjectNotFound) {
			t.Fatalf("%d workers: err = %v, want ErrObjectNotFound", w, err)
		}
		var prefix int64
		for _, c := range m.Chunks[:broken] {
			prefix += c.Size
		}
		if !bytes.Equal(out.Bytes(), data[:prefix]) {
			t.Fatalf("%d workers: wrote %d bytes, want exactly the %d before the failed chunk", w, out.Len(), prefix)
		}
	}
}

func TestRestoreFileStopsOnWriteError(t *testing.T) {
	noLeakedGoroutines(t)
	s := storeWithWorkers(t, 4)
	m, _, _, err := s.PutFile(bytes.NewReader(testData(t, 30<<20)))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreFile(m, failWriter{}); err == nil {
		t.Fatal("write error swallowed")
	}
}

func TestRestoreFileEmptyManifest(t *testing.T) {
	var out bytes.Buffer
	if err := storeWithWorkers(t, 4).RestoreFile(model.Manifest{}, &out); err != nil || out.Len() != 0 {
		t.Fatalf("err=%v wrote=%d", err, out.Len())
	}
}

func TestConcurrentPutFileOnOneStore(t *testing.T) {
	s := storeWithWorkers(t, 4)
	data := testData(t, 20<<20)
	results := make([]model.Manifest, 6)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() {
			m, _, _, err := s.PutFile(bytes.NewReader(data))
			if err != nil {
				t.Error(err)
			}
			results[i] = m
		})
	}
	wg.Wait()
	for _, m := range results[1:] {
		if !slices.Equal(m.Chunks, results[0].Chunks) {
			t.Fatal("concurrent adds of identical data produced different manifests")
		}
	}
	if bad, err := s.VerifyAll(); err != nil || len(bad) != 0 {
		t.Fatalf("store unhealthy after concurrent adds: bad=%v err=%v", bad, err)
	}
	var out bytes.Buffer
	if err := s.RestoreFile(results[0], &out); err != nil || !bytes.Equal(out.Bytes(), data) {
		t.Fatalf("restore after concurrent adds: err=%v equal=%v", err, bytes.Equal(out.Bytes(), data))
	}
}

func TestVerifyAllReportsEveryCorruptObjectSorted(t *testing.T) {
	s := storeWithWorkers(t, 4)
	m, _, _, err := s.PutFile(bytes.NewReader(testData(t, 30<<20)))
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, c := range []model.Chunk{m.Chunks[0], m.Chunks[len(m.Chunks)-1]} {
		writeObject(t, s, c.Hash, []byte("garbage"))
		want = append(want, c.Hash)
	}
	slices.Sort(want)
	bad, err := s.VerifyAll()
	if err != nil || !slices.Equal(bad, want) {
		t.Fatalf("bad=%v err=%v, want %v", bad, err, want)
	}
}

func TestCompressBound(t *testing.T) {
	enc := func(b []byte) int {
		out, err := compress(nil, b)
		if err != nil {
			t.Fatal(err)
		}
		return len(out)
	}
	// Incompressible input is the worst case for expansion.
	for _, n := range []int{0, 1, 1000, 128 << 10, maxChunk} {
		if got, bound := enc(testData(t, n)), compressBound(n); got > bound {
			t.Errorf("compressed %d bytes to %d, above bound %d", n, got, bound)
		}
	}
}

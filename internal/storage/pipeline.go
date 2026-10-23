package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"runtime"
	"sort"
	"sync"

	"github.com/joeymeij/datagit/internal/model"
)

// Chunks are independent once cut, so add and restore process several
// at a time: chunking and the whole-file hash stay sequential, while
// per-chunk hashing, compression, decompression, and I/O run on a
// bounded worker pool. Output order is always the input order
// (ADR-0012).
//
// Memory is bounded by construction: each operation owns a fixed set of
// workers+1 chunk buffers (circulated through a free list) plus one
// scratch buffer per worker, each sized to the chunks it has held.

// maxWorkers caps concurrency. Beyond 4, add gains <10% (and the CLI
// none, since CSV schema detection becomes the bottleneck) while memory
// keeps growing (ADR-0012).
const maxWorkers = 4

func defaultWorkers() int { return min(runtime.GOMAXPROCS(0), maxWorkers) }

// freeList returns a channel holding n reusable (initially empty)
// buffers.
func freeList(n int) chan []byte {
	free := make(chan []byte, n)
	for range n {
		free <- nil
	}
	return free
}

type putJob struct {
	idx  int
	data []byte // copy of the chunk, from the free list
}

type putResult struct {
	idx   int
	chunk model.Chunk
	err   error
}

// PutFile streams r into the store, splitting it into content-defined
// chunks. It returns the file manifest, the whole-file SHA-256 (hex),
// and the total size.
func (s *Store) PutFile(r io.Reader) (model.Manifest, string, int64, error) {
	free := freeList(s.workers + 1)
	jobs := make(chan putJob)
	results := make(chan putResult, s.workers)
	stop := make(chan struct{})

	var workers sync.WaitGroup
	for range s.workers {
		workers.Go(func() {
			var scratch []byte
			for j := range jobs {
				hash, err := s.putChunk(j.data, &scratch)
				results <- putResult{idx: j.idx, chunk: model.Chunk{Hash: hash, Size: int64(len(j.data))}, err: err}
				free <- j.data
			}
		})
	}

	// The producer's writes to these are visible to the collector
	// below: closing jobs, the workers finishing, and closing results
	// happen in that order before the collector's loop ends.
	var (
		readErr  error
		total    int64
		fileHash = sha256.New()
	)
	go func() {
		defer close(jobs)
		chunks := newChunker(r)
		for idx := 0; ; idx++ {
			chunk, err := chunks.next()
			if err == io.EOF {
				return
			}
			if err != nil {
				readErr = err
				return
			}
			fileHash.Write(chunk)
			total += int64(len(chunk))
			var buf []byte
			select {
			case buf = <-free:
			case <-stop:
				return
			}
			// The chunker reuses its buffer, so workers get a copy.
			buf = append(ensureCap(buf, len(chunk)), chunk...)
			select {
			case jobs <- putJob{idx: idx, data: buf}:
			case <-stop:
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	var (
		manifest model.Manifest
		firstErr error
	)
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			close(stop)
		}
		for len(manifest.Chunks) <= res.idx {
			manifest.Chunks = append(manifest.Chunks, model.Chunk{})
		}
		manifest.Chunks[res.idx] = res.chunk
	}
	if firstErr == nil {
		firstErr = readErr
	}
	if firstErr != nil {
		return model.Manifest{}, "", 0, firstErr
	}
	return manifest, hex.EncodeToString(fileHash.Sum(nil)), total, nil
}

type restoreJob struct {
	hash string
	out  chan loaded
}

type loaded struct {
	data []byte // the chunk, or on error the buffer to give back
	err  error
	held bool // data occupies a free-list slot that must be returned
}

// RestoreFile writes every chunk of manifest, in order, to w. Chunks are
// loaded and verified concurrently, up to s.workers ahead of the writer.
// Nothing after a failed chunk is written.
func (s *Store) RestoreFile(manifest model.Manifest, w io.Writer) error {
	// At most workers results are pending and one is being written, so
	// workers+1 buffers always suffice and no worker waits forever.
	free := freeList(s.workers + 1)
	jobs := make(chan restoreJob)
	pending := make(chan chan loaded, s.workers)
	stop := make(chan struct{})

	var workers sync.WaitGroup
	for range s.workers {
		workers.Go(func() {
			var scratch []byte
			for j := range jobs {
				buf := <-free
				data, err := s.loadChunk(j.hash, buf, &scratch)
				if err != nil {
					data = buf
				}
				j.out <- loaded{data: data, err: err, held: true}
			}
		})
	}
	go func() {
		defer close(pending)
		defer close(jobs)
		for _, c := range manifest.Chunks {
			out := make(chan loaded, 1)
			select {
			case pending <- out:
			case <-stop:
				return
			}
			select {
			case jobs <- restoreJob{hash: c.Hash, out: out}:
			case <-stop:
				// Never reached a worker, so it holds no buffer.
				out <- loaded{err: errStopped}
				return
			}
		}
	}()

	var firstErr error
	for out := range pending {
		r := <-out
		if firstErr == nil {
			if r.err != nil {
				firstErr = r.err
			} else if _, err := w.Write(r.data); err != nil {
				firstErr = fmt.Errorf("writing restored data: %w", err)
			}
			if firstErr != nil {
				close(stop)
			}
		}
		if r.held {
			free <- r.data
		}
	}
	workers.Wait()
	return firstErr
}

// errStopped marks a restore job abandoned after an earlier failure.
var errStopped = fmt.Errorf("restore stopped")

// VerifyAll re-hashes every object in the store and returns the hashes
// of corrupt or unreadable objects, sorted. A nil error with an empty
// slice means the store is healthy. Objects are verified concurrently.
func (s *Store) VerifyAll() ([]string, error) {
	hashes, err := s.Objects()
	if err != nil {
		return nil, err
	}
	jobs := make(chan string)
	var (
		mu      sync.Mutex
		bad     []string
		workers sync.WaitGroup
	)
	for range s.workers {
		workers.Go(func() {
			var dst, scratch []byte
			for hash := range jobs {
				data, err := s.loadChunk(hash, dst, &scratch)
				if err != nil {
					mu.Lock()
					bad = append(bad, hash)
					mu.Unlock()
					continue
				}
				dst = data
			}
		})
	}
	for _, h := range hashes {
		jobs <- h
	}
	close(jobs)
	workers.Wait()
	sort.Strings(bad)
	return bad, nil
}

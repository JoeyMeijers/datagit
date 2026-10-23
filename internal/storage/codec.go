package storage

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// Chunks are compressed and decompressed whole, in memory (they are at
// most maxChunk bytes), through one shared encoder and decoder that each
// run single-threaded per call; parallelism comes from processing
// several chunks at once (pipeline.go). A streaming zstd encoder per
// chunk spawned helper goroutines and issued many small writes
// (ADR-0012).

// maxStoredObject caps how many compressed bytes are read for one
// object. zstd never expands input by more than a small fraction, so a
// larger object is corrupt or hostile.
const maxStoredObject = 2 * maxChunk

// codec returns the process-wide encoder and decoder. EncodeAll and
// DecodeAll are safe for concurrent use. The decoder refuses output
// larger than maxChunk, so a small hostile object fetched from a remote
// cannot expand into gigabytes of memory.
//
// The encoder keeps one history buffer per concurrent call, sized by
// the window. A window of avgChunk cuts that memory by ~40% versus the
// 8 MiB default with no measurable loss of ratio: matches further back
// than an average chunk are rare within a single chunk (ADR-0012).
var codec = sync.OnceValues(func() (*codecPair, error) {
	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderConcurrency(defaultWorkers()), zstd.WithWindowSize(avgChunk))
	if err != nil {
		return nil, fmt.Errorf("initializing zstd encoder: %w", err)
	}
	dec, err := zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(defaultWorkers()), zstd.WithDecoderMaxMemory(maxChunk))
	if err != nil {
		return nil, fmt.Errorf("initializing zstd decoder: %w", err)
	}
	return &codecPair{enc: enc, dec: dec}, nil
})

type codecPair struct {
	enc *zstd.Encoder
	dec *zstd.Decoder
}

// compress appends chunk, zstd-compressed, to dst[:0]. dst is grown
// once to the worst-case compressed size, so a reused buffer never
// reallocates mid-encode.
func compress(dst, chunk []byte) ([]byte, error) {
	c, err := codec()
	if err != nil {
		return nil, err
	}
	return c.enc.EncodeAll(chunk, ensureCap(dst, compressBound(len(chunk)))), nil
}

// compressBound is an upper bound on the compressed size of n bytes
// (zstd's ZSTD_COMPRESSBOUND).
func compressBound(n int) int {
	bound := n + n>>8
	if n < 128<<10 {
		bound += (128<<10 - n) >> 11
	}
	return bound
}

// ensureCap returns buf[:0] with capacity at least n, growing with 25%
// headroom so buffers that see slowly growing sizes settle quickly
// without doubling.
func ensureCap(buf []byte, n int) []byte {
	if cap(buf) >= n {
		return buf[:0]
	}
	return make([]byte, 0, n+n/4)
}

// decompress appends the decoding of src to dst[:0].
func decompress(dst, src []byte) ([]byte, error) {
	c, err := codec()
	if err != nil {
		return nil, err
	}
	return c.dec.DecodeAll(src, dst[:0])
}

// readObject appends f's contents to dst[:0], refusing objects larger
// than maxStoredObject.
func readObject(dst []byte, f *os.File) ([]byte, error) {
	w := appendWriter{dst[:0]}
	n, err := io.Copy(&w, io.LimitReader(f, maxStoredObject+1))
	if err != nil {
		return nil, err
	}
	if n > maxStoredObject {
		return nil, fmt.Errorf("object exceeds %d bytes", maxStoredObject)
	}
	return w.b, nil
}

type appendWriter struct{ b []byte }

func (w *appendWriter) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}

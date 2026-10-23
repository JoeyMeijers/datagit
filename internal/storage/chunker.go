package storage

import (
	"fmt"
	"io"
)

// Content-defined chunking (FastCDC, Xia et al. 2016, with normalized
// chunking). Boundaries depend on the bytes themselves, not on offsets,
// so inserting or deleting data only changes the chunks around the
// edit; every chunk after it realigns and deduplicates. See ADR-0011.
//
// Changing any of these constants or the gear table changes where
// boundaries fall. Existing data stays readable (manifests list their
// chunks explicitly), but new versions stop deduplicating against it,
// so treat them as frozen.
const (
	minChunk = 512 << 10 // no boundary before this many bytes
	avgChunk = 2 << 20   // target size; switches from strict to loose mask
	maxChunk = 8 << 20   // forced boundary; also bounds memory per add

	// Normalized chunking: a mask with two more bits than log2(avgChunk)
	// makes cuts rarer before the target size and one with two fewer
	// makes them likelier after it, narrowing the size distribution.
	// Masks take the high bits, which depend on the last 64 input
	// bytes (the gear hash shifts one bit per byte).
	maskStrict = uint64(1<<23-1) << (64 - 23)
	maskLoose  = uint64(1<<19-1) << (64 - 19)
)

// gear maps each byte value to a fixed pseudo-random 64-bit value.
var gear = gearTable()

// gearTable derives the table from SplitMix64 with a fixed seed, so it
// is reproducible without embedding 256 literals.
func gearTable() [256]uint64 {
	var t [256]uint64
	x := uint64(0x6461746167697421) // "datagit!"
	for i := range t {
		x += 0x9e3779b97f4a7c15
		z := x
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		t[i] = z ^ (z >> 31)
	}
	return t
}

// cutPoint returns the length of the first chunk of data. Callers must
// pass at least maxChunk bytes unless data is the end of the stream,
// otherwise a boundary could be forced at the end of a partial read.
func cutPoint(data []byte) int {
	n := len(data)
	if n <= minChunk {
		return n
	}
	if n > maxChunk {
		n = maxChunk
	}
	normal := min(avgChunk, n)
	var fp uint64
	i := minChunk
	for ; i < normal; i++ {
		fp = (fp << 1) + gear[data[i]]
		if fp&maskStrict == 0 {
			return i + 1
		}
	}
	for ; i < n; i++ {
		fp = (fp << 1) + gear[data[i]]
		if fp&maskLoose == 0 {
			return i + 1
		}
	}
	return n
}

// chunker streams content-defined chunks from r using a fixed buffer of
// 2×maxChunk, so memory is bounded regardless of input size.
type chunker struct {
	r          io.Reader
	buf        []byte
	start, end int // unconsumed data is buf[start:end]
	eof        bool
}

func newChunker(r io.Reader) *chunker {
	return &chunker{r: r, buf: make([]byte, 2*maxChunk)}
}

// next returns the next chunk, or io.EOF after the last one. The slice
// aliases the internal buffer and is only valid until the next call.
func (c *chunker) next() ([]byte, error) {
	if err := c.fill(); err != nil {
		return nil, err
	}
	if c.start == c.end {
		return nil, io.EOF
	}
	n := cutPoint(c.buf[c.start:c.end])
	chunk := c.buf[c.start : c.start+n]
	c.start += n
	return chunk, nil
}

// fill ensures at least maxChunk unconsumed bytes are buffered, or that
// the whole remaining input is (at end of stream).
func (c *chunker) fill() error {
	if c.eof || c.end-c.start >= maxChunk {
		return nil
	}
	// Compact only when the tail cannot hold a full chunk; with a
	// 2×maxChunk buffer that is at most once per maxChunk consumed.
	if len(c.buf)-c.start < maxChunk {
		c.end = copy(c.buf, c.buf[c.start:c.end])
		c.start = 0
	}
	n, err := io.ReadAtLeast(c.r, c.buf[c.end:], maxChunk-(c.end-c.start))
	c.end += n
	switch err {
	case nil:
		return nil
	case io.EOF, io.ErrUnexpectedEOF:
		c.eof = true
		return nil
	default:
		return fmt.Errorf("reading input: %w", err)
	}
}

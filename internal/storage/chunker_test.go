package storage

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"
	"testing/iotest"
)

// chunkSizes runs the streaming chunker over r and returns chunk lengths.
func chunkSizes(t *testing.T, r io.Reader) []int {
	t.Helper()
	var sizes []int
	c := newChunker(r)
	for {
		chunk, err := c.next()
		if err == io.EOF {
			return sizes
		}
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(chunk))
	}
}

// chunkSet returns the distinct chunk contents of data.
func chunkSet(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	off := 0
	for _, n := range chunkSizes(t, bytes.NewReader(data)) {
		set[string(data[off:off+n])] = true
		off += n
	}
	return set
}

// newBytes counts the bytes of updated that are not already stored as
// chunks of original: what an add of the new version would write.
func newBytes(t *testing.T, original, updated []byte) int {
	t.Helper()
	have := chunkSet(t, original)
	stored, off := 0, 0
	for _, n := range chunkSizes(t, bytes.NewReader(updated)) {
		if !have[string(updated[off:off+n])] {
			stored += n
		}
		off += n
	}
	return stored
}

func checkBounds(t *testing.T, sizes []int, total int) {
	t.Helper()
	sum := 0
	for i, n := range sizes {
		sum += n
		last := i == len(sizes)-1
		if n > maxChunk || n == 0 || (!last && n < minChunk) {
			t.Fatalf("chunk %d has size %d outside [%d, %d]", i, n, minChunk, maxChunk)
		}
	}
	if sum != total {
		t.Fatalf("chunks cover %d bytes, want %d", sum, total)
	}
}

func TestChunkerBoundsAndAverage(t *testing.T) {
	for name, data := range map[string][]byte{
		"random": testData(t, 64<<20),
		"csv":    csvData(64 << 20),
	} {
		t.Run(name, func(t *testing.T) {
			sizes := chunkSizes(t, bytes.NewReader(data))
			checkBounds(t, sizes, len(data))
			avg := len(data) / len(sizes)
			if avg < avgChunk/2 || avg > 2*avgChunk {
				t.Fatalf("average chunk %d bytes, want near %d", avg, avgChunk)
			}
		})
	}
}

func TestChunkerEdgeSizes(t *testing.T) {
	for _, n := range []int{0, 1, minChunk - 1, minChunk, minChunk + 1, maxChunk - 1, maxChunk, maxChunk + 1, 2 * maxChunk, 3*maxChunk + 7} {
		data := testData(t, n)
		sizes := chunkSizes(t, bytes.NewReader(data))
		checkBounds(t, sizes, n)
		if n > 0 && n <= minChunk && len(sizes) != 1 {
			t.Errorf("%d bytes: %d chunks, want 1", n, len(sizes))
		}
	}
}

func TestChunkerUniformInputHitsMaximum(t *testing.T) {
	// No content variation means no natural boundary: chunks fall back
	// to the forced maximum and identical runs deduplicate.
	data := make([]byte, 3*maxChunk+5)
	sizes := chunkSizes(t, bytes.NewReader(data))
	checkBounds(t, sizes, len(data))
	if !slices.Equal(sizes, []int{maxChunk, maxChunk, maxChunk, 5}) {
		t.Fatalf("sizes = %v", sizes)
	}
}

func TestChunkerIndependentOfReadSizes(t *testing.T) {
	data := testData(t, 12<<20)
	want := chunkSizes(t, bytes.NewReader(data))
	for name, r := range map[string]io.Reader{
		"one byte": iotest.OneByteReader(bytes.NewReader(data)),
		"half":     iotest.HalfReader(bytes.NewReader(data)),
		"data+EOF": iotest.DataErrReader(bytes.NewReader(data)),
	} {
		if got := chunkSizes(t, r); !slices.Equal(got, want) {
			t.Errorf("%s reader: sizes %v, want %v", name, got, want)
		}
	}
}

func TestChunkerGoldenBoundaries(t *testing.T) {
	// Boundaries are part of the storage format in practice: if these
	// change, new versions stop deduplicating against existing data.
	// Do not update these values without a new ADR.
	if gear[0] != 0x81f2d7fbdf28fca8 || gear[255] != 0xeb5e00140b95cf04 {
		t.Fatalf("gear table changed: gear[0]=%#x gear[255]=%#x", gear[0], gear[255])
	}
	golden := map[string]struct {
		data []byte
		want []int
	}{
		"random": {testData(t, 16<<20), []int{2268120, 2423849, 2281377, 1503013, 2209740, 2918635, 2112438, 1060044}},
		"csv":    {csvData(16 << 20), []int{719208, 2176488, 2410891, 2731493, 3112099, 2510507, 2739050, 377498}},
	}
	for name, g := range golden {
		if got := chunkSizes(t, bytes.NewReader(g.data)); !slices.Equal(got, g.want) {
			t.Errorf("%s: boundaries %v, want %v", name, got, g.want)
		}
	}
}

func TestChunkerLocalizesEdits(t *testing.T) {
	data := csvData(32 << 20)
	header := bytes.IndexByte(data, '\n') + 1
	row := []byte("999999999,region-xx,12345.67,2026-01-01\n")
	mid := len(data) / 2
	mid += bytes.IndexByte(data[mid:], '\n') + 1
	next := mid + bytes.IndexByte(data[mid:], '\n') + 1

	edits := map[string][]byte{
		"insert row at top": slices.Concat(data[:header], row, data[header:]),
		"delete row mid":    slices.Concat(data[:mid], data[next:]),
		"append row":        slices.Concat(data, row),
		"prepend bytes":     slices.Concat([]byte("x"), data),
	}
	// An edit may rewrite the chunks it touches plus a neighbour whose
	// boundary moved: allow two maximum-size chunks. Fixed-size chunking
	// rewrites everything after an insert or delete.
	limit := 2 * maxChunk
	for name, updated := range edits {
		if n := newBytes(t, data, updated); n > limit {
			t.Errorf("%s: %d new bytes of %d, want <= %d", name, n, len(updated), limit)
		}
	}
}

func TestChunkerPropagatesReadError(t *testing.T) {
	boom := errors.New("boom")
	r := io.MultiReader(bytes.NewReader(testData(t, maxChunk+10)), iotest.ErrReader(boom))
	c := newChunker(r)
	var err error
	for err == nil {
		_, err = c.next()
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped boom", err)
	}
}

func BenchmarkCutPoint(b *testing.B) {
	data := testData(b, 64<<20)
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		for rest := data; len(rest) > 0; {
			rest = rest[cutPoint(rest):]
		}
	}
}

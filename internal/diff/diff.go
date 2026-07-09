// Package diff compares two commits: which files were added, removed,
// or modified; how schemas changed (from stored metadata); and, for
// formats with row readers, how many rows were added and removed
// (computed as a streaming hash multiset difference; see ADR-0005).
package diff

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io"

	"github.com/joeymeij/dit/internal/model"
	"github.com/joeymeij/dit/internal/tabular"
)

// TypeChange records a column whose detected type changed.
type TypeChange struct {
	Column   string
	Old, New string
}

// FileChange describes how one path differs between two commits.
type FileChange struct {
	Path   string
	Status string // added | removed | modified

	Old, New *model.FileEntry // nil when absent on that side

	// Schema changes (modified files with schemas on both sides).
	ColsAdded   []model.Column
	ColsRemoved []string
	TypeChanges []TypeChange

	// Row changes (modified files whose format supports row reading).
	// RowDiff is false when rows could not be compared (e.g. parquet);
	// RowsAdded/RowsRemoved are then meaningless.
	RowDiff                bool
	RowsAdded, RowsRemoved int64

	// Row content samples, populated only when Options.MaxSampleRows > 0.
	SampleAdded, SampleRemoved [][]string
}

// OpenFunc streams the content of a committed file entry.
type OpenFunc func(model.FileEntry) (io.ReadCloser, error)

// Options tunes a diff run.
type Options struct {
	// MaxSampleRows caps how many added/removed rows are captured per
	// file (each side). 0 disables row capture (counts only).
	MaxSampleRows int
}

// Commits diffs commit a against commit b (a = old, b = new). open is
// used to read file contents for row-level comparison; formats without
// row readers degrade to metadata-only diffs.
func Commits(a, b model.Commit, open OpenFunc, opts Options) ([]FileChange, error) {
	oldFiles := map[string]model.FileEntry{}
	for _, f := range a.Files {
		oldFiles[f.Path] = f
	}
	var changes []FileChange
	seen := map[string]bool{}

	for i := range b.Files {
		nf := b.Files[i]
		seen[nf.Path] = true
		of, existed := oldFiles[nf.Path]
		if !existed {
			changes = append(changes, FileChange{Path: nf.Path, Status: "added", New: &b.Files[i]})
			continue
		}
		if of.SHA256 == nf.SHA256 {
			continue // unchanged
		}
		ch := FileChange{Path: nf.Path, Status: "modified", Old: &of, New: &b.Files[i]}
		diffSchemas(&ch)
		if err := diffRows(&ch, open, opts); err != nil {
			return nil, fmt.Errorf("row diff for %s: %w", nf.Path, err)
		}
		changes = append(changes, ch)
	}
	for i := range a.Files {
		if !seen[a.Files[i].Path] {
			changes = append(changes, FileChange{Path: a.Files[i].Path, Status: "removed", Old: &a.Files[i]})
		}
	}
	return changes, nil
}

// diffSchemas fills column-level changes from stored schema metadata.
func diffSchemas(ch *FileChange) {
	if ch.Old.Schema == nil || ch.New.Schema == nil {
		return
	}
	oldCols := map[string]string{}
	for _, c := range ch.Old.Schema.Columns {
		oldCols[c.Name] = c.Type
	}
	newSeen := map[string]bool{}
	for _, c := range ch.New.Schema.Columns {
		newSeen[c.Name] = true
		oldType, existed := oldCols[c.Name]
		switch {
		case !existed:
			ch.ColsAdded = append(ch.ColsAdded, c)
		case oldType != c.Type:
			ch.TypeChanges = append(ch.TypeChanges, TypeChange{Column: c.Name, Old: oldType, New: c.Type})
		}
	}
	for _, c := range ch.Old.Schema.Columns {
		if !newSeen[c.Name] {
			ch.ColsRemoved = append(ch.ColsRemoved, c.Name)
		}
	}
}

// diffRows computes added/removed row counts as a multiset difference
// of row hashes. Old rows are read into a count map; new rows decrement
// it. Rows are unordered data, so reordering is not a change. When
// opts.MaxSampleRows > 0, up to that many added rows are captured
// during the new-side pass, and removed rows via one extra pass over
// the old side.
func diffRows(ch *FileChange, open OpenFunc, opts Options) error {
	oldReader, err := openTabular(*ch.Old, open)
	if errors.Is(err, tabular.ErrUnsupported) {
		return nil // metadata-only diff for this format
	}
	if err != nil {
		return err
	}
	defer oldReader.Close()

	newReader, err := openTabular(*ch.New, open)
	if err != nil {
		return err
	}
	defer newReader.Close()

	counts := make(map[[16]byte]int64)
	if err := eachRowHash(oldReader, func(k [16]byte, rec []string) {
		counts[k]++
	}); err != nil {
		return err
	}
	var added int64
	if err := eachRowHash(newReader, func(k [16]byte, rec []string) {
		if counts[k] > 0 {
			counts[k]--
			return
		}
		added++
		if len(ch.SampleAdded) < opts.MaxSampleRows {
			ch.SampleAdded = append(ch.SampleAdded, cloneRow(rec))
		}
	}); err != nil {
		return err
	}
	var removed int64
	for _, n := range counts {
		removed += n
	}
	ch.RowDiff = true
	ch.RowsAdded, ch.RowsRemoved = added, removed

	if opts.MaxSampleRows > 0 && removed > 0 {
		if err := collectRemoved(ch, open, counts, opts.MaxSampleRows); err != nil {
			return err
		}
	}
	return nil
}

// collectRemoved re-streams the old side to recover the content of
// removed rows (their hashes are the leftover counts).
func collectRemoved(ch *FileChange, open OpenFunc, leftover map[[16]byte]int64, max int) error {
	r, err := openTabular(*ch.Old, open)
	if err != nil {
		return err
	}
	defer r.Close()
	return eachRowHash(r, func(k [16]byte, rec []string) {
		if len(ch.SampleRemoved) >= max || leftover[k] <= 0 {
			return
		}
		leftover[k]--
		ch.SampleRemoved = append(ch.SampleRemoved, cloneRow(rec))
	})
}

func cloneRow(rec []string) []string {
	out := make([]string, len(rec))
	copy(out, rec)
	return out
}

func openTabular(e model.FileEntry, open OpenFunc) (tabular.Reader, error) {
	src, err := open(e)
	if err != nil {
		return nil, err
	}
	r, err := tabular.ForFormat(e.Format, src)
	if err != nil {
		src.Close()
		return nil, err
	}
	return r, nil
}

// eachRowHash streams rows, calling fn with each row's 128-bit FNV-1a
// hash (fields joined with a unit separator) and the row itself. The
// row slice is only valid for the duration of the call.
func eachRowHash(r tabular.Reader, fn func([16]byte, []string)) error {
	h := fnv.New128a()
	for {
		rec, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		h.Reset()
		for i, v := range rec {
			if i > 0 {
				h.Write([]byte{0x1f})
			}
			h.Write([]byte(v))
		}
		var key [16]byte
		h.Sum(key[:0])
		fn(key, rec)
	}
}

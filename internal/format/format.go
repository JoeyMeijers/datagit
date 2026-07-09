// Package format detects dataset formats and extracts lightweight
// metadata (schema, row count, column count) at `dit add` time.
//
// Detectors are registered in a package-level registry so new formats
// (Parquet, Arrow, JSON Lines, ...) can be added — or provided by
// plugins later — without touching callers.
package format

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joeymeij/dit/internal/model"
)

// Info is the metadata a detector extracts from a dataset file.
type Info struct {
	Format string // csv | tsv | parquet | binary
	Rows   int64
	Cols   int
	Schema *model.Schema
}

// Detector extracts Info from a file on disk. Implementations must
// stream and never load the whole file into memory.
type Detector interface {
	// Detect reports whether this detector handles the file at path.
	Detect(path string) bool
	// Extract reads metadata from the file at path.
	Extract(path string) (Info, error)
}

var registry []Detector

// Register adds a detector to the registry. Later registrations take
// precedence over earlier ones.
func Register(d Detector) { registry = append([]Detector{d}, registry...) }

// Extract runs the first matching detector against path. Files no
// detector recognizes are classified as opaque "binary" with no schema.
func Extract(path string) (Info, error) {
	for _, d := range registry {
		if d.Detect(path) {
			info, err := d.Extract(path)
			if err != nil {
				return Info{}, fmt.Errorf("extracting %s metadata from %s: %w", info.Format, filepath.Base(path), err)
			}
			return info, nil
		}
	}
	return Info{Format: "binary"}, nil
}

// ext returns the lower-cased file extension without the dot.
func ext(path string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
}

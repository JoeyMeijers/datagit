// Package tabular provides a streaming row-oriented reader abstraction
// over dataset files. It is the shared foundation for data-aware
// features (profile, diff); adding a new readable format means adding
// one implementation here.
//
// Values are surfaced as strings (CSV-native); consumers parse as
// needed. Readers stream and never buffer whole files.
package tabular

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsupported is returned when no row reader exists for a format.
// Callers should degrade gracefully (e.g. metadata-only diff).
var ErrUnsupported = errors.New("format does not support row reading")

// Reader streams rows from a tabular dataset.
type Reader interface {
	// Columns returns the header names.
	Columns() []string
	// Next returns the next data row, or io.EOF after the last row.
	// The returned slice is only valid until the next call.
	Next() ([]string, error)
	// Close releases the underlying source.
	Close() error
}

// Open returns a Reader for the file at path, chosen by extension.
func Open(path string) (Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r, err := ForFormat(FormatOf(path), f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

// FormatOf maps a file path to a datagit format name.
func FormatOf(path string) string {
	switch strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".") {
	case "csv":
		return "csv"
	case "tsv":
		return "tsv"
	case "parquet":
		return "parquet"
	default:
		return "binary"
	}
}

// ForFormat returns a Reader for a named format over src. src is closed
// by the Reader's Close (or immediately on error).
func ForFormat(format string, src io.ReadCloser) (Reader, error) {
	switch format {
	case "csv":
		return newCSVReader(src, ',')
	case "tsv":
		return newCSVReader(src, '\t')
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, format)
	}
}

// csvReader streams CSV/TSV rows. The first record is the header.
type csvReader struct {
	src     io.ReadCloser
	r       *csv.Reader
	columns []string
}

func newCSVReader(src io.ReadCloser, comma rune) (*csvReader, error) {
	r := csv.NewReader(src)
	r.Comma = comma
	r.FieldsPerRecord = -1 // tolerate ragged rows
	r.ReuseRecord = true

	header, err := r.Read()
	if err == io.EOF {
		return &csvReader{src: src, r: r}, nil // empty file: no columns
	}
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	cols := make([]string, len(header))
	copy(cols, header)
	return &csvReader{src: src, r: r, columns: cols}, nil
}

func (c *csvReader) Columns() []string { return c.columns }

func (c *csvReader) Next() ([]string, error) {
	rec, err := c.r.Read()
	if err != nil {
		return nil, err // io.EOF passes through untouched
	}
	return rec, nil
}

func (c *csvReader) Close() error { return c.src.Close() }

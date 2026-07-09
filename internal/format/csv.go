package format

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joeymeij/datagit/internal/model"
)

// typeSampleRows caps how many rows are examined for type inference.
// Row counting always scans the entire file (streaming).
const typeSampleRows = 1000

// CSVDetector handles .csv and .tsv files.
type CSVDetector struct{}

func init() { Register(CSVDetector{}) }

// Detect matches on file extension.
func (CSVDetector) Detect(path string) bool {
	e := ext(path)
	return e == "csv" || e == "tsv"
}

// Extract streams the file once, inferring column types from a sample of
// rows and counting all data rows. The first record is treated as the
// header, matching common statistical-data conventions.
func (CSVDetector) Extract(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()

	name := "csv"
	r := csv.NewReader(f)
	if ext(path) == "tsv" {
		r.Comma = '\t'
		name = "tsv"
	}
	r.FieldsPerRecord = -1 // tolerate ragged rows; profiling flags them later
	r.ReuseRecord = true

	header, err := r.Read()
	if err == io.EOF {
		return Info{Format: name, Schema: &model.Schema{}}, nil
	}
	if err != nil {
		return Info{}, fmt.Errorf("reading header: %w", err)
	}
	cols := make([]string, len(header))
	copy(cols, header)

	inf := newInferrer(len(cols))
	var rows int64
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Info{}, fmt.Errorf("row %d: %w", rows+2, err)
		}
		if rows < typeSampleRows {
			inf.observe(rec)
		}
		rows++
	}

	sch := &model.Schema{Columns: make([]model.Column, len(cols))}
	for i, c := range cols {
		sch.Columns[i] = model.Column{Name: c, Type: inf.typeOf(i)}
	}
	return Info{Format: name, Rows: rows, Cols: len(cols), Schema: sch}, nil
}

// inferrer tracks which types remain possible for each column. A column's
// type is the most specific candidate every non-empty observed value
// satisfies, falling back to "string".
type inferrer struct {
	possible []map[string]bool // per column: candidate type -> still possible
	seen     []bool            // per column: any non-empty value observed
}

var candidateTypes = []string{"integer", "float", "boolean", "date", "datetime"}

// typePriority orders candidates from most to least specific.
var typePriority = []string{"boolean", "integer", "float", "date", "datetime"}

func newInferrer(cols int) *inferrer {
	inf := &inferrer{
		possible: make([]map[string]bool, cols),
		seen:     make([]bool, cols),
	}
	for i := range inf.possible {
		m := make(map[string]bool, len(candidateTypes))
		for _, t := range candidateTypes {
			m[t] = true
		}
		inf.possible[i] = m
	}
	return inf
}

func (inf *inferrer) observe(rec []string) {
	for i, v := range rec {
		if i >= len(inf.possible) {
			break // ragged row wider than header
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue // missing values do not constrain the type
		}
		inf.seen[i] = true
		p := inf.possible[i]
		if p["integer"] && !isInt(v) {
			p["integer"] = false
		}
		if p["float"] && !isFloat(v) {
			p["float"] = false
		}
		if p["boolean"] && !isBool(v) {
			p["boolean"] = false
		}
		if p["date"] && !isDate(v) {
			p["date"] = false
		}
		if p["datetime"] && !isDateTime(v) {
			p["datetime"] = false
		}
	}
}

func (inf *inferrer) typeOf(col int) string {
	if !inf.seen[col] {
		return "string" // empty column: nothing to infer
	}
	for _, t := range typePriority {
		if inf.possible[col][t] {
			return t
		}
	}
	return "string"
}

func isInt(v string) bool {
	_, err := strconv.ParseInt(v, 10, 64)
	return err == nil
}

func isFloat(v string) bool {
	_, err := strconv.ParseFloat(v, 64)
	return err == nil
}

func isBool(v string) bool {
	switch strings.ToLower(v) {
	case "true", "false":
		return true
	}
	return false
}

var dateLayouts = []string{"2006-01-02", "2006/01/02", "02-01-2006", "01/02/2006"}

func isDate(v string) bool {
	for _, l := range dateLayouts {
		if _, err := time.Parse(l, v); err == nil {
			return true
		}
	}
	return false
}

var dateTimeLayouts = []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"}

func isDateTime(v string) bool {
	for _, l := range dateTimeLayouts {
		if _, err := time.Parse(l, v); err == nil {
			return true
		}
	}
	return false
}

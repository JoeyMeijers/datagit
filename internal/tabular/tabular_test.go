package tabular

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func readAll(t *testing.T, r Reader) [][]string {
	t.Helper()
	var rows [][]string
	for {
		rec, err := r.Next()
		if err == io.EOF {
			return rows
		}
		if err != nil {
			t.Fatal(err)
		}
		row := make([]string, len(rec))
		copy(row, rec)
		rows = append(rows, row)
	}
}

func TestOpenCSV(t *testing.T) {
	p := writeFile(t, "d.csv", "a,b\n1,x\n2,y\n")
	r, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if got := r.Columns(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("columns = %v", got)
	}
	rows := readAll(t, r)
	if len(rows) != 2 || rows[0][1] != "x" || rows[1][0] != "2" {
		t.Fatalf("rows = %v", rows)
	}
}

func TestOpenTSV(t *testing.T) {
	p := writeFile(t, "d.tsv", "a\tb\n1\tx\n")
	r, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	rows := readAll(t, r)
	if len(rows) != 1 || rows[0][1] != "x" {
		t.Fatalf("rows = %v", rows)
	}
}

func TestOpenEmptyFile(t *testing.T) {
	p := writeFile(t, "d.csv", "")
	r, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if len(r.Columns()) != 0 {
		t.Fatalf("columns = %v, want none", r.Columns())
	}
	if rows := readAll(t, r); len(rows) != 0 {
		t.Fatalf("rows = %v, want none", rows)
	}
}

func TestOpenUnsupported(t *testing.T) {
	p := writeFile(t, "d.parquet", "PAR1")
	_, err := Open(p)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestOpenMissingFile(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "nope.csv")); err == nil {
		t.Fatal("expected error")
	}
}

func TestRaggedRowsPassThrough(t *testing.T) {
	p := writeFile(t, "d.csv", "a,b\n1\n2,3,4\n")
	r, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	rows := readAll(t, r)
	if len(rows) != 2 || len(rows[0]) != 1 || len(rows[1]) != 3 {
		t.Fatalf("rows = %v", rows)
	}
}

func TestFormatOf(t *testing.T) {
	for path, want := range map[string]string{
		"a.csv": "csv", "A.TSV": "tsv", "x.parquet": "parquet", "x.bin": "binary", "noext": "binary",
	} {
		if got := FormatOf(path); got != want {
			t.Errorf("FormatOf(%q) = %s, want %s", path, got, want)
		}
	}
}

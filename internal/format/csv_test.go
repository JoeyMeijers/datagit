package format

import (
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

func TestCSVDetect(t *testing.T) {
	d := CSVDetector{}
	for path, want := range map[string]bool{
		"data.csv": true, "DATA.CSV": true, "data.tsv": true,
		"data.parquet": false, "data.txt": false, "csv": false,
	} {
		if got := d.Detect(path); got != want {
			t.Errorf("Detect(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestCSVExtractTypes(t *testing.T) {
	p := writeFile(t, "t.csv",
		"id,name,score,active,born,seen\n"+
			"1,alice,3.5,true,1990-01-02,2024-01-02T10:00:00Z\n"+
			"2,bob,4,false,1985-12-31,2024-06-01 09:30:00\n")
	info, err := Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != "csv" || info.Rows != 2 || info.Cols != 6 {
		t.Fatalf("info = %+v", info)
	}
	want := []string{"integer", "string", "float", "boolean", "date", "datetime"}
	for i, w := range want {
		if got := info.Schema.Columns[i].Type; got != w {
			t.Errorf("col %s: type = %s, want %s", info.Schema.Columns[i].Name, got, w)
		}
	}
}

func TestCSVIntColumnWithFloatBecomesFloat(t *testing.T) {
	p := writeFile(t, "t.csv", "x\n1\n2\n3.5\n")
	info, err := Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Schema.Columns[0].Type; got != "float" {
		t.Fatalf("type = %s, want float", got)
	}
}

func TestCSVMissingValuesIgnoredForTyping(t *testing.T) {
	p := writeFile(t, "t.csv", "x,y\n1,\n,2\n3,\n")
	info, err := Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range info.Schema.Columns {
		if c.Type != "integer" {
			t.Errorf("col %s: type = %s, want integer", c.Name, c.Type)
		}
	}
}

func TestCSVEmptyColumnIsString(t *testing.T) {
	p := writeFile(t, "t.csv", "x,empty\n1,\n2,\n")
	info, err := Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Schema.Columns[1].Type; got != "string" {
		t.Fatalf("empty column type = %s, want string", got)
	}
}

func TestCSVEmptyFile(t *testing.T) {
	p := writeFile(t, "t.csv", "")
	info, err := Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Rows != 0 || info.Cols != 0 {
		t.Fatalf("info = %+v, want empty", info)
	}
}

func TestCSVHeaderOnly(t *testing.T) {
	p := writeFile(t, "t.csv", "a,b,c\n")
	info, err := Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Rows != 0 || info.Cols != 3 {
		t.Fatalf("rows=%d cols=%d, want 0/3", info.Rows, info.Cols)
	}
}

func TestCSVRaggedRowsTolerated(t *testing.T) {
	p := writeFile(t, "t.csv", "a,b\n1,2\n3,4,5\n6\n")
	info, err := Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Rows != 3 || info.Cols != 2 {
		t.Fatalf("rows=%d cols=%d, want 3/2", info.Rows, info.Cols)
	}
}

func TestTSV(t *testing.T) {
	p := writeFile(t, "t.tsv", "a\tb\n1\tx\n")
	info, err := Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != "tsv" || info.Cols != 2 || info.Rows != 1 {
		t.Fatalf("info = %+v", info)
	}
	if info.Schema.Columns[0].Type != "integer" || info.Schema.Columns[1].Type != "string" {
		t.Fatalf("schema = %+v", info.Schema)
	}
}

func TestUnknownFormatIsBinary(t *testing.T) {
	p := writeFile(t, "blob.bin", "\x00\x01\x02")
	info, err := Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != "binary" || info.Schema != nil {
		t.Fatalf("info = %+v, want binary/no schema", info)
	}
}

func TestCSVBooleanColumn(t *testing.T) {
	p := writeFile(t, "t.csv", "flag\ntrue\nFALSE\nTrue\n")
	info, err := Extract(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Schema.Columns[0].Type; got != "boolean" {
		t.Fatalf("type = %s, want boolean", got)
	}
}

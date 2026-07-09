package diff

import (
	"io"
	"strings"
	"testing"

	"github.com/joeymeij/rowen/internal/model"
)

// commitOf builds a commit whose file contents are served from contents
// by the test OpenFunc.
func commitOf(files map[string]model.FileEntry) model.Commit {
	c := model.Commit{}
	for _, f := range files {
		c.Files = append(c.Files, f)
	}
	return c
}

func entry(path, format, sha string, schema *model.Schema, rows int64) model.FileEntry {
	return model.FileEntry{Path: path, Format: format, SHA256: sha, Schema: schema, Rows: rows}
}

func schema(cols ...model.Column) *model.Schema { return &model.Schema{Columns: cols} }

// opener serves file content by SHA256 from an in-memory map.
func opener(contents map[string]string) OpenFunc {
	return func(e model.FileEntry) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(contents[e.SHA256])), nil
	}
}

func single(t *testing.T, changes []FileChange, status string) FileChange {
	t.Helper()
	if len(changes) != 1 || changes[0].Status != status {
		t.Fatalf("changes = %+v, want one %q", changes, status)
	}
	return changes[0]
}

func TestAddedAndRemovedFiles(t *testing.T) {
	old := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s1", nil, 1)})
	new_ := commitOf(map[string]model.FileEntry{"b.csv": entry("b.csv", "csv", "s2", nil, 1)})
	changes, err := Commits(old, new_, opener(nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	got := map[string]string{}
	for _, ch := range changes {
		got[ch.Path] = ch.Status
	}
	if got["b.csv"] != "added" || got["a.csv"] != "removed" {
		t.Fatalf("got = %v", got)
	}
}

func TestUnchangedFileSkipped(t *testing.T) {
	e := entry("a.csv", "csv", "same", nil, 1)
	changes, err := Commits(commitOf(map[string]model.FileEntry{"a.csv": e}),
		commitOf(map[string]model.FileEntry{"a.csv": e}), opener(nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %+v, want none", changes)
	}
}

func TestSchemaChanges(t *testing.T) {
	old := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s1",
		schema(model.Column{Name: "id", Type: "integer"}, model.Column{Name: "gone", Type: "string"}), 1)})
	new_ := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s2",
		schema(model.Column{Name: "id", Type: "float"}, model.Column{Name: "fresh", Type: "boolean"}), 1)})

	changes, err := Commits(old, new_, opener(map[string]string{"s1": "id,gone\n1,x\n", "s2": "id,fresh\n1.5,true\n"}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	ch := single(t, changes, "modified")
	if len(ch.ColsAdded) != 1 || ch.ColsAdded[0].Name != "fresh" {
		t.Fatalf("added = %+v", ch.ColsAdded)
	}
	if len(ch.ColsRemoved) != 1 || ch.ColsRemoved[0] != "gone" {
		t.Fatalf("removed = %+v", ch.ColsRemoved)
	}
	if len(ch.TypeChanges) != 1 || ch.TypeChanges[0] != (TypeChange{"id", "integer", "float"}) {
		t.Fatalf("types = %+v", ch.TypeChanges)
	}
}

func TestRowDiffCounts(t *testing.T) {
	contents := map[string]string{
		"s1": "x\n1\n2\n2\n3\n",
		"s2": "x\n2\n3\n3\n4\n5\n",
	}
	old := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s1", nil, 4)})
	new_ := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s2", nil, 5)})
	changes, err := Commits(old, new_, opener(contents), Options{})
	if err != nil {
		t.Fatal(err)
	}
	ch := single(t, changes, "modified")
	if !ch.RowDiff {
		t.Fatal("expected row diff")
	}
	// old {1,2,2,3} → new {2,3,3,4,5}: added {3,4,5}, removed {1,2}.
	if ch.RowsAdded != 3 || ch.RowsRemoved != 2 {
		t.Fatalf("rows +%d -%d, want +3 -2", ch.RowsAdded, ch.RowsRemoved)
	}
}

func TestRowReorderIsNoChange(t *testing.T) {
	contents := map[string]string{"s1": "x\n1\n2\n3\n", "s2": "x\n3\n1\n2\n"}
	old := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s1", nil, 3)})
	new_ := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s2", nil, 3)})
	changes, err := Commits(old, new_, opener(contents), Options{})
	if err != nil {
		t.Fatal(err)
	}
	ch := single(t, changes, "modified")
	if ch.RowsAdded != 0 || ch.RowsRemoved != 0 {
		t.Fatalf("rows +%d -%d, want 0/0", ch.RowsAdded, ch.RowsRemoved)
	}
}

func TestUnsupportedFormatMetadataOnly(t *testing.T) {
	old := commitOf(map[string]model.FileEntry{"a.parquet": entry("a.parquet", "parquet", "s1",
		schema(model.Column{Name: "id", Type: "integer"}), 10)})
	new_ := commitOf(map[string]model.FileEntry{"a.parquet": entry("a.parquet", "parquet", "s2",
		schema(model.Column{Name: "id", Type: "integer"}, model.Column{Name: "v", Type: "float"}), 12)})
	changes, err := Commits(old, new_, opener(nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	ch := single(t, changes, "modified")
	if ch.RowDiff {
		t.Fatal("parquet must not row-diff")
	}
	if len(ch.ColsAdded) != 1 || ch.ColsAdded[0].Name != "v" {
		t.Fatalf("schema diff missing: %+v", ch)
	}
}

func TestRowSamples(t *testing.T) {
	contents := map[string]string{
		"s1": "x,y\n1,a\n2,b\n3,c\n",
		"s2": "x,y\n2,b\n4,d\n5,e\n",
	}
	old := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s1", nil, 3)})
	new_ := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s2", nil, 3)})
	changes, err := Commits(old, new_, opener(contents), Options{MaxSampleRows: 10})
	if err != nil {
		t.Fatal(err)
	}
	ch := single(t, changes, "modified")
	if ch.RowsAdded != 2 || ch.RowsRemoved != 2 {
		t.Fatalf("rows +%d -%d", ch.RowsAdded, ch.RowsRemoved)
	}
	if len(ch.SampleAdded) != 2 || ch.SampleAdded[0][0] != "4" || ch.SampleAdded[1][0] != "5" {
		t.Fatalf("added sample = %v", ch.SampleAdded)
	}
	if len(ch.SampleRemoved) != 2 || ch.SampleRemoved[0][0] != "1" || ch.SampleRemoved[1][0] != "3" {
		t.Fatalf("removed sample = %v", ch.SampleRemoved)
	}
}

func TestRowSamplesCapped(t *testing.T) {
	contents := map[string]string{
		"s1": "x\n1\n2\n3\n4\n5\n",
		"s2": "x\n6\n7\n8\n9\n10\n",
	}
	old := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s1", nil, 5)})
	new_ := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s2", nil, 5)})
	changes, err := Commits(old, new_, opener(contents), Options{MaxSampleRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	ch := single(t, changes, "modified")
	if ch.RowsAdded != 5 || ch.RowsRemoved != 5 {
		t.Fatalf("counts wrong: +%d -%d", ch.RowsAdded, ch.RowsRemoved)
	}
	if len(ch.SampleAdded) != 2 || len(ch.SampleRemoved) != 2 {
		t.Fatalf("samples not capped: %d/%d", len(ch.SampleAdded), len(ch.SampleRemoved))
	}
}

func TestNoSamplesWithoutOption(t *testing.T) {
	contents := map[string]string{"s1": "x\n1\n", "s2": "x\n2\n"}
	old := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s1", nil, 1)})
	new_ := commitOf(map[string]model.FileEntry{"a.csv": entry("a.csv", "csv", "s2", nil, 1)})
	changes, err := Commits(old, new_, opener(contents), Options{})
	if err != nil {
		t.Fatal(err)
	}
	ch := single(t, changes, "modified")
	if ch.SampleAdded != nil || ch.SampleRemoved != nil {
		t.Fatalf("samples = %v/%v, want none", ch.SampleAdded, ch.SampleRemoved)
	}
}

func TestEmptyCommits(t *testing.T) {
	changes, err := Commits(model.Commit{}, model.Commit{}, opener(nil), Options{})
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%v err=%v", changes, err)
	}
}

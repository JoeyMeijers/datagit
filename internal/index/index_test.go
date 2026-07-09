package index

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/joeymeij/rowen/internal/model"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestStageAndStaged(t *testing.T) {
	d := openTestDB(t)

	entries, err := d.Staged()
	if err != nil || len(entries) != 0 {
		t.Fatalf("fresh db: entries=%v err=%v", entries, err)
	}

	e := model.FileEntry{Path: "b.csv", Size: 3, SHA256: "aaa", Format: "csv", Rows: 1, Cols: 2}
	if err := d.Stage(e); err != nil {
		t.Fatal(err)
	}
	if err := d.Stage(model.FileEntry{Path: "a.csv", Size: 1, SHA256: "bbb"}); err != nil {
		t.Fatal(err)
	}

	entries, err = d.Staged()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Path != "a.csv" || entries[1].Path != "b.csv" {
		t.Fatalf("entries = %+v, want sorted a.csv,b.csv", entries)
	}
	if entries[1].Rows != 1 || entries[1].Cols != 2 || entries[1].Format != "csv" {
		t.Fatalf("metadata lost: %+v", entries[1])
	}
}

func TestStageReplacesExisting(t *testing.T) {
	d := openTestDB(t)
	if err := d.Stage(model.FileEntry{Path: "a.csv", SHA256: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Stage(model.FileEntry{Path: "a.csv", SHA256: "v2"}); err != nil {
		t.Fatal(err)
	}
	entries, err := d.Staged()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].SHA256 != "v2" {
		t.Fatalf("entries = %+v, want single v2", entries)
	}
}

func TestClearStaging(t *testing.T) {
	d := openTestDB(t)
	if err := d.Stage(model.FileEntry{Path: "a.csv"}); err != nil {
		t.Fatal(err)
	}
	if err := d.ClearStaging(); err != nil {
		t.Fatal(err)
	}
	entries, err := d.Staged()
	if err != nil || len(entries) != 0 {
		t.Fatalf("after clear: entries=%v err=%v", entries, err)
	}
}

func TestPutCommit(t *testing.T) {
	d := openTestDB(t)
	c := model.Commit{
		ID:        "c1",
		Message:   "first",
		Author:    "tester",
		CreatedAt: time.Now().UTC(),
		Files: []model.FileEntry{
			{Path: "a.csv", SHA256: "aaa", Size: 5, Format: "csv", Rows: 2, Cols: 1},
		},
	}
	if err := d.PutCommit(c); err != nil {
		t.Fatal(err)
	}
	// Idempotent re-index (e.g. rebuild) must not fail.
	if err := d.PutCommit(c); err != nil {
		t.Fatal(err)
	}
}

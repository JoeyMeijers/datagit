package index

import (
	"testing"
	"time"

	"github.com/joeymeij/dit/internal/model"
)

func seedCommit(t *testing.T, d *DB, id string, at time.Time, files ...model.FileEntry) {
	t.Helper()
	if err := d.PutCommit(model.Commit{ID: id, Message: "m", Author: "a", CreatedAt: at, Files: files}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchByPathAndColumn(t *testing.T) {
	d := openTestDB(t)
	now := time.Now().UTC()
	seedCommit(t, d, "c1", now,
		model.FileEntry{Path: "income_survey.csv", Format: "csv",
			Schema: &model.Schema{Columns: []model.Column{{Name: "household", Type: "integer"}}}},
		model.FileEntry{Path: "weather.csv", Format: "csv",
			Schema: &model.Schema{Columns: []model.Column{{Name: "Temp", Type: "float"}}}},
	)

	hits, err := d.Search("survey")
	if err != nil || len(hits) != 1 || hits[0].Path != "income_survey.csv" {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	// Column match, case-insensitive both ways.
	hits, err = d.Search("temp")
	if err != nil || len(hits) != 1 || hits[0].Path != "weather.csv" {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	hits, err = d.Search("HOUSEHOLD")
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	hits, err = d.Search("nothing-here")
	if err != nil || len(hits) != 0 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	if _, err := d.Search(""); err == nil {
		t.Fatal("empty term should fail")
	}
}

func TestSearchNewestCommitWins(t *testing.T) {
	d := openTestDB(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedCommit(t, d, "old", base, model.FileEntry{Path: "d.csv", Rows: 1})
	seedCommit(t, d, "new", base.Add(time.Hour), model.FileEntry{Path: "d.csv", Rows: 9})

	hits, err := d.Search("d.csv")
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	if hits[0].CommitID != "new" || hits[0].Entry.Rows != 9 {
		t.Fatalf("hit = %+v, want newest", hits[0])
	}
}

func TestSearchNoSchema(t *testing.T) {
	d := openTestDB(t)
	seedCommit(t, d, "c1", time.Now().UTC(), model.FileEntry{Path: "blob.bin", Format: "binary"})
	hits, err := d.Search("blob")
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	hits, err = d.Search("household")
	if err != nil || len(hits) != 0 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
}

package index

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/joeymeij/datagit/internal/model"
)

// Benchmarks for the SQLite driver choice (ADR-0010). Commits are sized
// at 100 files, well above typical datagit commits.

func benchDB(b *testing.B) *DB {
	b.Helper()
	db, err := Open(filepath.Join(b.TempDir(), "index.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })
	return db
}

func benchEntry(i int) model.FileEntry {
	return model.FileEntry{
		Path:   fmt.Sprintf("data/file-%04d.csv", i),
		Size:   1 << 20,
		SHA256: fmt.Sprintf("%064x", i),
		Format: "csv",
		Rows:   1000,
		Cols:   3,
		Schema: &model.Schema{Columns: []model.Column{
			{Name: "id", Type: "integer"}, {Name: "income", Type: "float"}, {Name: "region", Type: "string"},
		}},
	}
}

func BenchmarkOpen(b *testing.B) {
	path := filepath.Join(b.TempDir(), "index.db")
	for b.Loop() {
		db, err := Open(path)
		if err != nil {
			b.Fatal(err)
		}
		db.Close()
	}
}

func BenchmarkStage(b *testing.B) {
	db := benchDB(b)
	i := 0
	for b.Loop() {
		if err := db.Stage(benchEntry(i % 100)); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkPutCommit100Files(b *testing.B) {
	db := benchDB(b)
	files := make([]model.FileEntry, 100)
	for i := range files {
		files[i] = benchEntry(i)
	}
	i := 0
	for b.Loop() {
		c := model.Commit{
			ID: fmt.Sprintf("%064x", i), Message: "bench", Author: "bench",
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Files: files,
		}
		if err := db.PutCommit(c); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkSearch(b *testing.B) {
	db := benchDB(b)
	files := make([]model.FileEntry, 100)
	for i := range files {
		files[i] = benchEntry(i)
	}
	for i := range 50 {
		c := model.Commit{
			ID: fmt.Sprintf("%064x", i), Message: "bench", Author: "bench",
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Files: files,
		}
		if err := db.PutCommit(c); err != nil {
			b.Fatal(err)
		}
	}
	for b.Loop() {
		if _, err := db.Search("income"); err != nil {
			b.Fatal(err)
		}
	}
}

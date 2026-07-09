// Package index persists rowen's queryable state in an embedded SQLite
// database (.rowen/index.db): the staging area and a searchable cache
// of commits and their files. Commit JSON files under .rowen/commits/
// remain the durable source of truth; the index can be rebuilt from them.
package index

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3" // registers the "sqlite3" driver

	"github.com/joeymeij/rowen/internal/model"
)

// DB wraps the SQLite index database.
type DB struct {
	sql *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS staging (
	path        TEXT PRIMARY KEY,
	entry_json  TEXT NOT NULL,
	staged_at   TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS commits (
	id          TEXT PRIMARY KEY,
	parent      TEXT,
	message     TEXT NOT NULL,
	author      TEXT NOT NULL,
	created_at  TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS commit_files (
	commit_id   TEXT NOT NULL REFERENCES commits(id),
	path        TEXT NOT NULL,
	sha256      TEXT NOT NULL,
	size        INTEGER NOT NULL,
	format      TEXT,
	rows        INTEGER,
	cols        INTEGER,
	entry_json  TEXT NOT NULL,
	PRIMARY KEY (commit_id, path)
);
CREATE INDEX IF NOT EXISTS idx_commit_files_path ON commit_files(path);
`

// Open opens (creating if necessary) the index database at path.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("opening index db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing index schema: %w", err)
	}
	return &DB{sql: db}, nil
}

// Close releases the database handle.
func (d *DB) Close() error { return d.sql.Close() }

// Stage inserts or replaces a staged file entry.
func (d *DB) Stage(e model.FileEntry) error {
	blob, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encoding staged entry: %w", err)
	}
	_, err = d.sql.Exec(
		`INSERT INTO staging(path, entry_json, staged_at) VALUES(?,?,?)
		 ON CONFLICT(path) DO UPDATE SET entry_json=excluded.entry_json, staged_at=excluded.staged_at`,
		e.Path, string(blob), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("staging %s: %w", e.Path, err)
	}
	return nil
}

// Staged returns all staged entries ordered by path.
func (d *DB) Staged() ([]model.FileEntry, error) {
	rows, err := d.sql.Query(`SELECT entry_json FROM staging ORDER BY path`)
	if err != nil {
		return nil, fmt.Errorf("reading staging area: %w", err)
	}
	defer rows.Close()

	var entries []model.FileEntry
	for rows.Next() {
		var blob string
		if err := rows.Scan(&blob); err != nil {
			return nil, err
		}
		var e model.FileEntry
		if err := json.Unmarshal([]byte(blob), &e); err != nil {
			return nil, fmt.Errorf("decoding staged entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ClearStaging empties the staging area (after a successful commit).
func (d *DB) ClearStaging() error {
	_, err := d.sql.Exec(`DELETE FROM staging`)
	return err
}

// PutCommit records a commit and its files in the index inside one
// transaction, so the index never holds a half-written commit.
func (d *DB) PutCommit(c model.Commit) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after successful Commit

	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO commits(id, parent, message, author, created_at) VALUES(?,?,?,?,?)`,
		c.ID, c.Parent, c.Message, c.Author, c.CreatedAt.UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("indexing commit: %w", err)
	}
	for _, f := range c.Files {
		blob, err := json.Marshal(f)
		if err != nil {
			return fmt.Errorf("encoding file entry: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO commit_files(commit_id, path, sha256, size, format, rows, cols, entry_json)
			 VALUES(?,?,?,?,?,?,?,?)`,
			c.ID, f.Path, f.SHA256, f.Size, f.Format, f.Rows, f.Cols, string(blob),
		); err != nil {
			return fmt.Errorf("indexing file %s: %w", f.Path, err)
		}
	}
	return tx.Commit()
}

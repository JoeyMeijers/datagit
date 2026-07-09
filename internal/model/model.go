// Package model defines the core data structures shared across rowen:
// commits, file entries, chunk manifests, and dataset schemas.
package model

import "time"

// Chunk identifies one content-addressed chunk of a file.
type Chunk struct {
	// Hash is the hex-encoded SHA-256 of the uncompressed chunk bytes.
	Hash string `json:"hash"`
	// Size is the uncompressed size in bytes.
	Size int64 `json:"size"`
}

// Manifest describes how a file is assembled from chunks, in order.
type Manifest struct {
	Chunks []Chunk `json:"chunks"`
}

// Column describes a single detected column of a tabular dataset.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"` // integer | float | boolean | date | datetime | string
}

// Schema is the detected tabular schema of a dataset file.
type Schema struct {
	Columns []Column `json:"columns"`
}

// FileEntry is one versioned file inside a commit (or the staging area).
type FileEntry struct {
	Path     string   `json:"path"`
	Size     int64    `json:"size"`
	SHA256   string   `json:"sha256"` // hash of the whole uncompressed file
	Manifest Manifest `json:"manifest"`

	// Format metadata; zero values mean "not detected".
	Format   string  `json:"format,omitempty"` // csv | tsv | parquet | binary
	Rows     int64   `json:"rows,omitempty"`
	Cols     int     `json:"cols,omitempty"`
	Schema   *Schema `json:"schema,omitempty"`
	StagedAt string  `json:"staged_at,omitempty"`
}

// Commit is an immutable snapshot of a set of files.
type Commit struct {
	ID        string      `json:"id"`
	Parent    string      `json:"parent,omitempty"`
	Message   string      `json:"message"`
	Author    string      `json:"author"`
	CreatedAt time.Time   `json:"created_at"`
	Files     []FileEntry `json:"files"`
}

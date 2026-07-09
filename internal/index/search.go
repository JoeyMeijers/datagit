package index

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joeymeij/dit/internal/model"
)

// SearchHit is one dataset matching a search term, at its most recent
// indexed commit.
type SearchHit struct {
	Path     string
	CommitID string
	Entry    model.FileEntry
}

// Search returns datasets whose path or column names contain term
// (case-insensitive). Each path appears once, from its newest commit.
func (d *DB) Search(term string) ([]SearchHit, error) {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return nil, fmt.Errorf("search term must not be empty")
	}
	// Newest first; the first row per path wins.
	rows, err := d.sql.Query(
		`SELECT cf.commit_id, cf.path, cf.entry_json
		 FROM commit_files cf JOIN commits c ON c.id = cf.commit_id
		 ORDER BY c.created_at DESC, cf.path`)
	if err != nil {
		return nil, fmt.Errorf("searching index: %w", err)
	}
	defer rows.Close()

	var hits []SearchHit
	seen := map[string]bool{}
	for rows.Next() {
		var commitID, path, blob string
		if err := rows.Scan(&commitID, &path, &blob); err != nil {
			return nil, err
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		var e model.FileEntry
		if err := json.Unmarshal([]byte(blob), &e); err != nil {
			return nil, fmt.Errorf("decoding entry for %s: %w", path, err)
		}
		if matches(term, e) {
			hits = append(hits, SearchHit{Path: path, CommitID: commitID, Entry: e})
		}
	}
	return hits, rows.Err()
}

// matches reports whether the term occurs in the path or a column name.
func matches(term string, e model.FileEntry) bool {
	if strings.Contains(strings.ToLower(e.Path), term) {
		return true
	}
	if e.Schema == nil {
		return false
	}
	for _, c := range e.Schema.Columns {
		if strings.Contains(strings.ToLower(c.Name), term) {
			return true
		}
	}
	return false
}

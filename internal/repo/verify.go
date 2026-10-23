package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joeymeij/datagit/internal/model"
)

// Verify checks repository integrity and returns one description per
// problem found; an empty result means the repository is healthy. It
// re-hashes every stored chunk, checks that every commit document
// hashes to its ID, and that every chunk a commit references exists.
func (r *Repo) Verify() ([]string, error) {
	bad, err := r.store.VerifyAll()
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, h := range bad {
		problems = append(problems, "chunk "+h+": corrupt or unreadable")
	}
	commitProblems, err := r.verifyCommits()
	if err != nil {
		return nil, err
	}
	return append(problems, commitProblems...), nil
}

func (r *Repo) verifyCommits() ([]string, error) {
	dir := filepath.Join(metaDir(r.Root), "commits")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading commits: %w", err)
	}
	var problems []string
	for _, e := range entries {
		id, isCommit := strings.CutSuffix(e.Name(), ".json")
		if e.IsDir() || !isCommit {
			continue
		}
		blob, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading commit %s: %w", e.Name(), err)
		}
		var c model.Commit
		if err := json.Unmarshal(blob, &c); err != nil {
			problems = append(problems, fmt.Sprintf("commit %s: undecodable: %v", id, err))
			continue
		}
		if err := validateCommit(c, id); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		for _, f := range c.Files {
			for _, chunk := range f.Manifest.Chunks {
				if !r.store.Has(chunk.Hash) {
					problems = append(problems,
						fmt.Sprintf("commit %s: %s references missing chunk %s", id[:12], f.Path, chunk.Hash))
				}
			}
		}
	}
	sort.Strings(problems)
	return problems, nil
}

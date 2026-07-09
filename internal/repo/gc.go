package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joeymeij/dit/internal/model"
)

// GCResult summarizes a garbage-collection run.
type GCResult struct {
	Removed    int   // objects deleted (or that would be, when dry-run)
	FreedBytes int64 // compressed bytes freed
	Kept       int   // referenced objects retained
}

// GC deletes chunks referenced by no commit and no staged entry. With
// dryRun it only reports what would be deleted. Referenced-set
// collection reads every commit, so GC is safe to run at any time —
// anything reachable is kept.
func (r *Repo) GC(dryRun bool) (GCResult, error) {
	referenced, err := r.referencedHashes()
	if err != nil {
		return GCResult{}, err
	}
	objects, err := r.store.Objects()
	if err != nil {
		return GCResult{}, fmt.Errorf("listing objects: %w", err)
	}
	var res GCResult
	for _, hash := range objects {
		if referenced[hash] {
			res.Kept++
			continue
		}
		size, err := r.store.SizeOf(hash)
		if err != nil {
			return res, err
		}
		if !dryRun {
			if _, err := r.store.Remove(hash); err != nil {
				return res, err
			}
		}
		res.Removed++
		res.FreedBytes += size
	}
	return res, nil
}

// referencedHashes collects every chunk hash reachable from any commit
// or the staging area.
func (r *Repo) referencedHashes() (map[string]bool, error) {
	refs := map[string]bool{}
	add := func(entries []model.FileEntry) {
		for _, e := range entries {
			for _, c := range e.Manifest.Chunks {
				refs[c.Hash] = true
			}
		}
	}

	commitsDir := filepath.Join(metaDir(r.Root), "commits")
	entries, err := os.ReadDir(commitsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading commits: %w", err)
	}
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		blob, err := os.ReadFile(filepath.Join(commitsDir, de.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading commit %s: %w", de.Name(), err)
		}
		var c model.Commit
		if err := json.Unmarshal(blob, &c); err != nil {
			return nil, fmt.Errorf("decoding commit %s: %w", de.Name(), err)
		}
		add(c.Files)
	}

	staged, err := r.idx.Staged()
	if err != nil {
		return nil, err
	}
	add(staged)
	return refs, nil
}

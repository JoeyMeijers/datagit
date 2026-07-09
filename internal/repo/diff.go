package repo

import (
	"io"
	"os"
	"path/filepath"

	"github.com/joeymeij/rowen/internal/diff"
	"github.com/joeymeij/rowen/internal/format"
	"github.com/joeymeij/rowen/internal/model"
	"github.com/joeymeij/rowen/internal/tabular"
)

// Diff compares two commits (by ID or prefix) and returns per-file
// changes. maxSampleRows > 0 additionally captures up to that many
// added/removed rows per file. Row-level diffs stream file content out
// of the object store without materializing files on disk.
func (r *Repo) Diff(refOld, refNew string, maxSampleRows int) ([]diff.FileChange, error) {
	oldCommit, err := r.LoadCommit(refOld)
	if err != nil {
		return nil, err
	}
	newCommit, err := r.LoadCommit(refNew)
	if err != nil {
		return nil, err
	}
	return diff.Commits(oldCommit, newCommit, r.openEntry, diff.Options{MaxSampleRows: maxSampleRows})
}

// DiffWorkTree compares the working tree against HEAD (like `git diff
// HEAD`): tracked files — those in HEAD or the staging area — that were
// modified, deleted, or staged-but-new. Untracked files are ignored.
func (r *Repo) DiffWorkTree(maxSampleRows int) ([]diff.FileChange, error) {
	var oldCommit model.Commit // empty when the repo has no commits yet
	head, err := r.Head()
	if err != nil {
		return nil, err
	}
	if head != "" {
		if oldCommit, err = r.LoadCommit(head); err != nil {
			return nil, err
		}
	}

	newCommit, err := r.workTreeCommit(oldCommit)
	if err != nil {
		return nil, err
	}
	return diff.Commits(oldCommit, newCommit, r.openEntry, diff.Options{MaxSampleRows: maxSampleRows})
}

// workTreeCommit builds a virtual commit from the working-tree state of
// every tracked path (HEAD ∪ staging). Files deleted from the working
// tree are omitted, so the diff reports them as removed.
func (r *Repo) workTreeCommit(head model.Commit) (model.Commit, error) {
	paths := map[string]bool{}
	for _, f := range head.Files {
		paths[f.Path] = true
	}
	staged, err := r.idx.Staged()
	if err != nil {
		return model.Commit{}, err
	}
	for _, f := range staged {
		paths[f.Path] = true
	}

	var c model.Commit
	for path := range paths {
		e, err := r.workTreeEntry(path)
		if os.IsNotExist(err) {
			continue // deleted in the working tree
		}
		if err != nil {
			return model.Commit{}, err
		}
		c.Files = append(c.Files, e)
	}
	return c, nil
}

// workTreeEntry describes a working-tree file as a FileEntry. Its
// Manifest is left empty, which openEntry uses to read from disk
// instead of the object store.
func (r *Repo) workTreeEntry(path string) (model.FileEntry, error) {
	abs := filepath.Join(r.Root, filepath.FromSlash(path))
	fi, err := os.Stat(abs)
	if err != nil {
		return model.FileEntry{}, err
	}
	sum, err := hashFile(abs)
	if err != nil {
		return model.FileEntry{}, err
	}
	e := model.FileEntry{Path: path, Size: fi.Size(), SHA256: sum, Format: tabular.FormatOf(path)}
	// Schema metadata is best effort, as in Add.
	if info, err := format.Extract(abs); err == nil {
		e.Format = info.Format
		e.Rows = info.Rows
		e.Cols = info.Cols
		e.Schema = info.Schema
	}
	return e, nil
}

// openEntry streams a file's content: committed entries (with a chunk
// manifest) come from the object store; working-tree entries (no
// manifest) are read from disk.
func (r *Repo) openEntry(e model.FileEntry) (io.ReadCloser, error) {
	if len(e.Manifest.Chunks) == 0 && e.Size > 0 {
		return os.Open(filepath.Join(r.Root, filepath.FromSlash(e.Path)))
	}
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(r.store.RestoreFile(e.Manifest, pw))
	}()
	return pr, nil
}

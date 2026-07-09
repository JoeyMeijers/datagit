// Package repo orchestrates dit operations against a repository:
// the .dit directory holding objects, commits, refs, config, and the
// SQLite index. It composes the storage, index, and format packages and
// exposes the operations the CLI commands call.
package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joeymeij/dit/internal/format"
	"github.com/joeymeij/dit/internal/index"
	"github.com/joeymeij/dit/internal/model"
	"github.com/joeymeij/dit/internal/storage"
)

// DirName is the repository metadata directory, analogous to .git.
const DirName = ".dit"

// ErrNotARepo is returned when no .dit directory is found.
var ErrNotARepo = errors.New("not a dit repository (run `dit init`)")

// ErrNothingStaged is returned by Commit when the staging area is empty.
var ErrNothingStaged = errors.New("nothing staged; use `dit add <file>` first")

// Repo is an open dit repository.
type Repo struct {
	Root  string // working-tree root (parent of .dit)
	store *storage.Store
	idx   *index.DB
}

// Config holds user-editable repository configuration.
type Config struct {
	Author string `json:"author"`
}

func metaDir(root string) string { return filepath.Join(root, DirName) }

// Init creates a new repository in root. It fails if one already exists.
func Init(root string) error {
	meta := metaDir(root)
	if _, err := os.Stat(meta); err == nil {
		return fmt.Errorf("repository already exists at %s", meta)
	}
	for _, d := range []string{"objects", "commits", "metadata", "refs", "config"} {
		if err := os.MkdirAll(filepath.Join(meta, d), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}
	cfg := Config{Author: defaultAuthor()}
	blob, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(meta, "config", "config.json"), blob, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	// Touch the index so the layout is complete from the start.
	idx, err := index.Open(filepath.Join(meta, "index.db"))
	if err != nil {
		return err
	}
	return idx.Close()
}

func defaultAuthor() string {
	if u := os.Getenv("DIT_AUTHOR"); u != "" {
		return u
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "unknown"
}

// Open locates the repository containing dir (walking upward like git)
// and opens it.
func Open(dir string) (*Repo, error) {
	root, err := findRoot(dir)
	if err != nil {
		return nil, err
	}
	idx, err := index.Open(filepath.Join(metaDir(root), "index.db"))
	if err != nil {
		return nil, err
	}
	return &Repo{
		Root:  root,
		store: storage.NewStore(filepath.Join(metaDir(root), "objects")),
		idx:   idx,
	}, nil
}

// Close releases repository resources.
func (r *Repo) Close() error { return r.idx.Close() }

func findRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if fi, err := os.Stat(filepath.Join(abs, DirName)); err == nil && fi.IsDir() {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", ErrNotARepo
		}
		abs = parent
	}
}

// relPath validates that path lies inside the working tree and returns
// its slash-separated path relative to the repository root.
func (r *Repo) relPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(r.Root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s is outside the repository", path)
	}
	if strings.HasPrefix(rel, DirName) {
		return "", fmt.Errorf("cannot add files inside %s", DirName)
	}
	return filepath.ToSlash(rel), nil
}

// Add stages a file: chunks it into the object store and records its
// manifest plus detected format metadata in the staging area.
func (r *Repo) Add(path string) (model.FileEntry, error) {
	rel, err := r.relPath(path)
	if err != nil {
		return model.FileEntry{}, err
	}
	f, err := os.Open(filepath.Join(r.Root, filepath.FromSlash(rel)))
	if err != nil {
		return model.FileEntry{}, fmt.Errorf("opening %s: %w", rel, err)
	}
	defer f.Close()

	manifest, sum, size, err := r.store.PutFile(f)
	if err != nil {
		return model.FileEntry{}, fmt.Errorf("storing %s: %w", rel, err)
	}
	entry := model.FileEntry{
		Path:     rel,
		Size:     size,
		SHA256:   sum,
		Manifest: manifest,
		StagedAt: time.Now().UTC().Format(time.RFC3339),
	}
	info, err := format.Extract(filepath.Join(r.Root, filepath.FromSlash(rel)))
	if err != nil {
		// Metadata extraction is best-effort: a malformed CSV must not
		// block versioning the bytes. Record it as opaque binary.
		info = format.Info{Format: "binary"}
	}
	entry.Format = info.Format
	entry.Rows = info.Rows
	entry.Cols = info.Cols
	entry.Schema = info.Schema

	if err := r.idx.Stage(entry); err != nil {
		return model.FileEntry{}, err
	}
	return entry, nil
}

// Commit snapshots the staging area merged over the parent commit's
// files, writes the commit JSON, updates the index and HEAD, and clears
// the staging area.
func (r *Repo) Commit(message string) (model.Commit, error) {
	if strings.TrimSpace(message) == "" {
		return model.Commit{}, errors.New("commit message must not be empty")
	}
	staged, err := r.idx.Staged()
	if err != nil {
		return model.Commit{}, err
	}
	if len(staged) == 0 {
		return model.Commit{}, ErrNothingStaged
	}

	files := map[string]model.FileEntry{}
	parent, err := r.Head()
	if err != nil {
		return model.Commit{}, err
	}
	if parent != "" {
		pc, err := r.LoadCommit(parent)
		if err != nil {
			return model.Commit{}, fmt.Errorf("loading parent commit: %w", err)
		}
		for _, f := range pc.Files {
			files[f.Path] = f
		}
	}
	for _, f := range staged {
		f.StagedAt = ""
		files[f.Path] = f
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	c := model.Commit{
		Parent:    parent,
		Message:   message,
		Author:    r.author(),
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	for _, p := range paths {
		c.Files = append(c.Files, files[p])
	}
	c.ID, err = commitID(c)
	if err != nil {
		return model.Commit{}, err
	}

	blob, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return model.Commit{}, err
	}
	commitPath := filepath.Join(metaDir(r.Root), "commits", c.ID+".json")
	if err := os.WriteFile(commitPath, blob, 0o644); err != nil {
		return model.Commit{}, fmt.Errorf("writing commit: %w", err)
	}
	if err := r.idx.PutCommit(c); err != nil {
		return model.Commit{}, err
	}
	if err := r.setHead(c.ID); err != nil {
		return model.Commit{}, err
	}
	if err := r.idx.ClearStaging(); err != nil {
		return model.Commit{}, err
	}
	return c, nil
}

// commitID derives a deterministic ID from the commit's canonical JSON
// (with the ID field empty).
func commitID(c model.Commit) (string, error) {
	c.ID = ""
	blob, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(blob)
	return hex.EncodeToString(sum[:]), nil
}

func (r *Repo) author() string {
	blob, err := os.ReadFile(filepath.Join(metaDir(r.Root), "config", "config.json"))
	if err == nil {
		var cfg Config
		if json.Unmarshal(blob, &cfg) == nil && cfg.Author != "" {
			return cfg.Author
		}
	}
	return defaultAuthor()
}

// Head returns the current HEAD commit ID, or "" for an empty repository.
func (r *Repo) Head() (string, error) {
	blob, err := os.ReadFile(filepath.Join(metaDir(r.Root), "refs", "HEAD"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading HEAD: %w", err)
	}
	return strings.TrimSpace(string(blob)), nil
}

func (r *Repo) setHead(id string) error {
	return os.WriteFile(filepath.Join(metaDir(r.Root), "refs", "HEAD"), []byte(id+"\n"), 0o644)
}

// LoadCommit reads a commit by full ID or unique prefix (min 4 chars).
func (r *Repo) LoadCommit(ref string) (model.Commit, error) {
	id, err := r.resolve(ref)
	if err != nil {
		return model.Commit{}, err
	}
	blob, err := os.ReadFile(filepath.Join(metaDir(r.Root), "commits", id+".json"))
	if err != nil {
		return model.Commit{}, fmt.Errorf("reading commit %s: %w", id, err)
	}
	var c model.Commit
	if err := json.Unmarshal(blob, &c); err != nil {
		return model.Commit{}, fmt.Errorf("decoding commit %s: %w", id, err)
	}
	return c, nil
}

// resolve expands a commit ID prefix to a full ID.
func (r *Repo) resolve(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if len(ref) == 64 {
		return ref, nil
	}
	if len(ref) < 4 {
		return "", fmt.Errorf("commit reference %q too short (min 4 characters)", ref)
	}
	entries, err := os.ReadDir(filepath.Join(metaDir(r.Root), "commits"))
	if err != nil {
		return "", err
	}
	var matches []string
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".json")
		if strings.HasPrefix(id, ref) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no commit matches %q", ref)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("commit reference %q is ambiguous (%d matches)", ref, len(matches))
	}
}

// Log returns commits from HEAD back to the root, newest first.
func (r *Repo) Log() ([]model.Commit, error) {
	head, err := r.Head()
	if err != nil {
		return nil, err
	}
	var out []model.Commit
	for id := head; id != ""; {
		c, err := r.LoadCommit(id)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
		id = c.Parent
	}
	return out, nil
}

// FileState classifies a tracked file's working-tree status.
type FileState struct {
	Path  string
	State string // staged | modified | missing | committed
}

// Status reports staged files and tracked files whose working-tree
// content differs from HEAD.
func (r *Repo) Status() ([]FileState, error) {
	staged, err := r.idx.Staged()
	if err != nil {
		return nil, err
	}
	var out []FileState
	inStaging := map[string]bool{}
	for _, e := range staged {
		inStaging[e.Path] = true
		out = append(out, FileState{Path: e.Path, State: "staged"})
	}

	head, err := r.Head()
	if err != nil {
		return nil, err
	}
	if head == "" {
		return out, nil
	}
	c, err := r.LoadCommit(head)
	if err != nil {
		return nil, err
	}
	for _, f := range c.Files {
		if inStaging[f.Path] {
			continue
		}
		sum, err := hashFile(filepath.Join(r.Root, filepath.FromSlash(f.Path)))
		switch {
		case os.IsNotExist(err):
			out = append(out, FileState{Path: f.Path, State: "missing"})
		case err != nil:
			return nil, err
		case sum != f.SHA256:
			out = append(out, FileState{Path: f.Path, State: "modified"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Checkout restores every file of the given commit into the working
// tree. Unless force is set, it refuses to run while changes are staged.
func (r *Repo) Checkout(ref string, force bool) (model.Commit, error) {
	if !force {
		staged, err := r.idx.Staged()
		if err != nil {
			return model.Commit{}, err
		}
		if len(staged) > 0 {
			return model.Commit{}, errors.New("staging area is not empty; commit first or use --force")
		}
	}
	c, err := r.LoadCommit(ref)
	if err != nil {
		return model.Commit{}, err
	}
	for _, f := range c.Files {
		if err := r.restoreFile(f); err != nil {
			return model.Commit{}, fmt.Errorf("restoring %s: %w", f.Path, err)
		}
	}
	if err := r.setHead(c.ID); err != nil {
		return model.Commit{}, err
	}
	return c, nil
}

// restoreFile writes a file entry to a temp file and renames it into
// place, so an interrupted checkout never leaves truncated files.
func (r *Repo) restoreFile(f model.FileEntry) error {
	dst := filepath.Join(r.Root, filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".dit-restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := r.store.RestoreFile(f.Manifest, tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// Verify checks every object in the store and returns corrupt hashes.
func (r *Repo) Verify() ([]string, error) {
	return r.store.VerifyAll()
}

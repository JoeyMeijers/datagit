package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DefaultBranch is the branch created implicitly by `dgit init`.
const DefaultBranch = "main"

// headRefPrefix marks a symbolic HEAD ("ref: heads/<name>").
const headRefPrefix = "ref: heads/"

// branchNameRe restricts branch names to safe, unambiguous tokens.
var branchNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// Branch describes one branch head.
type Branch struct {
	Name     string
	CommitID string
	Current  bool
}

// MergeResult reports the outcome of a merge.
type MergeResult struct {
	Status   string // "up-to-date" | "fast-forward"
	CommitID string // resulting HEAD commit
	Files    int    // files restored (fast-forward only)
}

func (r *Repo) branchPath(name string) string {
	return filepath.Join(metaDir(r.Root), "refs", "heads", name)
}

// IsBranch reports whether ref names an existing branch.
func (r *Repo) IsBranch(ref string) bool { return r.isBranch(ref) }

// isBranch reports whether ref names an existing branch.
func (r *Repo) isBranch(ref string) bool {
	if !branchNameRe.MatchString(strings.TrimSpace(ref)) {
		return false
	}
	_, err := os.Stat(r.branchPath(strings.TrimSpace(ref)))
	return err == nil
}

// CurrentBranch returns the branch HEAD is on, or "" when detached.
// The branch may be unborn (no commits yet).
func (r *Repo) CurrentBranch() (string, error) {
	raw, err := r.readHeadFile()
	if err != nil {
		return "", err
	}
	name, symbolic := strings.CutPrefix(raw, headRefPrefix)
	if !symbolic {
		return "", nil
	}
	return name, nil
}

// Branches lists all branches sorted by name.
func (r *Repo) Branches() ([]Branch, error) {
	current, err := r.CurrentBranch()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(metaDir(r.Root), "refs", "heads"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading branches: %w", err)
	}
	var out []Branch
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		blob, err := os.ReadFile(r.branchPath(e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading branch %s: %w", e.Name(), err)
		}
		out = append(out, Branch{
			Name:     e.Name(),
			CommitID: strings.TrimSpace(string(blob)),
			Current:  e.Name() == current,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateBranch creates a branch pointing at the current HEAD commit.
func (r *Repo) CreateBranch(name string) error {
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if !branchNameRe.MatchString(name) {
		return fmt.Errorf("invalid branch name %q (allowed: letters, digits, . _ -)", name)
	}
	if r.isBranch(name) {
		return fmt.Errorf("branch %s already exists", name)
	}
	head, err := r.Head()
	if err != nil {
		return err
	}
	if head == "" {
		return errors.New("cannot create a branch before the first commit")
	}
	if err := os.MkdirAll(filepath.Dir(r.branchPath(name)), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(r.branchPath(name), []byte(head+"\n"))
}

// DeleteBranch removes a branch. The current branch cannot be deleted.
func (r *Repo) DeleteBranch(name string) error {
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if !r.isBranch(name) {
		return fmt.Errorf("branch %s does not exist", name)
	}
	current, err := r.CurrentBranch()
	if err != nil {
		return err
	}
	if name == current {
		return fmt.Errorf("cannot delete the current branch %s", name)
	}
	return os.Remove(r.branchPath(name))
}

// Merge fast-forwards the current branch to the tip of other. Diverged
// histories are rejected: three-way dataset merge is future work
// (ADR-0007).
func (r *Repo) Merge(other string) (MergeResult, error) {
	unlock, err := r.lock()
	if err != nil {
		return MergeResult{}, err
	}
	defer unlock()
	current, err := r.CurrentBranch()
	if err != nil {
		return MergeResult{}, err
	}
	if current == "" {
		return MergeResult{}, errors.New("not on a branch (detached HEAD); checkout a branch first")
	}
	if other == current {
		return MergeResult{}, fmt.Errorf("cannot merge %s into itself", other)
	}
	staged, err := r.idx.Staged()
	if err != nil {
		return MergeResult{}, err
	}
	if len(staged) > 0 {
		return MergeResult{}, errors.New("staging area is not empty; commit or discard first")
	}

	head, err := r.Head()
	if err != nil {
		return MergeResult{}, err
	}
	// other may be a branch, a remote-tracking ref (origin/main), or a
	// commit ID/prefix — anything resolvable and fast-forwardable.
	target, err := r.resolve(other)
	if err != nil {
		return MergeResult{}, fmt.Errorf("cannot merge %s: %w", other, err)
	}
	if head == target {
		return MergeResult{Status: "up-to-date", CommitID: head}, nil
	}
	// Target already contained in our history: nothing to do.
	if ok, err := r.isAncestor(target, head); err != nil {
		return MergeResult{}, err
	} else if ok {
		return MergeResult{Status: "up-to-date", CommitID: head}, nil
	}
	// Fast-forward requires our HEAD to be an ancestor of the target
	// (head == "" means an unborn branch, trivially fast-forwardable).
	if head != "" {
		if ok, err := r.isAncestor(head, target); err != nil {
			return MergeResult{}, err
		} else if !ok {
			return MergeResult{}, fmt.Errorf(
				"branches have diverged; three-way merge is not supported yet")
		}
	}

	c, err := r.LoadCommit(target)
	if err != nil {
		return MergeResult{}, err
	}
	if err := r.updateWorktree(c, false); err != nil {
		return MergeResult{}, err
	}
	if err := writeFileAtomic(r.branchPath(current), []byte(c.ID+"\n")); err != nil {
		return MergeResult{}, err
	}
	return MergeResult{Status: "fast-forward", CommitID: c.ID, Files: len(c.Files)}, nil
}

// isAncestor reports whether ancestor is reachable from descendant by
// walking parent links (a commit is its own ancestor).
func (r *Repo) isAncestor(ancestor, descendant string) (bool, error) {
	for id := descendant; id != ""; {
		if id == ancestor {
			return true, nil
		}
		c, err := r.LoadCommit(id)
		if err != nil {
			return false, err
		}
		id = c.Parent
	}
	return false, nil
}

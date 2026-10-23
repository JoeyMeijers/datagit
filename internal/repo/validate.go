package repo

import (
	"fmt"
	"path"
	"strings"

	"github.com/joeymeij/datagit/internal/model"
)

// isObjectID reports whether s is a full commit or chunk ID: 64
// lowercase hex characters (a hex-encoded SHA-256).
func isObjectID(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validateEntryPath rejects file paths that would escape the working
// tree or touch repository metadata when restored. Paths come from
// commit documents, which may originate from an untrusted remote.
func validateEntryPath(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("empty file path")
	case strings.ContainsAny(p, "\\\x00"):
		return fmt.Errorf("file path %q contains a forbidden character", p)
	case path.IsAbs(p) || path.Clean(p) != p:
		return fmt.Errorf("file path %q is not a clean relative path", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("file path %q escapes the repository", p)
		}
	}
	if first, _, _ := strings.Cut(p, "/"); first == DirName {
		return fmt.Errorf("file path %q is inside %s", p, DirName)
	}
	return nil
}

// validateCommit checks that a commit document is well-formed and that
// its content hashes to id. It is the trust boundary for commits
// received from remotes.
func validateCommit(c model.Commit, id string) error {
	if !isObjectID(id) {
		return fmt.Errorf("invalid commit ID %q", id)
	}
	want, err := commitID(c)
	if err != nil {
		return err
	}
	if c.ID != id || want != id {
		return fmt.Errorf("commit %s failed verification: content does not match its ID", id[:12])
	}
	if c.Parent != "" && !isObjectID(c.Parent) {
		return fmt.Errorf("commit %s has invalid parent %q", id[:12], c.Parent)
	}
	for _, f := range c.Files {
		if err := validateEntryPath(f.Path); err != nil {
			return fmt.Errorf("commit %s: %w", id[:12], err)
		}
		for _, chunk := range f.Manifest.Chunks {
			if !isObjectID(chunk.Hash) {
				return fmt.Errorf("commit %s: %s has invalid chunk hash %q", id[:12], f.Path, chunk.Hash)
			}
		}
	}
	return nil
}

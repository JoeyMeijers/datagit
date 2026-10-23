package repo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joeymeij/datagit/internal/model"
	"github.com/joeymeij/datagit/internal/remote"
)

// remotesFile stores configured remotes under .datagit/config.
const remotesFile = "remotes.json"

type remotesConfig struct {
	Remotes map[string]string `json:"remotes"` // name -> URL
}

// SyncResult reports what a publish or fetch transferred.
type SyncResult struct {
	Branch   string
	CommitID string
	Commits  int // commit documents transferred
	Chunks   int // chunk objects transferred
	UpToDate bool
}

func (r *Repo) remotesPath() string {
	return filepath.Join(metaDir(r.Root), "config", remotesFile)
}

// Remotes returns the configured remotes (name -> URL).
func (r *Repo) Remotes() (map[string]string, error) {
	blob, err := os.ReadFile(r.remotesPath())
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading remotes: %w", err)
	}
	var cfg remotesConfig
	if err := json.Unmarshal(blob, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", remotesFile, err)
	}
	if cfg.Remotes == nil {
		cfg.Remotes = map[string]string{}
	}
	return cfg.Remotes, nil
}

func (r *Repo) saveRemotes(remotes map[string]string) error {
	blob, err := json.MarshalIndent(remotesConfig{Remotes: remotes}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(r.remotesPath(), blob)
}

// AddRemote registers a remote after validating its URL scheme.
func (r *Repo) AddRemote(name, url string) error {
	if !branchNameRe.MatchString(name) {
		return fmt.Errorf("invalid remote name %q", name)
	}
	b, err := remote.Open(url)
	if err != nil {
		return err
	}
	b.Close()
	remotes, err := r.Remotes()
	if err != nil {
		return err
	}
	if _, exists := remotes[name]; exists {
		return fmt.Errorf("remote %s already exists", name)
	}
	remotes[name] = url
	return r.saveRemotes(remotes)
}

// RemoveRemote unregisters a remote.
func (r *Repo) RemoveRemote(name string) error {
	remotes, err := r.Remotes()
	if err != nil {
		return err
	}
	if _, exists := remotes[name]; !exists {
		return fmt.Errorf("remote %s does not exist", name)
	}
	delete(remotes, name)
	return r.saveRemotes(remotes)
}

// openRemote opens the backend for a configured remote name.
func (r *Repo) openRemote(name string) (remote.Backend, error) {
	remotes, err := r.Remotes()
	if err != nil {
		return nil, err
	}
	url, ok := remotes[name]
	if !ok {
		return nil, fmt.Errorf("remote %s is not configured (dgit remote add %s <url>)", name, name)
	}
	return remote.Open(url)
}

// Publish uploads the given branch (default: current) to the remote:
// every missing commit and chunk, then the branch ref. The remote ref
// only moves fast-forward; diverged remotes are rejected.
func (r *Repo) Publish(remoteName, branch string) (SyncResult, error) {
	if branch == "" {
		current, err := r.CurrentBranch()
		if err != nil {
			return SyncResult{}, err
		}
		if current == "" {
			return SyncResult{}, errors.New("detached HEAD; name a branch to publish")
		}
		branch = current
	}
	if !r.isBranch(branch) {
		return SyncResult{}, fmt.Errorf("branch %s does not exist", branch)
	}
	tip, err := r.resolve(branch)
	if err != nil {
		return SyncResult{}, err
	}

	b, err := r.openRemote(remoteName)
	if err != nil {
		return SyncResult{}, err
	}
	defer b.Close()

	res := SyncResult{Branch: branch, CommitID: tip}
	refKey := "refs/heads/" + branch

	// Fast-forward safety: the remote tip must be in our history.
	remoteTip, err := readRemoteRef(b, refKey)
	if err != nil {
		return SyncResult{}, err
	}
	if remoteTip == tip {
		res.UpToDate = true
		return res, nil
	}
	if remoteTip != "" {
		ok, err := r.isAncestor(remoteTip, tip)
		if err != nil {
			return SyncResult{}, fmt.Errorf(
				"remote branch %s points to an unknown commit; fetch first (%w)", branch, err)
		}
		if !ok {
			return SyncResult{}, fmt.Errorf("remote branch %s has diverged; fetch first", branch)
		}
	}

	// Upload missing commits (walking back until the remote has one)
	// and every missing chunk they reference.
	for id := tip; id != "" && id != remoteTip; {
		c, err := r.LoadCommit(id)
		if err != nil {
			return res, err
		}
		commitKey := "commits/" + id + ".json"
		has, err := b.Has(commitKey)
		if err != nil {
			return res, err
		}
		if has {
			break // remote already has this commit and, invariantly, its ancestors
		}
		if err := r.uploadChunks(b, c, &res); err != nil {
			return res, err
		}
		blob, err := os.ReadFile(filepath.Join(metaDir(r.Root), "commits", id+".json"))
		if err != nil {
			return res, err
		}
		if err := b.Put(commitKey, bytes.NewReader(blob)); err != nil {
			return res, fmt.Errorf("uploading commit %s: %w", id[:12], err)
		}
		res.Commits++
		id = c.Parent
	}

	if err := b.Put(refKey, strings.NewReader(tip+"\n")); err != nil {
		return res, fmt.Errorf("updating remote ref: %w", err)
	}
	return res, nil
}

func (r *Repo) uploadChunks(b remote.Backend, c model.Commit, res *SyncResult) error {
	for _, f := range c.Files {
		for _, chunk := range f.Manifest.Chunks {
			key := "objects/" + chunk.Hash[:2] + "/" + chunk.Hash[2:]
			has, err := b.Has(key)
			if err != nil {
				return err
			}
			if has {
				continue
			}
			src, err := r.store.OpenRaw(chunk.Hash)
			if err != nil {
				return err
			}
			err = b.Put(key, src)
			src.Close()
			if err != nil {
				return fmt.Errorf("uploading chunk %s: %w", chunk.Hash[:12], err)
			}
			res.Chunks++
		}
	}
	return nil
}

// Fetch downloads every remote branch: missing commits and chunks, and
// remote-tracking refs under refs/remotes/<name>/<branch>. Local
// branches and the working tree are never touched.
func (r *Repo) Fetch(remoteName string) ([]SyncResult, error) {
	b, err := r.openRemote(remoteName)
	if err != nil {
		return nil, err
	}
	defer b.Close()

	refKeys, err := b.List("refs/heads/")
	if err != nil {
		return nil, fmt.Errorf("listing remote refs: %w", err)
	}
	sort.Strings(refKeys)

	var out []SyncResult
	for _, refKey := range refKeys {
		branch := strings.TrimPrefix(refKey, "refs/heads/")
		tip, err := readRemoteRef(b, refKey)
		if err != nil {
			return out, err
		}
		res := SyncResult{Branch: branch, CommitID: tip}
		if err := r.downloadHistory(b, tip, &res); err != nil {
			return out, fmt.Errorf("fetching %s: %w", branch, err)
		}
		trackPath := filepath.Join(metaDir(r.Root), "refs", "remotes", remoteName, branch)
		if err := os.MkdirAll(filepath.Dir(trackPath), 0o755); err != nil {
			return out, err
		}
		if err := writeFileAtomic(trackPath, []byte(tip+"\n")); err != nil {
			return out, err
		}
		res.UpToDate = res.Commits == 0
		out = append(out, res)
	}
	return out, nil
}

// downloadHistory walks the remote commit chain from tip, downloading
// missing commits and their chunks. Every fetched chunk is integrity-
// verified by re-hashing before use.
func (r *Repo) downloadHistory(b remote.Backend, tip string, res *SyncResult) error {
	for id := tip; id != ""; {
		localPath := filepath.Join(metaDir(r.Root), "commits", id+".json")
		if _, err := os.Stat(localPath); err == nil {
			break // we have this commit and, invariantly, its ancestors
		}
		src, err := b.Get("commits/" + id + ".json")
		if err != nil {
			return err
		}
		blob, err := io.ReadAll(src)
		src.Close()
		if err != nil {
			return err
		}
		var c model.Commit
		if err := json.Unmarshal(blob, &c); err != nil {
			return fmt.Errorf("decoding commit %s: %w", id[:12], err)
		}
		if c.ID != id {
			return fmt.Errorf("remote commit %s has mismatched ID %s", id[:12], c.ID)
		}
		if err := r.downloadChunks(b, c, res); err != nil {
			return err
		}
		if err := os.WriteFile(localPath, blob, 0o644); err != nil {
			return err
		}
		if err := r.idx.PutCommit(c); err != nil {
			return err
		}
		res.Commits++
		id = c.Parent
	}
	return nil
}

func (r *Repo) downloadChunks(b remote.Backend, c model.Commit, res *SyncResult) error {
	for _, f := range c.Files {
		for _, chunk := range f.Manifest.Chunks {
			if r.store.Has(chunk.Hash) {
				continue
			}
			src, err := b.Get("objects/" + chunk.Hash[:2] + "/" + chunk.Hash[2:])
			if err != nil {
				return err
			}
			err = r.store.PutRaw(chunk.Hash, src)
			src.Close()
			if err != nil {
				return err
			}
			// Reject tampered or corrupt chunks immediately.
			if err := r.store.ReadChunk(chunk.Hash, io.Discard); err != nil {
				r.store.Remove(chunk.Hash) //nolint:errcheck // best-effort cleanup
				return fmt.Errorf("fetched chunk failed verification: %w", err)
			}
			res.Chunks++
		}
	}
	return nil
}

// readRemoteRef returns the commit ID a remote ref points to, or ""
// when the ref does not exist yet.
func readRemoteRef(b remote.Backend, key string) (string, error) {
	src, err := b.Get(key)
	if errors.Is(err, remote.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer src.Close()
	blob, err := io.ReadAll(io.LimitReader(src, 128))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(blob)), nil
}

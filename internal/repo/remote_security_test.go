package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joeymeij/datagit/internal/model"
)

// publishedRemote publishes one commit from a fresh repo and returns
// the remote's directory and the published commit.
func publishedRemote(t *testing.T) (dir string, c model.Commit) {
	t.Helper()
	url := remoteURL(t)
	src := newRepo(t)
	if err := src.AddRemote("origin", url); err != nil {
		t.Fatal(err)
	}
	id := commitFile(t, src, "d.csv", "x\n1\n", "v1")
	if _, err := src.Publish("origin", ""); err != nil {
		t.Fatal(err)
	}
	c, err := src.LoadCommit(id)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimPrefix(url, "file://"), c
}

// plantCommit writes c into the remote as the tip of main. When rehash
// is set, the ID is recomputed, as an attacker forging content would.
func plantCommit(t *testing.T, dir string, c model.Commit, rehash bool) string {
	t.Helper()
	if rehash {
		id, err := commitID(c)
		if err != nil {
			t.Fatal(err)
		}
		c.ID = id
	}
	blob, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	writeRemoteFile(t, dir, "commits/"+c.ID+".json", string(blob))
	writeRemoteFile(t, dir, "refs/heads/main", c.ID+"\n")
	return c.ID
}

func writeRemoteFile(t *testing.T, dir, key, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fetchInto fetches origin=dir into a fresh repo and returns the error.
func fetchInto(t *testing.T, dir string) (*Repo, error) {
	t.Helper()
	dst := newRepo(t)
	if err := dst.AddRemote("origin", "file://"+dir); err != nil {
		t.Fatal(err)
	}
	_, err := dst.Fetch("origin")
	return dst, err
}

func TestFetchRejectsTamperedCommitContent(t *testing.T) {
	dir, c := publishedRemote(t)
	// Same ID, altered content: the ID no longer matches the hash.
	c.Files[0].Path = "../escaped.csv"
	plantCommit(t, dir, c, false)

	dst, err := fetchInto(t, dir)
	if err == nil || !strings.Contains(err.Error(), "failed verification") {
		t.Fatalf("fetch err = %v, want verification failure", err)
	}
	if _, err := dst.LoadCommit("origin/main"); err == nil {
		t.Fatal("tampered commit must not create a tracking ref")
	}
}

func TestFetchRejectsUnsafePathsEvenWithValidHash(t *testing.T) {
	for _, p := range []string{"../escaped.csv", "/etc/passwd", ".datagit/refs/HEAD", "a/../../b", "a\\b"} {
		t.Run(p, func(t *testing.T) {
			dir, c := publishedRemote(t)
			c.Files[0].Path = p
			plantCommit(t, dir, c, true)
			if _, err := fetchInto(t, dir); err == nil {
				t.Fatalf("fetch accepted commit with path %q", p)
			}
		})
	}
}

func TestFetchRejectsInvalidChunkHash(t *testing.T) {
	dir, c := publishedRemote(t)
	c.Files[0].Manifest.Chunks[0].Hash = "a" // would panic on Hash[:2]
	plantCommit(t, dir, c, true)
	if _, err := fetchInto(t, dir); err == nil || !strings.Contains(err.Error(), "invalid chunk hash") {
		t.Fatalf("fetch err = %v, want invalid chunk hash", err)
	}
}

func TestFetchRejectsInvalidParent(t *testing.T) {
	dir, c := publishedRemote(t)
	c.Parent = "../../outside"
	plantCommit(t, dir, c, true)
	if _, err := fetchInto(t, dir); err == nil || !strings.Contains(err.Error(), "invalid parent") {
		t.Fatalf("fetch err = %v, want invalid parent", err)
	}
}

func TestFetchRejectsInvalidRefContent(t *testing.T) {
	for _, content := range []string{"short\n", "../../commits/x\n", strings.Repeat("Z", 64)} {
		dir, _ := publishedRemote(t)
		writeRemoteFile(t, dir, "refs/heads/main", content)
		if _, err := fetchInto(t, dir); err == nil || !strings.Contains(err.Error(), "invalid commit ID") {
			t.Fatalf("ref %q: fetch err = %v, want invalid commit ID", content, err)
		}
	}
}

func TestFetchRejectsInvalidBranchName(t *testing.T) {
	dir, c := publishedRemote(t)
	writeRemoteFile(t, dir, "refs/heads/.hidden", c.ID+"\n")
	if _, err := fetchInto(t, dir); err == nil || !strings.Contains(err.Error(), "invalid branch name") {
		t.Fatalf("fetch err = %v, want invalid branch name", err)
	}
}

func TestFetchRejectsOversizedCommit(t *testing.T) {
	dir, c := publishedRemote(t)
	writeRemoteFile(t, dir, "commits/"+c.ID+".json", strings.Repeat(" ", maxCommitBytes+1))
	dst := newRepo(t)
	if err := dst.AddRemote("origin", "file://"+dir); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.Fetch("origin"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("fetch err = %v, want size limit error", err)
	}
}

func TestFetchStoresCanonicalCommit(t *testing.T) {
	dir, c := publishedRemote(t)
	// Unknown fields in the remote document must not reach local storage.
	blob, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	padded := strings.Replace(string(blob), "{", `{"injected":"x",`, 1)
	writeRemoteFile(t, dir, "commits/"+c.ID+".json", padded)

	dst, err := fetchInto(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	local, err := os.ReadFile(filepath.Join(dst.Root, DirName, "commits", c.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(local), "injected") {
		t.Fatal("local commit kept unverified remote fields")
	}
}

func TestPublishRejectsInvalidRemoteRef(t *testing.T) {
	dir, _ := publishedRemote(t)
	writeRemoteFile(t, dir, "refs/heads/main", "garbage\n")
	r := newRepo(t)
	if err := r.AddRemote("origin", "file://"+dir); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "d.csv", "x\n1\n", "v1")
	if _, err := r.Publish("origin", ""); err == nil {
		t.Fatal("publish should refuse a remote ref holding garbage")
	}
}

func TestRestoreFileRejectsUnsafePath(t *testing.T) {
	r := newRepo(t)
	if err := r.restoreFile(model.FileEntry{Path: "../escaped.csv"}); err == nil {
		t.Fatal("restoreFile wrote outside the working tree")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.Root), "escaped.csv")); !os.IsNotExist(err) {
		t.Fatalf("escaped file exists: %v", err)
	}
}

func TestResolveRejectsTraversal(t *testing.T) {
	r := newRepo(t)
	commitFile(t, r, "d.csv", "x\n1\n", "v1")
	for _, ref := range []string{"../HEAD", "../../config/config.json", strings.Repeat("Z", 64)} {
		if _, err := r.resolve(ref); err == nil {
			t.Fatalf("resolve(%q) should fail", ref)
		}
	}
}

func TestValidateEntryPath(t *testing.T) {
	ok := []string{"a.csv", "dir/a.csv", "..notes.csv", ".datagit-export.csv", "a/.datagit/b"}
	bad := []string{"", "/abs", "..", "../a", "a/../b", "a//b", "./a", "a/", "a\\b", "a\x00b", ".datagit", ".datagit/x"}
	for _, p := range ok {
		if err := validateEntryPath(p); err != nil {
			t.Errorf("validateEntryPath(%q) = %v, want nil", p, err)
		}
	}
	for _, p := range bad {
		if err := validateEntryPath(p); err == nil {
			t.Errorf("validateEntryPath(%q) = nil, want error", p)
		}
	}
}

func TestIsObjectID(t *testing.T) {
	valid := strings.Repeat("0123456789abcdef", 4)
	cases := map[string]bool{
		valid:                   true,
		"":                      false,
		valid[:63]:              false,
		valid + "0":             false,
		strings.ToUpper(valid):  false,
		strings.Repeat("g", 64): false,
		"../" + valid[:61]:      false,
	}
	for s, want := range cases {
		if got := isObjectID(s); got != want {
			t.Errorf("isObjectID(%q) = %v, want %v", s, got, want)
		}
	}
}

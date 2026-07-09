package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// remoteURL returns a file:// remote in a temp dir.
func remoteURL(t *testing.T) string {
	t.Helper()
	return "file://" + filepath.Join(t.TempDir(), "remote")
}

func TestRemoteAddListRemove(t *testing.T) {
	r := newRepo(t)
	remotes, err := r.Remotes()
	if err != nil || len(remotes) != 0 {
		t.Fatalf("remotes=%v err=%v", remotes, err)
	}
	url := remoteURL(t)
	if err := r.AddRemote("origin", url); err != nil {
		t.Fatal(err)
	}
	if err := r.AddRemote("origin", url); err == nil {
		t.Fatal("duplicate remote should fail")
	}
	if err := r.AddRemote("bad name", url); err == nil {
		t.Fatal("invalid remote name should fail")
	}
	if err := r.AddRemote("x", "ftp://nope"); err == nil {
		t.Fatal("unsupported scheme should fail")
	}
	remotes, err = r.Remotes()
	if err != nil || remotes["origin"] != url {
		t.Fatalf("remotes=%v err=%v", remotes, err)
	}
	if err := r.RemoveRemote("origin"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveRemote("origin"); err == nil {
		t.Fatal("removing missing remote should fail")
	}
}

func TestPublishFetchRoundTrip(t *testing.T) {
	// Producer repo publishes two commits.
	src := newRepo(t)
	url := remoteURL(t)
	if err := src.AddRemote("origin", url); err != nil {
		t.Fatal(err)
	}
	commitFile(t, src, "d.csv", "x\n1\n", "v1")
	c2 := commitFile(t, src, "d.csv", "x\n1\n2\n", "v2")

	res, err := src.Publish("origin", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Commits != 2 || res.Chunks != 2 || res.CommitID != c2 {
		t.Fatalf("publish = %+v", res)
	}

	// Publishing again is a no-op.
	res, err = src.Publish("origin", "")
	if err != nil || !res.UpToDate {
		t.Fatalf("re-publish = %+v err=%v", res, err)
	}

	// Consumer repo fetches everything into tracking refs.
	dst := newRepo(t)
	if err := dst.AddRemote("origin", url); err != nil {
		t.Fatal(err)
	}
	results, err := dst.Fetch("origin")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Branch != DefaultBranch ||
		results[0].Commits != 2 || results[0].Chunks != 2 {
		t.Fatalf("fetch = %+v", results)
	}

	// Remote-tracking ref resolves and merges (unborn main fast-forwards).
	c, err := dst.LoadCommit("origin/" + DefaultBranch)
	if err != nil || c.ID != c2 {
		t.Fatalf("resolve origin/main: %v err=%v", c.ID, err)
	}
	mres, err := dst.Merge("origin/" + DefaultBranch)
	if err != nil || mres.Status != "fast-forward" {
		t.Fatalf("merge = %+v err=%v", mres, err)
	}
	blob, err := os.ReadFile(filepath.Join(dst.Root, "d.csv"))
	if err != nil || string(blob) != "x\n1\n2\n" {
		t.Fatalf("worktree = %q err=%v", blob, err)
	}
	// Fetched store passes integrity checks.
	if bad, err := dst.Verify(); err != nil || len(bad) != 0 {
		t.Fatalf("verify: bad=%v err=%v", bad, err)
	}

	// Re-fetch is up to date.
	results, err = dst.Fetch("origin")
	if err != nil || !results[0].UpToDate {
		t.Fatalf("re-fetch = %+v err=%v", results, err)
	}
}

func TestPublishIncremental(t *testing.T) {
	r := newRepo(t)
	url := remoteURL(t)
	if err := r.AddRemote("origin", url); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "d.csv", "x\n1\n", "v1")
	if _, err := r.Publish("origin", ""); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "d.csv", "x\n1\n2\n", "v2")
	res, err := r.Publish("origin", "")
	if err != nil {
		t.Fatal(err)
	}
	// Only the new commit and its new chunk travel.
	if res.Commits != 1 || res.Chunks != 1 {
		t.Fatalf("incremental publish = %+v", res)
	}
}

func TestPublishRejectsDivergedRemote(t *testing.T) {
	url := remoteURL(t)

	a := newRepo(t)
	if err := a.AddRemote("origin", url); err != nil {
		t.Fatal(err)
	}
	commitFile(t, a, "d.csv", "x\n1\n", "base a")
	if _, err := a.Publish("origin", ""); err != nil {
		t.Fatal(err)
	}

	// A second repo with unrelated history must not clobber the remote.
	b := newRepo(t)
	if err := b.AddRemote("origin", url); err != nil {
		t.Fatal(err)
	}
	commitFile(t, b, "d.csv", "x\n9\n", "base b")
	if _, err := b.Publish("origin", ""); err == nil {
		t.Fatal("publish over unrelated remote history should fail")
	}
	if !strings.Contains(errString(b.Publish("origin", "")), "fetch first") {
		t.Fatal("error should advise fetching first")
	}
}

func errString(_ SyncResult, err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestPublishGuards(t *testing.T) {
	r := newRepo(t)
	// Unconfigured remote.
	if _, err := r.Publish("origin", ""); err == nil {
		t.Fatal("publish without remote should fail")
	}
	if err := r.AddRemote("origin", remoteURL(t)); err != nil {
		t.Fatal(err)
	}
	// Unknown branch.
	if _, err := r.Publish("origin", "ghost"); err == nil {
		t.Fatal("publish of unknown branch should fail")
	}
	// Detached HEAD with no branch argument.
	c1 := commitFile(t, r, "d.csv", "x\n1\n", "v1")
	if _, err := r.Checkout(c1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Publish("origin", ""); err == nil {
		t.Fatal("publish while detached without branch name should fail")
	}
	// Naming the branch explicitly still works while detached.
	if _, err := r.Publish("origin", DefaultBranch); err != nil {
		t.Fatal(err)
	}
}

func TestFetchEmptyRemote(t *testing.T) {
	r := newRepo(t)
	if err := r.AddRemote("origin", remoteURL(t)); err != nil {
		t.Fatal(err)
	}
	results, err := r.Fetch("origin")
	if err != nil || len(results) != 0 {
		t.Fatalf("results=%v err=%v", results, err)
	}
}

func TestFetchDoesNotTouchLocalBranches(t *testing.T) {
	url := remoteURL(t)
	src := newRepo(t)
	if err := src.AddRemote("origin", url); err != nil {
		t.Fatal(err)
	}
	commitFile(t, src, "d.csv", "x\n1\n2\n3\n", "remote v")
	if _, err := src.Publish("origin", ""); err != nil {
		t.Fatal(err)
	}

	dst := newRepo(t)
	if err := dst.AddRemote("origin", url); err != nil {
		t.Fatal(err)
	}
	local := commitFile(t, dst, "other.csv", "y\n1\n", "local v")
	if _, err := dst.Fetch("origin"); err != nil {
		t.Fatal(err)
	}
	head, _ := dst.Head()
	if head != local {
		t.Fatalf("fetch moved HEAD: %s, want %s", head, local)
	}
	blob, err := os.ReadFile(filepath.Join(dst.Root, "other.csv"))
	if err != nil || string(blob) != "y\n1\n" {
		t.Fatalf("worktree changed: %q err=%v", blob, err)
	}
}

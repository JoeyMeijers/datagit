package repo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newRepo initializes and opens a repository in a temp dir.
func newRepo(t *testing.T) *Repo {
	t.Helper()
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func write(t *testing.T, r *Repo, rel, content string) string {
	t.Helper()
	p := filepath.Join(r.Root, rel)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInitCreatesLayout(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"objects", "commits", "metadata", "refs", "config"} {
		if fi, err := os.Stat(filepath.Join(root, DirName, d)); err != nil || !fi.IsDir() {
			t.Errorf("missing directory %s", d)
		}
	}
	if _, err := os.Stat(filepath.Join(root, DirName, "index.db")); err != nil {
		t.Error("missing index.db")
	}
}

func TestInitTwiceFails(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	if err := Init(root); err == nil {
		t.Fatal("second Init should fail")
	}
}

func TestOpenNoRepo(t *testing.T) {
	_, err := Open(t.TempDir())
	if !errors.Is(err, ErrNotARepo) {
		t.Fatalf("err = %v, want ErrNotARepo", err)
	}
}

func TestOpenFromSubdirectory(t *testing.T) {
	r := newRepo(t)
	sub := filepath.Join(r.Root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r2, err := Open(sub)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	if r2.Root != r.Root {
		t.Fatalf("root = %s, want %s", r2.Root, r.Root)
	}
}

func TestAddOutsideRepoRejected(t *testing.T) {
	r := newRepo(t)
	outside := filepath.Join(t.TempDir(), "x.csv")
	if err := os.WriteFile(outside, []byte("a\n1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(outside); err == nil {
		t.Fatal("adding a file outside the repo should fail")
	}
}

func TestAddInsideMetaDirRejected(t *testing.T) {
	r := newRepo(t)
	if _, err := r.Add(filepath.Join(r.Root, DirName, "config", "config.json")); err == nil {
		t.Fatal("adding a file inside .datagit should fail")
	}
}

func TestAddMissingFile(t *testing.T) {
	r := newRepo(t)
	if _, err := r.Add(filepath.Join(r.Root, "nope.csv")); err == nil {
		t.Fatal("adding a missing file should fail")
	}
}

func TestAddExtractsCSVMetadata(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "pop.csv", "region,population\nnorth,100\nsouth,250\n")
	e, err := r.Add(p)
	if err != nil {
		t.Fatal(err)
	}
	if e.Format != "csv" || e.Rows != 2 || e.Cols != 2 {
		t.Fatalf("entry = %+v", e)
	}
	if e.Schema.Columns[1].Type != "integer" {
		t.Fatalf("schema = %+v", e.Schema)
	}
}

func TestCommitEmptyStagingFails(t *testing.T) {
	r := newRepo(t)
	if _, err := r.Commit("msg"); !errors.Is(err, ErrNothingStaged) {
		t.Fatalf("err = %v, want ErrNothingStaged", err)
	}
}

func TestCommitEmptyMessageFails(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "a.csv", "x\n1\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("  "); err == nil {
		t.Fatal("empty message should fail")
	}
}

func TestCommitLogCheckoutRoundTrip(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "data.csv", "x\n1\n2\n")

	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	c1, err := r.Commit("v1")
	if err != nil {
		t.Fatal(err)
	}
	if c1.Parent != "" {
		t.Fatalf("first commit parent = %q, want empty", c1.Parent)
	}

	write(t, r, "data.csv", "x\n1\n2\n3\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	c2, err := r.Commit("v2")
	if err != nil {
		t.Fatal(err)
	}
	if c2.Parent != c1.ID {
		t.Fatalf("parent = %s, want %s", c2.Parent, c1.ID)
	}

	log, err := r.Log()
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].ID != c2.ID || log[1].ID != c1.ID {
		t.Fatalf("log = %+v", log)
	}

	// Roll back to v1 and confirm content is restored.
	if _, err := r.Checkout(c1.ID, false); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(blob) != "x\n1\n2\n" {
		t.Fatalf("restored content = %q", blob)
	}
	head, _ := r.Head()
	if head != c1.ID {
		t.Fatalf("HEAD = %s, want %s", head, c1.ID)
	}
}

func TestCheckoutByPrefix(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "a.csv", "x\n1\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	c, err := r.Commit("v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Checkout(c.ID[:8], false); err != nil {
		t.Fatalf("checkout by prefix: %v", err)
	}
	if _, err := r.Checkout("zzzz", false); err == nil {
		t.Fatal("unknown prefix should fail")
	}
	if _, err := r.Checkout("ab", false); err == nil {
		t.Fatal("too-short prefix should fail")
	}
}

func TestCheckoutRefusesDirtyStaging(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "a.csv", "x\n1\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	c, err := r.Commit("v1")
	if err != nil {
		t.Fatal(err)
	}
	write(t, r, "a.csv", "x\n9\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Checkout(c.ID, false); err == nil {
		t.Fatal("checkout with dirty staging should fail without force")
	}
	if _, err := r.Checkout(c.ID, true); err != nil {
		t.Fatalf("forced checkout: %v", err)
	}
}

func TestStatus(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "a.csv", "x\n1\n")

	states, err := r.Status()
	if err != nil || len(states) != 0 {
		t.Fatalf("fresh repo: states=%v err=%v", states, err)
	}

	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	states, err = r.Status()
	if err != nil || len(states) != 1 || states[0].State != "staged" {
		t.Fatalf("after add: states=%v err=%v", states, err)
	}

	if _, err := r.Commit("v1"); err != nil {
		t.Fatal(err)
	}
	states, err = r.Status()
	if err != nil || len(states) != 0 {
		t.Fatalf("after commit: states=%v err=%v", states, err)
	}

	write(t, r, "a.csv", "x\n2\n")
	states, err = r.Status()
	if err != nil || len(states) != 1 || states[0].State != "modified" {
		t.Fatalf("after modify: states=%v err=%v", states, err)
	}

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	states, err = r.Status()
	if err != nil || len(states) != 1 || states[0].State != "missing" {
		t.Fatalf("after delete: states=%v err=%v", states, err)
	}
}

func TestVerifyHealthyRepo(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "a.csv", "x\n1\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	bad, err := r.Verify()
	if err != nil || len(bad) != 0 {
		t.Fatalf("bad=%v err=%v", bad, err)
	}
}

func TestCommitIDDeterministic(t *testing.T) {
	c1, err := commitID(modelCommitFixture())
	if err != nil {
		t.Fatal(err)
	}
	c2, err := commitID(modelCommitFixture())
	if err != nil {
		t.Fatal(err)
	}
	if c1 != c2 {
		t.Fatalf("commit IDs differ: %s vs %s", c1, c2)
	}
}

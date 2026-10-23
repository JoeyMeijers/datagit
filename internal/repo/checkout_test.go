package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readWorktree(t *testing.T, r *Repo, name string) string {
	t.Helper()
	blob, err := os.ReadFile(filepath.Join(r.Root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(blob)
}

func TestCheckoutRefusesModifiedTrackedFile(t *testing.T) {
	r := newRepo(t)
	c1 := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	commitFile(t, r, "a.csv", "x\n1\n2\n", "v2")
	write(t, r, "a.csv", "x\nunsaved\n")

	_, err := r.Checkout(c1, false)
	if err == nil || !strings.Contains(err.Error(), "a.csv") {
		t.Fatalf("err = %v, want refusal naming a.csv", err)
	}
	if got := readWorktree(t, r, "a.csv"); got != "x\nunsaved\n" {
		t.Fatalf("unsaved edit lost: %q", got)
	}

	if _, err := r.Checkout(c1, true); err != nil {
		t.Fatal(err)
	}
	if got := readWorktree(t, r, "a.csv"); got != "x\n1\n" {
		t.Fatalf("forced checkout content = %q", got)
	}
}

func TestCheckoutRefusesOverwritingUntrackedFile(t *testing.T) {
	r := newRepo(t)
	c1 := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	commitFile(t, r, "b.csv", "y\n1\n", "add b")
	if _, err := r.Checkout(c1, false); err != nil {
		t.Fatal(err)
	}
	// b.csv is untracked at c1; an unrelated local b.csv must survive.
	write(t, r, "b.csv", "mine\n")
	_, err := r.Checkout(DefaultBranch, false)
	if err == nil || !strings.Contains(err.Error(), "b.csv (untracked)") {
		t.Fatalf("err = %v, want untracked refusal", err)
	}
	if got := readWorktree(t, r, "b.csv"); got != "mine\n" {
		t.Fatalf("untracked file overwritten: %q", got)
	}
	// Identical content loses nothing, so the switch proceeds.
	write(t, r, "b.csv", "y\n1\n")
	if _, err := r.Checkout(DefaultBranch, false); err != nil {
		t.Fatal(err)
	}
}

func TestCheckoutRemovesFilesAbsentFromTarget(t *testing.T) {
	r := newRepo(t)
	c1 := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	commitFile(t, r, "b.csv", "y\n1\n", "add b")
	if _, err := r.Checkout(c1, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "b.csv")); !os.IsNotExist(err) {
		t.Fatalf("b.csv should be removed, stat err = %v", err)
	}
	if got := readWorktree(t, r, "a.csv"); got != "x\n1\n" {
		t.Fatalf("a.csv = %q", got)
	}
	if st, err := r.Status(); err != nil || len(st) != 0 {
		t.Fatalf("status after checkout = %+v err=%v, want clean", st, err)
	}
}

func TestCheckoutRestoresMissingTrackedFile(t *testing.T) {
	r := newRepo(t)
	c1 := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	if err := os.Remove(filepath.Join(r.Root, "a.csv")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Checkout(c1, false); err != nil {
		t.Fatalf("a deleted file loses no work: %v", err)
	}
	if got := readWorktree(t, r, "a.csv"); got != "x\n1\n" {
		t.Fatalf("a.csv = %q", got)
	}
}

func TestForcedCheckoutDiscardsStaging(t *testing.T) {
	r := newRepo(t)
	c1 := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	p := write(t, r, "a.csv", "x\n9\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Checkout(c1, true); err != nil {
		t.Fatal(err)
	}
	staged, err := r.idx.Staged()
	if err != nil || len(staged) != 0 {
		t.Fatalf("staged = %v err=%v, want empty after --force", staged, err)
	}
}

func TestRestoredFilePermissions(t *testing.T) {
	r := newRepo(t)
	c1 := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	if err := os.Remove(filepath.Join(r.Root, "a.csv")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Checkout(c1, false); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(r.Root, "a.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", fi.Mode().Perm())
	}
}

func TestMergeRefusesModifiedTrackedFile(t *testing.T) {
	r := newRepo(t)
	commitFile(t, r, "a.csv", "x\n1\n", "v1")
	if err := r.CreateBranch("dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Checkout("dev", false); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "a.csv", "x\n1\n2\n", "v2")
	if _, err := r.Checkout(DefaultBranch, false); err != nil {
		t.Fatal(err)
	}
	write(t, r, "a.csv", "x\nunsaved\n")
	if _, err := r.Merge("dev"); err == nil {
		t.Fatal("fast-forward merge must not overwrite unsaved edits")
	}
	if got := readWorktree(t, r, "a.csv"); got != "x\nunsaved\n" {
		t.Fatalf("unsaved edit lost: %q", got)
	}
}

package repo

import (
	"os"
	"strings"
	"testing"
)

// commitFile writes content to rel, stages it, and commits.
func commitFile(t *testing.T, r *Repo, rel, content, msg string) string {
	t.Helper()
	p := write(t, r, rel, content)
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	c, err := r.Commit(msg)
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func TestInitStartsOnMain(t *testing.T) {
	r := newRepo(t)
	branch, err := r.CurrentBranch()
	if err != nil || branch != DefaultBranch {
		t.Fatalf("branch = %q err=%v, want %s", branch, err, DefaultBranch)
	}
	// Unborn: no commits yet.
	head, err := r.Head()
	if err != nil || head != "" {
		t.Fatalf("head = %q err=%v, want empty", head, err)
	}
}

func TestFirstCommitMaterializesMain(t *testing.T) {
	r := newRepo(t)
	id := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	branches, err := r.Branches()
	if err != nil || len(branches) != 1 {
		t.Fatalf("branches = %v err=%v", branches, err)
	}
	if branches[0].Name != DefaultBranch || branches[0].CommitID != id || !branches[0].Current {
		t.Fatalf("branch = %+v", branches[0])
	}
}

func TestCreateListDeleteBranch(t *testing.T) {
	r := newRepo(t)
	commitFile(t, r, "a.csv", "x\n1\n", "v1")

	if err := r.CreateBranch("dev"); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateBranch("dev"); err == nil {
		t.Fatal("duplicate branch should fail")
	}
	for _, bad := range []string{"", "-x", "a/b", "spaced name"} {
		if err := r.CreateBranch(bad); err == nil {
			t.Fatalf("invalid name %q should fail", bad)
		}
	}

	branches, err := r.Branches()
	if err != nil || len(branches) != 2 {
		t.Fatalf("branches = %v err=%v", branches, err)
	}
	if branches[0].Name != "dev" || branches[0].Current || !branches[1].Current {
		t.Fatalf("branches = %+v", branches)
	}

	if err := r.DeleteBranch(DefaultBranch); err == nil {
		t.Fatal("deleting current branch should fail")
	}
	if err := r.DeleteBranch("dev"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteBranch("dev"); err == nil {
		t.Fatal("deleting missing branch should fail")
	}
}

func TestCreateBranchBeforeFirstCommitFails(t *testing.T) {
	r := newRepo(t)
	if err := r.CreateBranch("dev"); err == nil {
		t.Fatal("branch before first commit should fail")
	}
}

func TestCommitAdvancesCurrentBranchOnly(t *testing.T) {
	r := newRepo(t)
	c1 := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	if err := r.CreateBranch("dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Checkout("dev", false); err != nil {
		t.Fatal(err)
	}
	c2 := commitFile(t, r, "a.csv", "x\n1\n2\n", "v2 on dev")

	branches, err := r.Branches()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, b := range branches {
		got[b.Name] = b.CommitID
	}
	if got["dev"] != c2 || got[DefaultBranch] != c1 {
		t.Fatalf("branches = %v, want dev=%s main=%s", got, c2, c1)
	}
}

func TestCheckoutBranchAttachesAndCommitDetaches(t *testing.T) {
	r := newRepo(t)
	c1 := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	commitFile(t, r, "a.csv", "x\n1\n2\n", "v2")

	// Checkout by raw commit ID detaches HEAD.
	if _, err := r.Checkout(c1[:12], false); err != nil {
		t.Fatal(err)
	}
	branch, err := r.CurrentBranch()
	if err != nil || branch != "" {
		t.Fatalf("branch = %q, want detached", branch)
	}
	// Committing while detached must not move main.
	c3 := commitFile(t, r, "a.csv", "x\n9\n", "detached commit")
	branches, err := r.Branches()
	if err != nil || len(branches) != 1 {
		t.Fatal(err)
	}
	if branches[0].CommitID == c3 {
		t.Fatal("detached commit moved the branch pointer")
	}

	// Checkout by branch name re-attaches.
	if _, err := r.Checkout(DefaultBranch, false); err != nil {
		t.Fatal(err)
	}
	branch, err = r.CurrentBranch()
	if err != nil || branch != DefaultBranch {
		t.Fatalf("branch = %q, want %s", branch, DefaultBranch)
	}
}

func TestResolveBranchName(t *testing.T) {
	r := newRepo(t)
	c1 := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	if err := r.CreateBranch("dev"); err != nil {
		t.Fatal(err)
	}
	c, err := r.LoadCommit("dev")
	if err != nil || c.ID != c1 {
		t.Fatalf("LoadCommit(dev) = %v err=%v, want %s", c.ID, err, c1)
	}
}

func TestMergeFastForward(t *testing.T) {
	r := newRepo(t)
	commitFile(t, r, "a.csv", "x\n1\n", "v1")
	if err := r.CreateBranch("dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Checkout("dev", false); err != nil {
		t.Fatal(err)
	}
	c2 := commitFile(t, r, "a.csv", "x\n1\n2\n", "v2")

	// Back to main, which is now behind dev.
	if _, err := r.Checkout(DefaultBranch, false); err != nil {
		t.Fatal(err)
	}
	res, err := r.Merge("dev")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "fast-forward" || res.CommitID != c2 {
		t.Fatalf("res = %+v", res)
	}
	// Working tree updated and main now at c2.
	blob, err := os.ReadFile(r.Root + "/a.csv")
	if err != nil || string(blob) != "x\n1\n2\n" {
		t.Fatalf("worktree = %q err=%v", blob, err)
	}
	head, _ := r.Head()
	if head != c2 {
		t.Fatalf("head = %s, want %s", head, c2)
	}

	// Merging again is a no-op.
	res, err = r.Merge("dev")
	if err != nil || res.Status != "up-to-date" {
		t.Fatalf("res = %+v err=%v", res, err)
	}
}

func TestMergeAlreadyContained(t *testing.T) {
	r := newRepo(t)
	commitFile(t, r, "a.csv", "x\n1\n", "v1")
	if err := r.CreateBranch("old"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "a.csv", "x\n1\n2\n", "v2")
	// main is ahead of old: nothing to merge.
	res, err := r.Merge("old")
	if err != nil || res.Status != "up-to-date" {
		t.Fatalf("res = %+v err=%v", res, err)
	}
}

func TestMergeDivergedRejected(t *testing.T) {
	r := newRepo(t)
	commitFile(t, r, "a.csv", "x\n1\n", "base")
	if err := r.CreateBranch("dev"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "a.csv", "x\n1\n2\n", "on main")
	if _, err := r.Checkout("dev", true); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "a.csv", "x\n1\n3\n", "on dev")
	if _, err := r.Checkout(DefaultBranch, true); err != nil {
		t.Fatal(err)
	}

	_, err := r.Merge("dev")
	if err == nil || !strings.Contains(err.Error(), "diverged") {
		t.Fatalf("err = %v, want diverged", err)
	}
}

func TestMergeGuards(t *testing.T) {
	r := newRepo(t)
	c1 := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	if err := r.CreateBranch("dev"); err != nil {
		t.Fatal(err)
	}

	// Merging a branch into itself.
	if _, err := r.Merge(DefaultBranch); err == nil {
		t.Fatal("self-merge should fail")
	}
	// Unknown branch.
	if _, err := r.Merge("nope"); err == nil {
		t.Fatal("unknown branch should fail")
	}
	// Dirty staging area.
	p := write(t, r, "a.csv", "x\n9\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Merge("dev"); err == nil {
		t.Fatal("merge with dirty staging should fail")
	}
	if err := r.idx.ClearStaging(); err != nil {
		t.Fatal(err)
	}
	// Detached HEAD.
	if _, err := r.Checkout(c1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Merge("dev"); err == nil {
		t.Fatal("merge while detached should fail")
	}
}

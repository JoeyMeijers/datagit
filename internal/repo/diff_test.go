package repo

import (
	"os"
	"testing"
)

// TestDiffEndToEnd exercises Diff against real commits with content
// streamed back out of the object store.
func TestDiffEndToEnd(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "d.csv", "id,score\n1,2.5\n2,3.5\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	c1, err := r.Commit("v1")
	if err != nil {
		t.Fatal(err)
	}

	write(t, r, "d.csv", "id,score,flag\n1,2.5,true\n3,9.5,false\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	p2 := write(t, r, "extra.csv", "a\n1\n")
	if _, err := r.Add(p2); err != nil {
		t.Fatal(err)
	}
	c2, err := r.Commit("v2")
	if err != nil {
		t.Fatal(err)
	}

	changes, err := r.Diff(c1.ID[:8], c2.ID[:8], 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	byPath := map[string]int{}
	for i, ch := range changes {
		byPath[ch.Path] = i
	}
	mod := changes[byPath["d.csv"]]
	if mod.Status != "modified" || !mod.RowDiff {
		t.Fatalf("mod = %+v", mod)
	}
	if mod.RowsAdded != 2 || mod.RowsRemoved != 2 {
		t.Fatalf("rows +%d -%d, want +2 -2", mod.RowsAdded, mod.RowsRemoved)
	}
	if len(mod.ColsAdded) != 1 || mod.ColsAdded[0].Name != "flag" {
		t.Fatalf("cols added = %+v", mod.ColsAdded)
	}
	if changes[byPath["extra.csv"]].Status != "added" {
		t.Fatalf("extra.csv = %+v", changes[byPath["extra.csv"]])
	}

	if _, err := r.Diff("zzzz", c2.ID, 0); err == nil {
		t.Fatal("unknown ref should fail")
	}
}

func TestDiffWithSamples(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "d.csv", "x\n1\n2\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	c1, err := r.Commit("v1")
	if err != nil {
		t.Fatal(err)
	}
	write(t, r, "d.csv", "x\n2\n9\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	c2, err := r.Commit("v2")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.Diff(c1.ID, c2.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %+v", changes)
	}
	ch := changes[0]
	if len(ch.SampleAdded) != 1 || ch.SampleAdded[0][0] != "9" {
		t.Fatalf("added = %v", ch.SampleAdded)
	}
	if len(ch.SampleRemoved) != 1 || ch.SampleRemoved[0][0] != "1" {
		t.Fatalf("removed = %v", ch.SampleRemoved)
	}
}

func TestDiffWorkTree(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "d.csv", "x\n1\n2\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("v1"); err != nil {
		t.Fatal(err)
	}

	// Clean tree: no changes.
	changes, err := r.DiffWorkTree(0)
	if err != nil || len(changes) != 0 {
		t.Fatalf("clean tree: changes=%v err=%v", changes, err)
	}

	// Modify without committing.
	write(t, r, "d.csv", "x\n1\n2\n3\n")
	changes, err = r.DiffWorkTree(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Status != "modified" {
		t.Fatalf("changes = %+v", changes)
	}
	if changes[0].RowsAdded != 1 || changes[0].RowsRemoved != 0 {
		t.Fatalf("rows +%d -%d, want +1 -0", changes[0].RowsAdded, changes[0].RowsRemoved)
	}
	if len(changes[0].SampleAdded) != 1 || changes[0].SampleAdded[0][0] != "3" {
		t.Fatalf("sample = %v", changes[0].SampleAdded)
	}

	// Untracked files are ignored.
	write(t, r, "untracked.csv", "a\n1\n")
	changes, err = r.DiffWorkTree(0)
	if err != nil || len(changes) != 1 {
		t.Fatalf("untracked leaked in: %+v err=%v", changes, err)
	}

	// Deleting a tracked file reports it as removed.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	changes, err = r.DiffWorkTree(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Status != "removed" || changes[0].Path != "d.csv" {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestDiffWorkTreeStagedNewFile(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "a.csv", "x\n1\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("v1"); err != nil {
		t.Fatal(err)
	}
	// Stage a brand-new file but do not commit: worktree diff vs HEAD
	// shows it as added.
	p2 := write(t, r, "new.csv", "y\n9\n")
	if _, err := r.Add(p2); err != nil {
		t.Fatal(err)
	}
	changes, err := r.DiffWorkTree(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Status != "added" || changes[0].Path != "new.csv" {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestDiffWorkTreeEmptyRepo(t *testing.T) {
	r := newRepo(t)
	changes, err := r.DiffWorkTree(0)
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%v err=%v", changes, err)
	}
}

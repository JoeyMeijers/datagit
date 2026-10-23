//go:build unix

package repo

import (
	"errors"
	"testing"
)

func TestMutatingCommandsRespectLock(t *testing.T) {
	r := newRepo(t)
	commitFile(t, r, "a.csv", "x\n1\n", "v1")
	p := write(t, r, "a.csv", "x\n1\n2\n")

	// A second handle stands in for a concurrent dgit process.
	other, err := Open(r.Root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	unlock, err := other.lock()
	if err != nil {
		t.Fatal(err)
	}

	checks := map[string]error{}
	_, checks["add"] = r.Add(p)
	_, checks["commit"] = r.Commit("v2")
	_, checks["checkout"] = r.Checkout(DefaultBranch, true)
	checks["branch"] = r.CreateBranch("dev")
	checks["branch -d"] = r.DeleteBranch("dev")
	_, checks["merge"] = r.Merge("dev")
	_, checks["gc"] = r.GC(true)
	checks["remote add"] = r.AddRemote("origin", remoteURL(t))
	checks["remote rm"] = r.RemoveRemote("origin")
	_, checks["fetch"] = r.Fetch("origin")
	for name, err := range checks {
		if !errors.Is(err, ErrLocked) {
			t.Errorf("%s while locked: err = %v, want ErrLocked", name, err)
		}
	}

	unlock()
	if _, err := r.Add(p); err != nil {
		t.Fatalf("add after unlock: %v", err)
	}
}

func TestReadOnlyCommandsIgnoreLock(t *testing.T) {
	r := newRepo(t)
	commitFile(t, r, "a.csv", "x\n1\n", "v1")
	unlock, err := r.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := r.Log(); err != nil {
		t.Fatalf("log while locked: %v", err)
	}
	if _, err := r.Status(); err != nil {
		t.Fatalf("status while locked: %v", err)
	}
}

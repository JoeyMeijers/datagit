package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddAcceptsDotPrefixedNames(t *testing.T) {
	r := newRepo(t)
	for _, name := range []string{"..notes.csv", ".datagit-export.csv"} {
		if _, err := r.Add(write(t, r, name, "x\n1\n")); err != nil {
			t.Errorf("add %s: %v", name, err)
		}
	}
}

func TestAddRejectsPathsOutsideWorktree(t *testing.T) {
	r := newRepo(t)
	outside := filepath.Join(filepath.Dir(r.Root), "outside.csv")
	if err := os.WriteFile(outside, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(outside); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("err = %v, want outside-repository error", err)
	}
	inside := filepath.Join(r.Root, DirName, "config", "config.json")
	if _, err := r.Add(inside); err == nil || !strings.Contains(err.Error(), "inside") {
		t.Fatalf("err = %v, want metadata-directory error", err)
	}
}

// fakeInfo is a minimal os.FileInfo for changedDuringRead.
type fakeInfo struct {
	os.FileInfo
	size int64
	mod  time.Time
}

func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) ModTime() time.Time { return f.mod }

func TestChangedDuringRead(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := fakeInfo{size: 10, mod: t0}
	cases := []struct {
		name  string
		after fakeInfo
		read  int64
		want  bool
	}{
		{"unchanged", base, 10, false},
		{"grew", fakeInfo{size: 11, mod: t0}, 10, true},
		{"rewritten same size", fakeInfo{size: 10, mod: t0.Add(time.Second)}, 10, true},
		{"short read", base, 9, true},
	}
	for _, c := range cases {
		if got := changedDuringRead(base, c.after, c.read); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

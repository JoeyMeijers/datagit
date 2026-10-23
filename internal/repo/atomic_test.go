package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ref")
	for _, content := range []string{"first\n", "second\n"} {
		if err := writeFileAtomic(p, []byte(content)); err != nil {
			t.Fatal(err)
		}
		blob, err := os.ReadFile(p)
		if err != nil || string(blob) != content {
			t.Fatalf("content = %q err=%v, want %q", blob, err, content)
		}
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v err=%v, want 0644", fi.Mode().Perm(), err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temp files left behind: %v err=%v", entries, err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "missing", "ref"), nil); err == nil {
		t.Fatal("writing into a missing directory should fail")
	}
}

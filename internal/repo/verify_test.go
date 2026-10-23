package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyDetectsTamperedCommit(t *testing.T) {
	r := newRepo(t)
	id := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	p := filepath.Join(r.Root, DirName, "commits", id+".json")
	blob, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(blob), `"message": "v1"`, `"message": "forged"`, 1)
	if err := os.WriteFile(p, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	problems, err := r.Verify()
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "failed verification") {
		t.Fatalf("problems=%v err=%v, want one verification failure", problems, err)
	}
}

func TestVerifyDetectsUndecodableCommit(t *testing.T) {
	r := newRepo(t)
	id := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	p := filepath.Join(r.Root, DirName, "commits", id+".json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	problems, err := r.Verify()
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "undecodable") {
		t.Fatalf("problems=%v err=%v", problems, err)
	}
}

func TestVerifyDetectsMissingAndCorruptChunks(t *testing.T) {
	r := newRepo(t)
	id := commitFile(t, r, "a.csv", "x\n1\n", "v1")
	c, err := r.LoadCommit(id)
	if err != nil {
		t.Fatal(err)
	}
	hash := c.Files[0].Manifest.Chunks[0].Hash
	chunkPath := filepath.Join(r.Root, DirName, "objects", hash[:2], hash[2:])

	if err := os.WriteFile(chunkPath, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	problems, err := r.Verify()
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "corrupt") {
		t.Fatalf("corrupt chunk: problems=%v err=%v", problems, err)
	}

	if err := os.Remove(chunkPath); err != nil {
		t.Fatal(err)
	}
	problems, err = r.Verify()
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "missing chunk") {
		t.Fatalf("missing chunk: problems=%v err=%v", problems, err)
	}
}

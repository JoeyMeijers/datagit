package remote

import (
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func openFileBackend(t *testing.T) Backend {
	t.Helper()
	b, err := Open("file://" + filepath.Join(t.TempDir(), "remote"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func put(t *testing.T, b Backend, key, content string) {
	t.Helper()
	if err := b.Put(key, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSchemes(t *testing.T) {
	if _, err := Open("no-scheme"); err == nil {
		t.Fatal("URL without scheme should fail")
	}
	if _, err := Open("ftp://x"); err == nil {
		t.Fatal("unsupported scheme should fail")
	}
	if _, err := Open("file://"); err == nil {
		t.Fatal("empty file path should fail")
	}
}

func TestPutGetHas(t *testing.T) {
	b := openFileBackend(t)
	put(t, b, "objects/ab/cdef", "chunk-bytes")

	has, err := b.Has("objects/ab/cdef")
	if err != nil || !has {
		t.Fatalf("has=%v err=%v", has, err)
	}
	has, err = b.Has("objects/zz/zzzz")
	if err != nil || has {
		t.Fatalf("missing key: has=%v err=%v", has, err)
	}

	src, err := b.Get("objects/ab/cdef")
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	blob, _ := io.ReadAll(src)
	if string(blob) != "chunk-bytes" {
		t.Fatalf("content = %q", blob)
	}
}

func TestGetMissingIsErrNotFound(t *testing.T) {
	b := openFileBackend(t)
	_, err := b.Get("nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPutOverwrites(t *testing.T) {
	b := openFileBackend(t)
	put(t, b, "refs/heads/main", "aaa\n")
	put(t, b, "refs/heads/main", "bbb\n")
	src, err := b.Get("refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	blob, _ := io.ReadAll(src)
	if string(blob) != "bbb\n" {
		t.Fatalf("content = %q", blob)
	}
}

func TestList(t *testing.T) {
	b := openFileBackend(t)
	// Empty remote lists nothing.
	keys, err := b.List("refs/heads/")
	if err != nil || len(keys) != 0 {
		t.Fatalf("keys=%v err=%v", keys, err)
	}
	put(t, b, "refs/heads/main", "a")
	put(t, b, "refs/heads/dev", "b")
	put(t, b, "commits/x.json", "c")

	keys, err = b.List("refs/heads/")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(keys)
	want := []string{"refs/heads/dev", "refs/heads/main"}
	if len(keys) != 2 || keys[0] != want[0] || keys[1] != want[1] {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
}

func TestPathEscapeRejected(t *testing.T) {
	b := openFileBackend(t)
	for _, key := range []string{"../evil", "/abs/path"} {
		if err := b.Put(key, strings.NewReader("x")); err == nil {
			t.Fatalf("key %q should be rejected", key)
		}
		if _, err := b.Get(key); err == nil {
			t.Fatalf("get %q should be rejected", key)
		}
	}
}

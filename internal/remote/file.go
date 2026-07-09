package remote

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	registerScheme("file", func(url string) (Backend, error) {
		root := strings.TrimPrefix(url, "file://")
		if root == "" {
			return nil, fmt.Errorf("file remote needs a path: file:///some/dir")
		}
		return &fileBackend{root: root}, nil
	})
}

// fileBackend stores remote data in a local or mounted directory —
// a network share, or simply another folder. It doubles as the
// reference implementation for backend tests.
type fileBackend struct {
	root string
}

// path maps a key to its location, rejecting path escapes.
func (f *fileBackend) path(key string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(key))
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("invalid remote key %q", key)
	}
	return filepath.Join(f.root, clean), nil
}

func (f *fileBackend) Get(key string) (io.ReadCloser, error) {
	p, err := f.path(key)
	if err != nil {
		return nil, err
	}
	r, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return r, err
}

func (f *fileBackend) Put(key string, r io.Reader) error {
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	// Temp-file-and-rename so concurrent readers never see partials.
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (f *fileBackend) Has(key string) (bool, error) {
	p, err := f.path(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (f *fileBackend) List(prefix string) ([]string, error) {
	var keys []string
	err := filepath.WalkDir(f.root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".tmp-") {
			return nil
		}
		rel, err := filepath.Rel(f.root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return keys, err
}

func (f *fileBackend) Close() error { return nil }

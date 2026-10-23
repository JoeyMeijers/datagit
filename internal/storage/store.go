// Package storage implements datagit's content-addressable chunk store.
//
// Files are split into content-defined chunks (see chunker.go), each
// chunk is hashed with SHA-256, compressed with zstandard, and written to
// .datagit/objects/<hh>/<rest-of-hash>. Identical chunks are stored once
// (deduplication). All reads and writes stream; no file is ever fully
// buffered in memory.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrObjectNotFound is returned when a chunk is missing from the store.
var ErrObjectNotFound = errors.New("object not found")

// ErrCorruptObject is returned when a stored chunk fails hash verification.
var ErrCorruptObject = errors.New("object corrupt: hash mismatch")

// Store is a content-addressable chunk store rooted at a directory.
type Store struct {
	root    string // e.g. .datagit/objects
	workers int    // chunks hashed/compressed/decoded concurrently
}

// NewStore opens (or prepares) a store rooted at dir.
func NewStore(dir string) *Store {
	return &Store{root: dir, workers: defaultWorkers()}
}

// objectPath maps a hex hash to its on-disk location, sharded by the
// first two hex characters to keep directories small.
func (s *Store) objectPath(hash string) (string, error) {
	if len(hash) != 64 || strings.ContainsAny(hash, "/\\.") {
		return "", fmt.Errorf("invalid object hash %q", hash)
	}
	return filepath.Join(s.root, hash[:2], hash[2:]), nil
}

// Has reports whether a chunk exists in the store.
func (s *Store) Has(hash string) bool {
	p, err := s.objectPath(hash)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// putChunk writes one chunk if not already present and returns its
// hash. scratch is reused for the compressed bytes.
func (s *Store) putChunk(chunk []byte, scratch *[]byte) (string, error) {
	sum := sha256.Sum256(chunk)
	hash := hex.EncodeToString(sum[:])
	p, err := s.objectPath(hash)
	if err != nil {
		return "", err
	}
	if s.Has(hash) {
		return hash, nil // deduplicated
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("creating object dir: %w", err)
	}
	// Write to a temp file then rename, so readers never see partial objects.
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("creating temp object: %w", err)
	}
	defer os.Remove(tmp.Name())

	compressed, err := compress(*scratch, chunk)
	if err != nil {
		tmp.Close()
		return "", err
	}
	*scratch = compressed
	if _, err := tmp.Write(compressed); err != nil {
		tmp.Close()
		return "", fmt.Errorf("writing object: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("closing temp object: %w", err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return "", fmt.Errorf("committing object: %w", err)
	}
	return hash, nil
}

// ReadChunk writes the decompressed chunk identified by hash to w. The
// content hash is verified before anything is written, so corrupt data
// never reaches w.
func (s *Store) ReadChunk(hash string, w io.Writer) error {
	var scratch []byte
	data, err := s.loadChunk(hash, nil, &scratch)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("writing chunk %s: %w", hash, err)
	}
	return nil
}

// loadChunk appends the verified, decompressed chunk to dst[:0]. scratch
// is reused for the compressed bytes.
func (s *Store) loadChunk(hash string, dst []byte, scratch *[]byte) ([]byte, error) {
	p, err := s.objectPath(hash)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrObjectNotFound, hash)
		}
		return nil, fmt.Errorf("opening object: %w", err)
	}
	compressed, err := readObject(*scratch, f)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("reading object %s: %w", hash, err)
	}
	*scratch = compressed
	data, err := decompress(dst, compressed)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrCorruptObject, hash, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != hash {
		return nil, fmt.Errorf("%w: %s", ErrCorruptObject, hash)
	}
	return data, nil
}

// Objects returns the hash of every object currently in the store.
func (s *Store) Objects() ([]string, error) {
	var hashes []string
	err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".tmp-") {
			return nil
		}
		hashes = append(hashes, filepath.Base(filepath.Dir(path))+d.Name())
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return hashes, err
}

// Remove deletes an object and returns its on-disk (compressed) size.
func (s *Store) Remove(hash string) (int64, error) {
	p, err := s.objectPath(hash)
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrObjectNotFound, hash)
	}
	if err := os.Remove(p); err != nil {
		return 0, fmt.Errorf("removing object %s: %w", hash, err)
	}
	return fi.Size(), nil
}

// SizeOf returns an object's on-disk (compressed) size.
func (s *Store) SizeOf(hash string) (int64, error) {
	p, err := s.objectPath(hash)
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrObjectNotFound, hash)
	}
	return fi.Size(), nil
}

// OpenRaw streams an object's stored (compressed) bytes, for transfer
// to remotes without recompression.
func (s *Store) OpenRaw(hash string) (io.ReadCloser, error) {
	p, err := s.objectPath(hash)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrObjectNotFound, hash)
	}
	return f, err
}

// PutRaw stores already-compressed object bytes under hash (used by
// fetch). Callers must verify integrity afterwards via ReadChunk.
func (s *Store) PutRaw(hash string, r io.Reader) error {
	p, err := s.objectPath(hash)
	if err != nil {
		return err
	}
	if s.Has(hash) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
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

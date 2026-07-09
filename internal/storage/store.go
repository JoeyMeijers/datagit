// Package storage implements datagit's content-addressable chunk store.
//
// Files are split into fixed-size chunks, each chunk is hashed with
// SHA-256, compressed with zstandard, and written to
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

	"github.com/klauspost/compress/zstd"

	"github.com/joeymeij/datagit/internal/model"
)

// DefaultChunkSize is the fixed chunk size (4 MiB). Content-defined
// chunking may replace this later; see ADR-0002.
const DefaultChunkSize = 4 << 20

// ErrObjectNotFound is returned when a chunk is missing from the store.
var ErrObjectNotFound = errors.New("object not found")

// ErrCorruptObject is returned when a stored chunk fails hash verification.
var ErrCorruptObject = errors.New("object corrupt: hash mismatch")

// Store is a content-addressable chunk store rooted at a directory.
type Store struct {
	root      string // e.g. .datagit/objects
	chunkSize int
}

// NewStore opens (or prepares) a store rooted at dir.
func NewStore(dir string) *Store {
	return &Store{root: dir, chunkSize: DefaultChunkSize}
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

// PutFile streams r into the store, splitting it into chunks. It returns
// the file manifest, the whole-file SHA-256 (hex), and total size.
func (s *Store) PutFile(r io.Reader) (model.Manifest, string, int64, error) {
	var (
		manifest model.Manifest
		total    int64
		fileHash = sha256.New()
		buf      = make([]byte, s.chunkSize)
	)
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			chunk := buf[:n]
			fileHash.Write(chunk)
			hash, werr := s.putChunk(chunk)
			if werr != nil {
				return model.Manifest{}, "", 0, werr
			}
			manifest.Chunks = append(manifest.Chunks, model.Chunk{Hash: hash, Size: int64(n)})
			total += int64(n)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return model.Manifest{}, "", 0, fmt.Errorf("reading input: %w", err)
		}
	}
	return manifest, hex.EncodeToString(fileHash.Sum(nil)), total, nil
}

// putChunk writes one chunk if not already present and returns its hash.
func (s *Store) putChunk(chunk []byte) (string, error) {
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

	enc, err := zstd.NewWriter(tmp)
	if err != nil {
		tmp.Close()
		return "", fmt.Errorf("initializing zstd: %w", err)
	}
	if _, err := enc.Write(chunk); err != nil {
		enc.Close()
		tmp.Close()
		return "", fmt.Errorf("compressing chunk: %w", err)
	}
	if err := enc.Close(); err != nil {
		tmp.Close()
		return "", fmt.Errorf("finalizing zstd: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("closing temp object: %w", err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return "", fmt.Errorf("committing object: %w", err)
	}
	return hash, nil
}

// ReadChunk streams the decompressed chunk identified by hash into w,
// verifying its content hash on the way through.
func (s *Store) ReadChunk(hash string, w io.Writer) error {
	p, err := s.objectPath(hash)
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrObjectNotFound, hash)
		}
		return fmt.Errorf("opening object: %w", err)
	}
	defer f.Close()

	dec, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("initializing zstd reader: %w", err)
	}
	defer dec.Close()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), dec.IOReadCloser()); err != nil {
		return fmt.Errorf("decompressing object %s: %w", hash, err)
	}
	if hex.EncodeToString(h.Sum(nil)) != hash {
		return fmt.Errorf("%w: %s", ErrCorruptObject, hash)
	}
	return nil
}

// RestoreFile streams every chunk of manifest, in order, into w.
func (s *Store) RestoreFile(manifest model.Manifest, w io.Writer) error {
	for _, c := range manifest.Chunks {
		if err := s.ReadChunk(c.Hash, w); err != nil {
			return err
		}
	}
	return nil
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

// VerifyAll re-hashes every object in the store and returns the hashes of
// corrupt or unreadable objects. A nil error with an empty slice means the
// store is healthy.
func (s *Store) VerifyAll() (bad []string, err error) {
	err = filepath.WalkDir(s.root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".tmp-") {
			return nil
		}
		hash := filepath.Base(filepath.Dir(path)) + d.Name()
		if verr := s.ReadChunk(hash, io.Discard); verr != nil {
			bad = append(bad, hash)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return bad, err
}

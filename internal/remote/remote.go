// Package remote implements datagit's remote storage: pluggable
// backends addressed by URL, used by publish/fetch to sync objects,
// commits, and refs. See ADR-0008.
//
// A remote holds the same logical layout as a local repository:
//
//	objects/<hh>/<rest>   zstd-compressed chunks
//	commits/<id>.json     commit documents
//	refs/heads/<branch>   branch heads
package remote

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrNotFound is returned when a key does not exist on the remote.
var ErrNotFound = errors.New("remote object not found")

// Backend is a minimal key/value store over a remote location.
// Implementations must be safe for sequential use; datagit syncs
// single-threaded for now.
type Backend interface {
	// Get streams the value at key. Returns ErrNotFound if absent.
	Get(key string) (io.ReadCloser, error)
	// Put stores the value at key, overwriting any existing value.
	Put(key string, r io.Reader) error
	// Has reports whether key exists.
	Has(key string) (bool, error)
	// List returns all keys under prefix (recursive).
	List(prefix string) ([]string, error)
	// Close releases backend resources.
	Close() error
}

// factory creates a backend from a URL.
type factory func(url string) (Backend, error)

var schemes = map[string]factory{}

// registerScheme adds a backend for a URL scheme (e.g. "file", "s3").
func registerScheme(scheme string, f factory) { schemes[scheme] = f }

// Open creates the backend for a remote URL such as
// file:///data/remote or s3://bucket/prefix.
func Open(url string) (Backend, error) {
	scheme, _, ok := strings.Cut(url, "://")
	if !ok {
		return nil, fmt.Errorf("invalid remote URL %q (expected scheme://...)", url)
	}
	f, ok := schemes[scheme]
	if !ok {
		return nil, fmt.Errorf("unsupported remote scheme %q (supported: %s)", scheme, supported())
	}
	return f(url)
}

func supported() string {
	var out []string
	for s := range schemes {
		out = append(out, s)
	}
	return strings.Join(out, ", ")
}

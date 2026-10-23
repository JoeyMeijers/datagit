package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// lockFile is the advisory lock that serializes commands modifying the
// repository (see ADR-0009).
const lockFile = "lock"

// ErrLocked is returned when another process holds the repository lock.
var ErrLocked = errors.New("another dgit process is modifying this repository; retry when it finishes")

// lock takes the exclusive repository lock without waiting and returns
// the function that releases it. The lock is held through an open file
// descriptor, so the OS drops it if the process dies: a crash or Ctrl-C
// never leaves a stale lock behind.
func (r *Repo) lock() (unlock func(), err error) {
	f, err := os.OpenFile(filepath.Join(metaDir(r.Root), lockFile), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening repository lock: %w", err)
	}
	if err := lockExclusive(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

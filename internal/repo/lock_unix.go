//go:build unix

package repo

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func lockExclusive(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrLocked
	}
	if err != nil {
		return fmt.Errorf("locking repository: %w", err)
	}
	return nil
}

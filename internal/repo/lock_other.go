//go:build !unix

package repo

import "os"

// lockExclusive is a no-op on platforms without flock; concurrent
// writers are unsupported there (ADR-0009).
func lockExclusive(*os.File) error { return nil }

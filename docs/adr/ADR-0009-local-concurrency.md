# ADR-0009: Local concurrency — repository lock and atomic metadata writes

## Status

Accepted

## Context

Commits, branch refs, HEAD, remote-tracking refs, and `remotes.json` were written with `os.WriteFile`, which truncates then writes: a crash mid-write left an empty or partial ref, losing the branch pointer. Nothing serialized concurrent `dgit` processes, which datagit explicitly targets (pipelines and Kubernetes jobs sharing a volume). Two concrete races existed: two commits on one branch could both read the same parent and the second ref write silently dropped the first commit; and `gc` could sweep chunks that a concurrent `add` had written but not yet staged, leaving a staged entry pointing at deleted data.

## Decision

**Atomic writes.** Every commit document and ref is written to a temp file in the target directory, fsynced, chmodded to 0644, and renamed into place (`writeFileAtomic`). Readers see either the old or the new content, never a partial one.

**Repository lock.** Commands that modify the repository — `add`, `commit`, `checkout`/`switch`, `branch` create/delete, `merge`, `fetch`, `gc`, `remote add/rm` — take an exclusive, non-blocking `flock` on `.datagit/lock` for their duration. A second writer fails immediately with `ErrLocked` and an actionable message instead of waiting. Read-only commands (`status`, `log`, `diff`, `profile`, `verify`, `publish`'s local reads) take no lock.

`flock` is held through an open file descriptor, so the kernel releases it when the process exits for any reason; a crash or Ctrl-C never leaves a stale lock to delete by hand. It is in the Go standard library on Unix, so no dependency is added. On non-Unix platforms the lock is a no-op.

## Consequences

### Positive

- Crashes can no longer corrupt refs or commit documents.
- Concurrent writers are rejected rather than silently losing commits or chunks.
- No stale-lock cleanup, no new dependency.

### Negative

- Writers fail rather than queue; scripts running concurrent writes must retry.
- Read-only commands may observe state between two steps of a writer (e.g. a commit file written before its ref moves). Every intermediate state is valid, so this is benign.
- `flock` is advisory and unreliable on some network filesystems (older NFS); repositories on such mounts are not protected.
- No lock on Windows until a platform implementation (e.g. `LockFileEx` via `golang.org/x/sys`) is added.

## Alternatives

- **`O_EXCL` lock file (git's `index.lock`).** Portable, but a killed process leaves the file behind and every later command fails until a human deletes it — a poor fit for unattended pipelines.
- **Blocking lock.** Friendlier for back-to-back scripts, but hangs indefinitely behind a stuck process; fail-fast with retry is more explicit.
- **SQLite transactions for all state.** Commit JSON files are the source of truth (ADR-0004); moving refs into SQLite would be a larger redesign for the same guarantee.

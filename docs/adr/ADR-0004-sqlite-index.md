# ADR-0004: SQLite index via mattn/go-sqlite3

## Status

Accepted

## Context

The spec calls for an embedded SQLite index (`.rowen/index.db`) for the staging area and searchable commit/file metadata, with no external database. Two mainstream Go drivers exist: `mattn/go-sqlite3` (CGO, canonical SQLite) and `modernc.org/sqlite` (pure Go transpile, served from gitlab.com — unreachable in the initial development environment).

## Decision

Use `github.com/mattn/go-sqlite3`, opened with WAL mode and a busy timeout. All index access is confined to `internal/index`, so the driver is swappable behind one package. Commit JSON files remain the source of truth; `index.db` is a rebuildable cache plus the staging area.

## Consequences

### Positive

- Battle-tested canonical SQLite; full feature set.
- Single-package isolation keeps a future switch to `modernc.org/sqlite` (to restore CGO-free cross-compilation) a one-file change.

### Negative

- CGO: building requires a C compiler, and cross-compiling is harder. This conflicts with the "single static binary, trivially cross-platform" goal; revisit `modernc.org/sqlite` when fetchable.

## Alternatives

- `modernc.org/sqlite`: preferred long-term (pure Go); unfetchable at decision time.
- JSON-file staging without SQLite: no query capability; spec explicitly includes `index.db` and future `search` builds on it.

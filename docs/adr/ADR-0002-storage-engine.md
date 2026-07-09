# ADR-0002: Content-addressable storage with fixed-size chunks

## Status

Accepted

## Context

datagit must version files from MBs to TBs with streaming I/O, deduplication, and integrity verification, using only local disk (no external database or service).

## Decision

- Files are split into fixed-size 4 MiB chunks, streamed — never fully buffered.
- Chunk identity = SHA-256 of the uncompressed chunk. Objects are stored zstd-compressed at `.datagit/objects/<hh>/<rest>`, sharded by the first two hex chars.
- Writes go to a temp file then `rename`, so readers never observe partial objects; existing objects are never rewritten (dedup by existence check).
- A whole-file SHA-256 is recorded per file for fast status comparison.
- Commits are immutable JSON documents in `.datagit/commits/`; commit ID = SHA-256 of the canonical commit JSON. Commit JSON is the durable source of truth; SQLite (`index.db`, see ADR-0004) is a rebuildable index plus staging area.

Dependencies: `github.com/klauspost/compress` (zstd) — pure Go, widely adopted, no transitive dependencies.

## Consequences

### Positive

- Identical chunks across versions/files are stored once; unchanged multi-GB files cost nothing to re-add.
- Every read path verifies hashes; `dgit verify` re-hashes the whole store.
- Constant memory usage regardless of file size.

### Negative

- Fixed-size chunking dedups poorly against insertions/deletions that shift byte offsets (a one-byte insert re-chunks the remainder of the file). Content-defined chunking (FastCDC) is the planned successor and only changes the chunker, not the store format.
- Chunk hashing/compression is currently sequential; parallel workers are a planned optimization.

## Alternatives

- Content-defined chunking now: better dedup, but more complexity before the core is proven.
- Whole-file objects (no chunking): simpler, but no incremental dedup and unbounded memory-less streaming benefits are lost for TB files.

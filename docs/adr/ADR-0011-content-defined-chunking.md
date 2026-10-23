# ADR-0011: Content-defined chunking (FastCDC)

## Status

Accepted

Supersedes the fixed-size chunking in ADR-0002.

## Context

ADR-0002 split files into fixed 4 MiB chunks and named content-defined chunking (CDC) as the planned successor. With fixed offsets, inserting or deleting any bytes shifts every later chunk boundary, so the rest of the file stops deduplicating.

For datagit's main workload that is the common case. Typical edits to CSV/TSV datasets are rows inserted, deleted or re-ordered near the top. Deduplication is the reason the tool stores versions as chunks at all.

Measured on a 256 MiB CSV, bytes newly stored after an edit:

| Edit | Fixed 4 MiB | FastCDC |
|---|---|---|
| Insert one row after the header | 256 MiB | 0.7 MiB |
| Delete one row mid-file | 128 MiB | 2.8 MiB |
| Change one byte mid-file | 4 MiB | 2.8 MiB |
| Append one row | ~0 MiB | 0.5 MiB |

End to end with the CLI, on a 200 MB CSV with one row inserted at the top, the object store grew 37 MB with fixed chunks and 148 KB with FastCDC.

## Decision

Chunk with FastCDC (Xia et al., USENIX ATC 2016), using normalized chunking (level 2), implemented in `internal/storage/chunker.go` with the standard library only:

- **Gear rolling hash:** `fp = fp<<1 + gear[b]`. The 256-entry gear table comes from SplitMix64 with a fixed seed.
- **Chunk sizes:** a boundary is taken where the hash's top bits are zero.
  - minimum 512 KiB (the hash is skipped below it);
  - average 2 MiB: a 23-bit mask before the average size and a 19-bit mask after it;
  - maximum 8 MiB (forced boundary).
- **Memory:** streaming through a fixed 2×max (16 MiB) buffer, so memory stays bounded for any file size.

The 2 MiB average was chosen over smaller sizes because manifests are stored inline in commit JSON (about 90 bytes per chunk). At 2 MiB a 1 TB file needs about 500k chunks, or about 45 MB of JSON, inside the 64 MiB fetch cap of ADR-0008. A 1 MiB average would exceed it.

**No migration.** The object store and manifest format are unchanged. Manifests list their chunks explicitly, so commits made with fixed chunks restore exactly as before; only new `add`s use CDC. The one cost is that the first CDC version of an existing file doesn't deduplicate against its fixed-chunk history.

**Boundaries are frozen.** The constants and gear table determine where boundaries fall. Changing them doesn't break reads, but new versions would stop deduplicating against all existing data. `TestChunkerGoldenBoundaries` pins them; changing them requires a new ADR.

## Consequences

### Positive

- Insertions and deletions cost about one or two chunks of new storage instead of the rest of the file. The same saving applies to `publish`/`fetch` transfer sizes, which are chunk-granular.
- Re-adding an edited file is faster when most chunks already exist, because existing chunks skip compression (0.88 s → 0.60 s in the end-to-end test).
- No new dependency and no format migration.

### Negative

- `add` throughput on new data drops. The boundary search runs at about 2.4 GiB/s, so on an Apple M4:
  - CSV drops 376 → 307 MiB/s (−19%), because zstd dominates;
  - incompressible data drops 944 → 638 MiB/s (−32%).

  That is still faster than most disks and networks. Pipelining chunking with hashing and compression across cores is the remedy if it matters.
- Chunk sizes vary (0.5–8 MiB) instead of being uniform.
- Uniform input with no content variation, such as all-zero regions, falls back to forced 8 MiB boundaries. These still deduplicate, because identical runs produce identical chunks.
- The inline-manifest limit (about 1 TB per commit at this average) is now explicit. Moving manifests into separate objects is the long-term fix and would let the average shrink.

## Alternatives

- **Keep fixed-size chunks.** Fastest, but deduplication fails on exactly the edits datasets get.
- **Rabin fingerprinting (restic, LBFS).** Equally effective, but several times slower than gear hashing.
- **A third-party FastCDC module.** About 150 lines of stdlib code isn't worth a dependency (AGENTS.md dependency policy), and the boundary-determining constants must stay under our control.
- **Row-aware chunking (cut only at newlines).** Better aligned for CSV, but useless for Parquet and binary formats. FastCDC already localizes row edits to one or two chunks.
- **Smaller average (1 MiB).** Finer deduplication, but halves the maximum file size per commit under inline manifests.

# ADR-0005: Streaming algorithms for diff and profile

## Status

Accepted

## Context

`dgit diff` and `dgit profile` must handle datasets far larger than memory, in one streaming pass, with deterministic results.

## Decision

Shared row access goes through `internal/tabular.Reader` (CSV/TSV now; Parquet and other formats plug in later). Formats without a row reader degrade gracefully: diff falls back to metadata-only (schema and row-count changes from stored commit metadata). Because formats are chosen by file extension, the CSV/TSV reader first sniffs the leading 4 KiB for NUL bytes; non-text content (typically a zip-based spreadsheet saved as `.csv`) is reported as unsupported rather than parsed, so it takes the same fallback on either side of a diff.

**Diff** treats rows as an unordered multiset. Old rows are hashed (128-bit FNV-1a, fields joined by a unit separator) into a count map; new rows decrement it. Surplus new rows are "added", leftover counts are "removed". Reordering is therefore not a change. Without a primary key, "modified rows" cannot be distinguished from remove+add and is not reported; key-aware diff is future work. Schema changes come from commit metadata — free, no data read.

Row *content* samples (`--rows N`) capture up to N added rows during the normal new-side pass; removed-row content requires one extra streaming pass over the old side (hashes alone cannot be inverted). Working-tree diff builds a virtual commit from tracked paths (HEAD ∪ staging): entries without a chunk manifest are read from disk, committed entries from the object store, through the same OpenFunc.

**Profile** computes per column in one pass: missing counts; exact min/max; exact mean/stddev via Welford's algorithm; distinct values tracked exactly up to a 10,000 cap (then flagged as a lower bound); median/quartiles/histogram from a 100,000-value reservoir sample (seeded, deterministic) — exact whenever the column fits the reservoir, flagged as sampled otherwise. Duplicate rows via a 128-bit row-hash set.

## Consequences

### Positive

- Single pass, bounded memory for all column statistics; deterministic outputs.
- Row diff is O(rows) time and independent of row order.

### Negative

- Diff's count map and profile's duplicate-row set grow with the number of *distinct rows* (~24 bytes/row); billions of rows would need a disk-backed or probabilistic structure (future work, flagged here deliberately).
- 128-bit hashing makes collisions negligible but not impossible.
- Sampled quantiles/histograms are approximate beyond 100k values (flagged in output).

## Alternatives

- Exact quantiles via external sort: precise but multi-pass and disk-heavy.
- t-digest sketches: better tails, adds a dependency and complexity; revisit if sampled quantiles prove insufficient.
- Key-based row diff: more useful semantics ("modified rows"), requires user-declared primary keys — planned as an extension, not a replacement.

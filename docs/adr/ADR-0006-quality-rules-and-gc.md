# ADR-0006: Quality rule engine and garbage collection

## Status

Accepted

## Context

The spec requires automatic data-quality validation (missing values, duplicate keys, invalid datatypes, empty/constant columns, outliers) with configurable rules, plus `gc` to reclaim storage. Both must respect the streaming/bounded-memory constraints of ADR-0005.

## Decision

**Rules** live in `.datagit/config/rules.json` as a JSON list; absent config applies `quality.DefaultRules()` (no-empty-columns, no-constant-columns, no-duplicate-rows, max-missing 50%, no-outliers |z|>4). Rule types: `max-missing`, `unique`, `datatype`, `no-empty-columns`, `no-constant-columns`, `no-duplicate-rows`, `no-outliers`. Rules select columns by name or `"*"`; unknown rule types, columns, or datatypes fail before any data is read. `dgit check` evaluates all rules in one streaming pass and exits non-zero on any violation (CI-friendly). Outliers use exact Welford mean/stddev with z-scores evaluated over a bounded sample (flagged when sampled); `unique` tracks up to 2^22 values per column and reports "not verified" beyond that rather than guessing.

**GC** computes the referenced set as every chunk reachable from any commit JSON plus the staging area, then deletes unreferenced objects. `--dry-run` reports without deleting. Since commits are immutable and never expire (no history rewriting exists), reachability is the complete safety invariant; concurrent `add` during `gc` is the one hazard, accepted and documented for a local-first single-user tool.

## Consequences

### Positive

- One pass per file; memory bounded except unique/duplicate tracking, which is capped and honest about giving up.
- Rule config is declarative, versionable alongside data, and validated before running.
- GC can never break a commit: anything reachable is kept.

### Negative

- Outlier detection is z-score only (assumes roughly normal data); IQR-based detection is a natural addition.
- No cross-file rules (e.g. referential integrity) yet.
- Running `gc` concurrently with `add` could collect a just-written, not-yet-staged chunk; acceptable for a single-user CLI, would need lockfiles for shared repos.

## Alternatives

- Reusing `profile.Report` for checks: insufficient for unique/datatype rules (capped distinct counts, no type targets); a dedicated pass keeps each package's guarantees clear.
- Refcounting chunks in SQLite for O(1) gc: faster, but a second source of truth to keep consistent; mark-and-sweep from commit JSON stays correct by construction.

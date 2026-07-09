# ADR-0003: In-house Parquet footer parser

## Status

Accepted

## Context

`rowen add` extracts schema and row count from Parquet files. Existing Go Parquet libraries (`parquet-go/parquet-go`, `xitongsys/parquet-go`) pull large dependency trees (Apache Thrift, Apache Arrow, AWS SDK, cloud storage SDKs), several served from vanity domains unreachable in the development environment. rowen only needs footer *metadata* — not data-page reading.

## Decision

Implement a minimal, decode-only Apache Thrift compact-protocol reader (~150 lines, `internal/format/thrift.go`) and parse the Parquet footer (`FileMetaData`) directly: fields `schema` (list<SchemaElement>) and `num_rows`, with unknown fields skipped generically. Physical types, ConvertedType, and LogicalType map to rowen column types; nested groups are reported as `nested`.

Correctness is anchored by golden files generated with pyarrow (committed under `internal/format/testdata/`, regenerable via `gen_fixtures.py`) plus adversarial tests (truncated footers, bogus lengths, oversized list headers).

## Consequences

### Positive

- Zero Parquet/Thrift dependencies; binary stays small and pure.
- Footer-only reads: extraction cost independent of file size.
- Malformed input fails with errors, never panics or OOMs (bounded reads).

### Negative

- ~300 lines of protocol code we own and must maintain.
- Metadata only — future features needing Parquet *data* (diff, profiling) will require a real reader; this parser does not grow into one.

## Alternatives

- `parquet-go/parquet-go`: full-featured, but unfetchable transitive deps and far more surface than needed.
- Treating Parquet as opaque binary: loses schema/row metadata, a core catalog feature.

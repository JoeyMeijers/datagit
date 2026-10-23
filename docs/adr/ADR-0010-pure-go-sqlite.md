# ADR-0010: Pure-Go SQLite driver (modernc.org/sqlite)

## Status

Accepted

Supersedes the driver choice in ADR-0004.

## Context

ADR-0004 chose `mattn/go-sqlite3` only because `modernc.org/sqlite` couldn't be downloaded at the time. It recorded the pure-Go driver as the preferred long-term option. The cgo driver works against datagit's "single static binary, trivially cross-platform" goal:

- builds need a C compiler;
- cross-compiling needs a C cross-toolchain per target;
- container images need a libc that matches the build.

All index access is confined to `internal/index`, so the swap touches one file.

## Decision

Use `modernc.org/sqlite` v1.60.1 (a C-to-Go translation of upstream SQLite) and build with `CGO_ENABLED=0`. The connection keeps the same settings: WAL journal mode and a 5 s busy timeout, now passed as `_pragma` DSN parameters.

The version was chosen as the newest release that had been public for at least two weeks.

Measured on an Apple M4 (`make bench`, `internal/index/index_bench_test.go`, 5 runs, statistically significant per benchstat):

| Operation | cgo driver | pure Go | Change |
|---|---|---|---|
| Open index | 271 µs | 290 µs | +7% |
| Stage one file | 4.6 µs | 7.6 µs | +67% |
| Commit with 100 files | 1.44 ms | 1.89 ms | +32% |
| Search 5,000 indexed files | 5.8 ms | 12.2 ms | +108% |

The stripped `dgit` binary on darwin/arm64 grows from 5.6 MB to 8.1 MB.

## Consequences

### Positive

- No C compiler is needed to build or install.
- `GOOS=… GOARCH=…` cross-compiles to every platform. `make cross` builds linux/darwin/windows for amd64/arm64, and CI runs it.
- Binaries are static, so scratch and distroless container images work for Kubernetes jobs.
- Simpler CI and contributor setup.

### Negative

- Index operations are 1.3–2× slower. In absolute terms they stay in the micro- to low-millisecond range, negligible next to hashing and compressing data files. Revisit if `search` over very large histories becomes interactive-latency-sensitive.
- The binary is about 2.5 MB larger.
- Eight more indirect modules (modernc libc/mathutil/memory and helpers) to keep updated. govulncheck in CI covers them.
- The minimum Go version rises to 1.26, because the driver and its `golang.org/x/sys` dependency require it. Go only supports its two newest releases (currently 1.26 and 1.27), so no supported toolchain is excluded.
- The race detector still needs cgo on Linux; `make race` enables it explicitly.

## Alternatives

- **Keep `mattn/go-sqlite3`.** Fastest, canonical C SQLite, but keeps the C toolchain requirement and painful cross-compilation.
- **`ncruces/go-sqlite3`.** Pure Go via a WebAssembly build of SQLite running on wazero. Also CGO-free and well maintained, but adds a WASM runtime and is less widely adopted than modernc.
- **Drop SQLite for JSON files.** Rejected in ADR-0004 for the same reasons (no query capability for `search`).

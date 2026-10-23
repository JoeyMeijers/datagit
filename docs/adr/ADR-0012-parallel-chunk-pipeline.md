# ADR-0012: Parallel chunk pipeline and shared zstd codec

## Status

Accepted

## Context

`add`, `checkout`, and `verify` processed one chunk at a time. Profiling `add` showed little time in compression or hashing. The time went elsewhere:

- **Per-chunk streaming zstd encoders.** One was created per chunk, and each spawned helper goroutines (about 37% of CPU went to goroutine wake-ups). It also wrote to disk in 128 KB pieces (about 27% in `write` syscalls).
- **The CSV schema pass.** It ran *after* storing, as a second sequential pass over the file. On a 1 GB CSV it took 1.7 s of the 5.4 s total.

## Decision

1. **Shared single-call codec** (`internal/storage/codec.go`).
   - One process-wide zstd encoder and decoder, used through `EncodeAll`/`DecodeAll`. Each call runs on one goroutine, and each chunk is written with a single `write`.
   - The encoder uses a 2 MiB window (= the average chunk size), which cuts per-encoder memory by about 40% with no measurable loss of compression ratio.
   - The decoder refuses output above `maxChunk`, so a small hostile object from a remote cannot decompress into gigabytes of memory.
   - `ReadChunk` now verifies the hash *before* writing, so corrupt data never reaches the output.
   - Objects written by the old streaming encoder remain readable.

2. **Parallel pipeline** (`internal/storage/pipeline.go`). Chunking and the whole-file hash stay sequential; per-chunk work runs on `min(GOMAXPROCS, 4)` workers, and output order always equals input order.
   - `PutFile`: chunk hash, dedup check, compression and write run in parallel.
   - `RestoreFile`: load, decode and verify run in parallel, up to `workers` chunks ahead of an in-order writer, which writes nothing past the first failed chunk.
   - `VerifyAll`: objects are verified in parallel.

   Memory is bounded by construction: each operation circulates a fixed set of workers+1 chunk buffers through a free list, plus one scratch buffer per worker. There is no `sync.Pool`, whose unbounded growth and 8 MiB-capacity buffers had pushed peak memory to about 500 MB in an earlier iteration.

3. **Format detection runs concurrently with storage** in `Repo.Add`. The two passes are independent, so `add` takes as long as the slower one, not both.

**Why 4 workers:** in storage benchmarks, 8 workers gained under 10% over 4 for `add` and 10–17% for restore. CLI `add` gained nothing, because CSV schema detection becomes the bottleneck. Meanwhile peak memory kept rising (169 → 182 MB).

**Containers:** since Go 1.25, `GOMAXPROCS` follows the container CPU limit, so a pod limited to 1 CPU automatically uses one worker and the smallest memory footprint.

### Measurements (Apple M4, 1 GB CSV, `dgit` CLI)

| Command | Before | After | Speed-up |
|---|---|---|---|
| `add` | 5.40 s | 1.92 s | 2.8× |
| `verify` | 0.81 s | 0.32 s | 2.5× |
| `checkout` | 1.25 s | 0.46 s | 2.7× |

Peak memory of `add` by CPU count:

| CPUs | Before | After | `add` time after |
|---|---|---|---|
| 1 | 117 MB | 99 MB | 6.26 s |
| 2 | 104 MB | 118 MB | 3.14 s |
| 4 | 104 MB | 169 MB | 1.78 s |
| 10 | 105 MB | 182 MB | 1.84 s |

Storage layer alone (`BenchmarkWorkers`, 64 MiB):

| Workers | Add CSV | Restore CSV | Restore random |
|---|---|---|---|
| 1 | 300 MB/s | 1.7 GB/s | 3.8 GB/s |
| 4 | 930 MB/s | 3.2 GB/s | 7.2 GB/s |

The compressed store size is unchanged (183 MB for the 1 GB CSV).

## Consequences

### Positive

- `add`, `checkout`, and `verify` are 2.5–2.9× faster on multi-core machines.
- On one CPU, peak memory is lower than before.
- Decompression bombs from hostile remotes are rejected, and corrupt chunks never reach restored files.

### Negative

- More concurrent code. This is mitigated by tests for:
  - identical results for any worker count;
  - clean shutdown with no leaked goroutines on store, read and write errors;
  - restore stopping exactly at the first bad chunk;
  - concurrent `PutFile` on one store.

  These tests also caught a real deadlock in restore's error path during development.
- With 4+ CPUs, peak memory is up to about 80 MB higher than before.
- `add` of incompressible data plateaus around 1.2 GB/s, because chunking and the whole-file hash are sequential. Splitting the whole-file hash into its own pipeline stage would lift that if it ever matters.
- CSV schema detection (`encoding/csv`, which allocates one string per row) is now the `add` bottleneck at about 1.7 s/GB. A faster row counter would change behaviour on malformed CSV, which currently falls back to `binary`, so it is deliberately left alone.

## Alternatives

- **Keep the streaming encoder, which parallelizes within a chunk.** Tried as a first step: switching to single-call encoding *without* the pipeline made CSV `add` 23% slower and restore 32% slower. That is why the pipeline is the actual fix.
- **`sync.Pool` buffers.** Tried; peak memory reached about 500 MB because pooled buffers had full 8 MiB capacity and the pool's size is unbounded.
- **Lower zstd level, or `WithLowerEncoderMem`.** Compression level changes the store size, and the low-memory mode saved less than the window change at equal speed.

# datagit

Git-like version control for datasets. A single Go binary that brings commits, rollback, integrity verification, and automatic schema detection to CSV, TSV, and Parquet files.

## Status

Early MVP. Core versioning works end to end; profiling, diff, lineage, cloud storage, and Kubernetes integration are on the roadmap (see `datagit_project_prompt.md` for the original project vision).

## Install

```bash
make build          # compiles ./bin/dgit
make install        # installs into $(go env GOPATH)/bin
```

Requires Go 1.26+. No C compiler is needed: the embedded SQLite index uses a pure-Go driver, so `dgit` is a static binary and `make cross` builds it for Linux, macOS, and Windows from any machine (ADR-0010). Run `make help` for all targets; `make check` (fmt + vet + tests) is the main CI entry point; CI also runs `make race`, `make lint` (golangci-lint), and `make vuln` (govulncheck).

## Usage

```bash
dgit init                      # create a repository in the current directory
dgit add population.csv        # stage a file (chunks, hashes, detects schema)
dgit commit -m "Initial import"
dgit status                    # staged / modified / missing files
dgit log                       # commit history
dgit schema population.csv     # detected column types at HEAD
dgit diff                      # uncommitted changes: working tree vs HEAD
dgit diff <old> [<new>]        # compare commits (new defaults to HEAD)
dgit diff --rows 20 <old>      # also print the added/removed rows themselves
dgit profile population.csv    # column statistics; --hist adds histograms
dgit check population.csv     # validate against quality rules (exit 1 on violations)
dgit search income             # find datasets by path or column name
dgit branch [name]             # list or create branches (-d deletes)
dgit switch [-c] <branch>      # switch branches (-c creates it first)
dgit checkout <branch|commit>  # switch branches, or detach at a commit
dgit merge <branch>            # fast-forward the current branch
dgit verify                    # integrity-check every chunk and commit
dgit gc [--dry-run]            # delete chunks no commit references
dgit remote add origin <url>   # configure a remote (file:// today, s3:// next)
dgit publish [remote] [branch] # upload branch history to the remote
dgit fetch [remote]            # download branches into origin/<branch> refs
```

Commits can be referenced by branch name or any unique ID prefix of at least 4 characters.

Repositories start on a `main` branch. `checkout <branch>` switches branches; `checkout <commit>` detaches HEAD at that commit. Checkout makes the working tree match the target exactly — files the target does not track are removed — and refuses to run if that would destroy work: staged changes, edited tracked files, or untracked files the target would overwrite. `--force` overrides the check and discards staged changes. Fast-forward `merge` applies the same protection. `merge` is currently fast-forward only — diverged branches are rejected until three-way dataset merge lands (ADR-0007).

## How it works

`dgit init` creates a `.datagit/` directory:

```
.datagit/
    objects/     content-addressable chunks (SHA-256, zstd-compressed)
    commits/     one JSON document per commit (source of truth)
    metadata/    reserved for the metadata catalog
    refs/        HEAD, heads/<branch>, remotes/<remote>/<branch>
    config/      config.json (author), remotes.json
    index.db     SQLite: staging area + queryable commit index
    lock         advisory lock held by commands that modify the repository
```

`add` streams a file in 4 MiB chunks: each chunk is SHA-256-hashed, zstd-compressed, and stored once — identical chunks across files and versions are deduplicated. Nothing is ever loaded fully into memory, so multi-GB files are fine.

`add` also detects the dataset format. CSV/TSV files are streamed once to count rows and infer column types (integer, float, boolean, date, datetime, string). Parquet files are read footer-only — schema and row count come from the file metadata, so extraction cost is independent of file size.

`commit` snapshots the staging area merged over the parent commit, writes an immutable JSON commit whose ID is the SHA-256 of its content, and advances HEAD.

`checkout` restores files via temp-file-and-rename, so an interrupted checkout never leaves truncated files; commits, refs, and config are written the same way. Commands that modify the repository take an exclusive lock and fail fast with a clear error if another `dgit` process holds it; read-only commands never wait (ADR-0009). `verify` re-hashes every stored chunk, checks that every commit document hashes to its ID, and reports chunks that commits reference but the store lacks.

`diff` compares two commits — or, with no arguments, the working tree against HEAD (like `git diff HEAD`; untracked files are ignored). It reports added/removed files, schema changes (columns added/removed, type changes), and — for CSV/TSV — rows added/removed, computed as an order-insensitive streaming hash comparison directly from the object store. Re-sorting a file is therefore not a change. `--rows N` prints up to N of the actual added/removed rows per file. Parquet files get metadata-only diffs for now.

`profile` computes per-column statistics in one streaming pass: missing values, distinct counts, min/max/mean/stddev (exact), median/quartiles/histograms (exact up to 100k values per column, reservoir-sampled beyond), plus duplicate-row counts. See ADR-0005 for the algorithms and their memory bounds.

`check` validates datasets against quality rules and exits non-zero on violations, so it slots into CI. Without configuration it applies sane defaults (empty columns, constant columns, duplicate rows, >50% missing, |z|>4 outliers). `search` matches dataset paths and column names across the commit index. `gc` mark-and-sweeps chunks unreachable from every commit and the staging area.

Remotes are serverless: any storage location holding the `objects/commits/refs` layout works. `publish` uploads only missing commits and chunks and moves the remote ref fast-forward-only (it can never destroy remote history — diverged remotes require a fetch first). `fetch` downloads into remote-tracking refs (`origin/main`) without touching your branches; merge with `dgit merge origin/main`. Remotes are treated as untrusted: every fetched chunk and commit is hash-verified on arrival, and commits are rejected if they reference file paths outside the working tree, malformed chunk hashes, or invalid parents. Remote branch names and refs are validated before they touch the local filesystem. `file://` remotes work today (local paths, network shares); `s3://` for S3/MinIO/GCS is the next backend behind the same interface (ADR-0008).

## Configuration

`.datagit/config/config.json`:

```json
{ "author": "your name" }
```

Falls back to `$DIT_AUTHOR`, then `$USER`.

`.datagit/config/rules.json` (optional) overrides the default quality rules:

```json
{
  "rules": [
    { "type": "unique", "columns": ["id"] },
    { "type": "datatype", "columns": ["age"], "datatype": "integer" },
    { "type": "max-missing", "ratio": 0.1, "columns": ["*"] },
    { "type": "no-duplicate-rows" },
    { "type": "no-empty-columns" },
    { "type": "no-constant-columns" },
    { "type": "no-outliers", "zscore": 4 }
  ]
}
```

See ADR-0006 for rule semantics.

## Development

```bash
make check       # gofmt check + go vet + all tests (offline, deterministic)
make race        # tests under the race detector (needs a C compiler on Linux)
make bench       # benchmarks
make cross       # cross-compile for all release platforms
make lint        # golangci-lint, configured in .golangci.yml
make vuln        # govulncheck dependency scan
```

Architecture decision records live in `docs/adr/`. Engineering standards are in `AGENTS.md`. Parquet test fixtures are committed under `internal/format/testdata/` and regenerated with `gen_fixtures.py` (pyarrow).

## Roadmap

s3:// remote backend (S3/MinIO/GCS), key-aware row diff, three-way merge, Parquet row reading, cross-file validation rules, lineage tracking, Kubernetes job linkage, and a plugin system.

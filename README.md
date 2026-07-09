# datagit

Git-like version control for datasets. A single Go binary that brings commits, rollback, integrity verification, and automatic schema detection to CSV, TSV, and Parquet files.

## Status

Early MVP. Core versioning works end to end; profiling, diff, lineage, cloud storage, and Kubernetes integration are on the roadmap (see `datagit_project_prompt.md` for the original project vision).

## Install

```bash
make build          # compiles ./bin/dgit
make install        # installs into $(go env GOPATH)/bin
```

Requires Go 1.24+ and a C compiler (for the embedded SQLite index). Run `make help` for all targets; `make check` (fmt + vet + tests) is the CI entry point.

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
dgit checkout <branch|commit>  # switch branches, or detach at a commit
dgit merge <branch>            # fast-forward the current branch
dgit verify                    # integrity-check every stored object
dgit gc [--dry-run]            # delete chunks no commit references
```

Commits can be referenced by branch name or any unique ID prefix of at least 4 characters.

Repositories start on a `main` branch. `checkout <branch>` switches branches; `checkout <commit>` detaches HEAD at that commit. `merge` is currently fast-forward only — diverged branches are rejected until three-way dataset merge lands (ADR-0007).

## How it works

`dgit init` creates a `.datagit/` directory:

```
.datagit/
    objects/     content-addressable chunks (SHA-256, zstd-compressed)
    commits/     one JSON document per commit (source of truth)
    metadata/    reserved for the metadata catalog
    refs/        HEAD
    config/      config.json (author)
    index.db     SQLite: staging area + queryable commit index
```

`add` streams a file in 4 MiB chunks: each chunk is SHA-256-hashed, zstd-compressed, and stored once — identical chunks across files and versions are deduplicated. Nothing is ever loaded fully into memory, so multi-GB files are fine.

`add` also detects the dataset format. CSV/TSV files are streamed once to count rows and infer column types (integer, float, boolean, date, datetime, string). Parquet files are read footer-only — schema and row count come from the file metadata, so extraction cost is independent of file size.

`commit` snapshots the staging area merged over the parent commit, writes an immutable JSON commit whose ID is the SHA-256 of its content, and advances HEAD.

`checkout` restores files via temp-file-and-rename, so an interrupted checkout never leaves truncated files. `verify` re-hashes every stored chunk.

`diff` compares two commits — or, with no arguments, the working tree against HEAD (like `git diff HEAD`; untracked files are ignored). It reports added/removed files, schema changes (columns added/removed, type changes), and — for CSV/TSV — rows added/removed, computed as an order-insensitive streaming hash comparison directly from the object store. Re-sorting a file is therefore not a change. `--rows N` prints up to N of the actual added/removed rows per file. Parquet files get metadata-only diffs for now.

`profile` computes per-column statistics in one streaming pass: missing values, distinct counts, min/max/mean/stddev (exact), median/quartiles/histograms (exact up to 100k values per column, reservoir-sampled beyond), plus duplicate-row counts. See ADR-0005 for the algorithms and their memory bounds.

`check` validates datasets against quality rules and exits non-zero on violations, so it slots into CI. Without configuration it applies sane defaults (empty columns, constant columns, duplicate rows, >50% missing, |z|>4 outliers). `search` matches dataset paths and column names across the commit index. `gc` mark-and-sweeps chunks unreachable from every commit and the staging area.

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
go test ./...    # all tests are offline and deterministic
go vet ./...
```

Architecture decision records live in `docs/adr/`. Engineering standards are in `AGENTS.md`. Parquet test fixtures are committed under `internal/format/testdata/` and regenerated with `gen_fixtures.py` (pyarrow).

## Roadmap

Key-aware row diff, Parquet row reading, cross-file validation rules, lineage tracking, S3/Azure/GCS remotes, Kubernetes job linkage, and a plugin system.

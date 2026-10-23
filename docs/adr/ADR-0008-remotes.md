# ADR-0008: Remotes — pluggable backends, publish/fetch sync

## Status

Accepted

## Context

datagit was purely local. The spec requires `publish`/`fetch` against object storage (S3, Azure, MinIO, GCS) so pipelines — especially Kubernetes jobs — can share dataset versions without a shared filesystem or server.

## Decision

A remote is a dumb key/value store mirroring the local layout (`objects/`, `commits/`, `refs/heads/`) behind a five-method `remote.Backend` interface (Get/Put/Has/List/Close), selected by URL scheme through a registry. The first backend is `file://` (any local or mounted path — network shares, and the reference implementation for tests). S3/MinIO is the next backend: the interface was sized so it needs exactly those five operations.

**Publish** uploads the branch's missing history: it walks parent links from the tip, stops at the first commit the remote already has (remotes invariantly hold ancestor-closed sets), uploads each missing commit's chunks (raw compressed bytes, no recompression) then the commit document, and finally moves the remote ref. The ref only moves fast-forward: a remote tip that is unknown locally or not our ancestor aborts with "fetch first" — publish can never destroy remote history.

**Fetch** downloads every remote branch into remote-tracking refs (`refs/remotes/<name>/<branch>`), never touching local branches or the working tree. Remotes are untrusted input. Every fetched chunk is integrity-verified by decompress-and-rehash before acceptance. Every fetched commit document must hash to its claimed ID, may only reference clean relative file paths inside the working tree (no `..`, absolute paths, backslashes, or `.datagit/`), well-formed chunk hashes, and a well-formed parent; it is stored locally in canonical re-encoded form so unverified fields never land on disk. Remote branch names must match the local branch-name rules and remote refs must hold a well-formed commit ID before either is used to build a local path. Commit documents are capped at 64 MiB. `restoreFile` re-validates paths regardless of origin (defense in depth). Remote-tracking refs resolve anywhere commit refs do (`dgit merge origin/main`, `dgit diff origin/main`).

## Consequences

### Positive

- No server: any S3 bucket or shared folder is a fully functional remote. Kubernetes jobs fetch → work → publish with only object-storage credentials.
- Transfers are incremental at chunk granularity; unchanged data never travels.
- Tampered remotes are detected at fetch time (hash verification on both commits and chunks), and a malicious remote cannot write outside the working tree.

### Negative

- Ref updates are not atomic across concurrent publishers (last write wins on the ref file/key; the fast-forward check narrows but cannot close the race). Fine for single-writer-per-branch workflows; conditional writes (S3 If-Match) are the future fix.
- Fetch pulls full branch history, not shallow; acceptable until histories grow long.
- No pruning of remote objects (`gc` is local-only); remote garbage accumulates after diverged-history mistakes.

## Alternatives

- Git-style smart protocol/server: far more machinery for no benefit at this scale; dumb storage is the DVC/lakeFS-proven model.
- Uploading uncompressed chunks: simpler debugging, but doubles transfer size for no gain since chunk IDs are content hashes of the uncompressed data either way.

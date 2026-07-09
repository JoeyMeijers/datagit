# ADR-0007: Branches with fast-forward-only merge

## Status

Accepted

## Context

datagit's history was a single line: HEAD held a raw commit ID, and checking out an old commit silently orphaned newer commits. Users expect git-like branches for experimenting with dataset revisions.

## Decision

Git's ref model, minimally: branch heads are files under `.datagit/refs/heads/<name>` containing a commit ID. HEAD is either symbolic (`ref: heads/<name>`) or a raw commit ID (detached). `dgit init` starts on an unborn `main` that materializes at the first commit. `commit` advances the current branch (or HEAD itself when detached). `checkout <branch>` attaches; `checkout <commit>` detaches. Commit references everywhere (`log`, `diff`, `checkout`, `merge`) resolve branch names first, then full IDs, then unique prefixes. Branch names match `[A-Za-z0-9][A-Za-z0-9._-]{0,99}`.

`merge` is **fast-forward only**: it moves the current branch to the target tip when the current HEAD is an ancestor of the target (walking parent links), restores the working tree, and reports "already up to date" when the target is already contained. Diverged histories are rejected with an explicit error.

Repositories created before this ADR (raw commit ID in HEAD) keep working: they are simply treated as detached.

## Consequences

### Positive

- Zero storage-format changes; branches are plain ref files, trivially inspectable.
- Old commits are no longer silently orphaned by checkout — they stay reachable from branches.
- Ancestor walks are O(history length); no graph index needed at this scale.

### Negative

- No three-way merge: diverged dataset branches cannot be combined yet. Doing this correctly needs key-aware row merging (base/ours/theirs) and dataset conflict semantics — deliberately deferred; it builds on the planned key-aware diff.
- One commit parent only: merge commits (two parents) don't exist until real merges do.

## Alternatives

- Immediate three-way merge: high risk of silently wrong data merges without primary-key semantics; rejected for now.
- Branch metadata in SQLite: refs as files match the commit-JSON-as-truth design and survive index rebuilds.

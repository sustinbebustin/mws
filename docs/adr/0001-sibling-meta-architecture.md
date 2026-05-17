# Sibling-meta architecture

**Status:** Superseded by [0004 -- Meta-at-root architecture](./0004-meta-at-root-architecture.md).

The **Meta workspace** lives as a peer directory of **Working copies** (e.g., `my-project-meta/` alongside `my-project/` and `my-project-bug-fix/`), not inside any one of them. Working copies symlink every top-level entry of the meta (discovered dynamically at init/clone time, excluding `.git/`) into themselves. This makes peer working copies fully symmetric -- any one can be deleted without affecting the others -- and edits to meta files propagate instantly across peers because they're the same files. Files created directly in a working copy stay local until explicitly promoted via `mws promote <path>`.

## Considered options

- **Meta-at-root with `.git/info/exclude` hiding nested native repos.** Initially chosen for the single-working-copy case. Rejected when the parallel-working-copy use case surfaced: each `cp -r` copy would have an isolated meta, requiring git push/pull ceremony to sync.
- **Meta-at-root with a "primary" copy and parallels symlinking back.** Asymmetric -- deleting or moving the primary breaks all parallels. Rejected as fragile.
- **Git worktrees.** User dispreferred per stated preference and prior experience.
- **CoW filesystem snapshots (`cp --reflink=auto`).** Requires btrfs/xfs-with-reflink/bcachefs/zfs; user is on ext4. Even if available, doesn't solve the sync problem.

## Consequences

- A **Working copy** is not a git repo at its top level. `git status` run there shows nothing. Meta state is committed from `cd <name>-meta`.
- Concurrent edits to the same meta file across peers race (last-write-wins). In practice peers work on different scopes; rare in observation.
- Tools (Claude Code, Cursor, ripgrep, fd) follow symlinks transparently for the meta dirs. No indexing or path issues observed.

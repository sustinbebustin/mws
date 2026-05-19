# Configurable working-copies subdirectory

Allow a **Meta workspace** to declare a single subdirectory under which `mws clone` (and `mws init`'s first **Working copy**) creates working copies. The mechanism is a top-level `working_copies_dir` field in the **mws config**; default empty preserves today's flat-at-meta-root layout.

```toml
project_name        = "my-project"
working_copies_dir  = "copies"   # optional, single path segment
```

With this set, `mws clone peer` creates `<meta>/copies/peer/` instead of `<meta>/peer/`. Every other command (`mws list`, `mws rm`, `mws relink`, `mws addrepo`, `mws sync-env`, `mws stage-env`, `mws promote`) honors the same root.

## Why

A meta workspace that accumulates several peers (`main/`, `feature-x/`, `bug-fix/`, `experiment-y/`, ...) clutters the meta root, where it shares space with the harness and config (`.mws/`, `.mws.toml`, `.envs/`, `.gitignore`). Grouping peers under one subdirectory keeps the meta root scannable at a glance and signals "everything below this point is a peer."

The change is purely organizational. Allowlist gitignore semantics, harness fan-out, and the `mws` command surface are unchanged.

## Considered options

- **Per-invocation flag (`mws clone --at copies/peer`).** Rejected. Peer layout is a workspace property, not a per-clone decision -- if `main/` is at `copies/main/`, every future peer must be too, or `mws list` and the rm guard get inconsistent. A persistent config field makes the choice once and applies it everywhere.
- **Arbitrary filesystem paths (`working_copies_dir = "../../shared/peers"`).** Rejected. The discovery model used by every command walks up from cwd to find the meta root and then scans the copies root for peers; allowing arbitrary paths would force every command to thread an explicit copies-root flag and would break the rm guard (`rm` refuses anything not under the meta). Keeping the value to a single path segment under the meta root keeps the model intact.
- **Multiple path segments (`working_copies_dir = "team/peers"`).** Rejected as speculative. No use case asks for it, and it complicates the validator (path-safe segment, no `..`, no leading `/`) without a payoff. Easy to extend later if a real need emerges.
- **Per-peer override (`mws clone --to <path>`).** Rejected. Same problem as the per-invocation flag, with the added downside that a single workspace would end up with a mix of layouts.
- **Migrate existing peers automatically on config change.** Rejected. Moving working copies is destructive (open editors, running dev servers, native repo state). The config change is a forward-looking default; relocating existing copies is left to the user, documented in [`docs/config.md`](../config.md).
- **Auto-migrate via `mws migrate`.** Out of scope. `mws migrate` already converts the legacy sibling-meta layout to meta-at-root; bolting on a second migration mode at the same entrypoint would muddy that command. A migrated workspace can set `working_copies_dir` later and move its peers by hand.

## Consequences

- The default behavior (empty `working_copies_dir`) is identical to pre-change behavior. Existing workspaces keep working with no edits.
- `working_copies_dir` is validated by the same `project.ValidateName` rule used for project and peer names: a single path-safe segment (letters, digits, `-`, `_`, `.`; no leading `.` or `-`; no `/`). The dot-prefix ban means `.mws`, `.envs`, `.git` can never be picked as the copies root.
- `Workspace.WorkingCopiesDir` is loaded best-effort during `project.Locate`. A malformed `.mws.toml` does not crash `Locate`; the next command that legitimately needs the config (`mws clone`, `mws addrepo`, ...) surfaces the real parse error. Validation of the value itself happens lazily in commands that consume it.
- Working copies remain inside the meta workspace. The rm guard still refuses anything not enumerated under the copies root, and the meta's allowlist `.gitignore` continues to ignore everything except `.mws.toml`, `.mws/`, `.gitignore`, and `README.md` -- the new `copies/` subdir is still untracked by the meta's git.
- The harness symlinks created in a nested working copy use relative paths back to `<meta>/.mws/` (computed by `filepath.Rel`), so deeper nesting "just works" -- a peer at `<meta>/copies/peer/` links `CLAUDE.md -> ../../.mws/CLAUDE.md`.
- `mws migrate` (legacy sibling-meta -> meta-at-root) keeps placing migrated peers at the meta root. A migrated user can adopt `working_copies_dir` after the fact, with the documented caveat that existing peers don't relocate themselves.

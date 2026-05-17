# Meta-at-root architecture

The **Meta workspace** is a single git repository whose root holds the **Harness dir** (`.mws/`), the **mws config** (`.mws.toml`), and **Working copies** as untracked children. Working copies live inside the meta workspace, not as siblings. The meta's `.gitignore` is an allowlist: ignore everything, then explicitly un-ignore `.gitignore`, `.mws.toml`, and `.mws/`. Anything else at the meta root -- working copies, env staging, scratch files -- is invisible to git by construction.

The **Harness dir** is a pure fan-out source: every top-level entry in `.mws/` is symlinked into every working copy with no exclusions. The **mws config** lives at the meta root rather than inside `.mws/` so the rule "everything in `.mws/` is harness" has no exceptions.

`mws` discovers the meta workspace by walking up from cwd looking for `.mws.toml`. Working copies do not carry a `.mws` symlink back to the meta -- walk-up handles discovery. Harness symlinks (`.claude/`, `.workspace/`, `CLAUDE.md`, etc.) DO live in each working copy because Claude Code, ripgrep, editors, and similar tools read those paths from cwd and do not walk up.

## Considered options

- **Sibling-meta architecture (ADR 0001).** Original design: meta workspace as a sibling directory of working copies. Superseded because nesting working copies inside the meta combined with an allowlist gitignore is symmetric, drops the doubled `-meta` suffix on dir names, and makes `cd <project>` land in a useful place (lists working copies). The original objection to meta-at-root -- `cp -r` produces isolated metas -- no longer applies: working copies are not `cp -r` of the meta. They are independent children created by `mws clone`, and the meta's `.git/` stays at the root, unduplicated.
- **Config inside `.mws/` with symlink-fan-out exclusions.** Keep `.mws/config.toml` and exclude it (plus env staging) from the fan-out via a hard-coded list. Rejected: every exclusion is a hidden coupling between the symlinker and the harness contents. Splitting config out makes the rule "everything in `.mws/` fans out, no exceptions" honest, and surfaces the mws config at the meta root where you would `ls` for it anyway.
- **Blocklist gitignore instead of allowlist.** Hard-code working-copy names or have `mws clone` maintain the ignore list. Rejected: working-copy names are arbitrary, so any blocklist is either incomplete or requires automation that fails silently when bypassed. The allowlist is safe by construction at the cost of one extra `!/...` line per new tracked root entry.
- **Keep the `-meta` suffix on the meta workspace dir.** No-op given meta-at-root: the suffix existed to disambiguate sibling dirs, which no longer applies. Dropped.

## Consequences

- `git status` from the meta root works; from inside a working copy, it shows nothing meta-related (working copies are not git repos at their top level). Native repos still respond to git operations from inside their respective directories.
- The `-meta` suffix convention is dropped. The meta workspace dir name is conventionally the project name (e.g. `<project>/`) but not enforced; `mws.toml`'s `project_name` is authoritative.
- Adding a new tracked root-level file requires updating the allowlist `.gitignore`. Forgetting this results in `git status` silently ignoring the file -- not catastrophic but easy to miss.
- `cd <project>/<copy>` is the path you'll spend almost all your time in. `cd <project>` is for meta-level work (git operations on the harness, editing the mws config, browsing env staging).
- `mws migrate` must convert existing sibling-meta workspaces (ADR 0001) to this shape: move `.git/` up, restructure `.mws/`, retarget symlinks, rewrite the gitignore as an allowlist. The conversion is non-reversible without restoring from a backup.

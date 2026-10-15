# mws notes live in a meta-root CLAUDE.md

The skeleton renders two `CLAUDE.md` files. `<meta>/CLAUDE.md` describes the meta workspace itself: the meta / harness / working-copy layout, where to commit what, and the `mws` commands for clone, promote and env staging. `<meta>/.mws/CLAUDE.md` holds project content only -- description, conventions, agent-skill config -- and is symlinked into every working copy as before. The meta-root allowlist `.gitignore` gains `!/CLAUDE.md` so the new file is tracked.

## Why

Mixing the two meant every project's own `CLAUDE.md` opened with mws boilerplate that the user then edited around. Claude Code loads `CLAUDE.md` from every ancestor of its cwd, so a file at the meta root reaches every working copy and worktree without a symlink, and stays out of the project's harness content. This is the one harness path that relies on walk-up; ADR-0004's point that tools read from cwd still holds for `.claude/`, `.workspace/` and the project `CLAUDE.md`.

## Consequences

- Agents launched from a working copy see both files: mws notes first, then the project's.
- Meta workspaces created before this change keep their old single `CLAUDE.md`; `mws migrate` adds `!/CLAUDE.md` to the allowlist it writes but does not render the new file.

# mws

A Go CLI (`mws`) that manages "meta workspaces" -- a directory layout for spinning up an AI-harness layer around one or more native git repos, with cheap parallel working copies that share the harness via symlinks.

## Language

**Meta workspace**:
A git-versioned directory holding the AI harness (`.claude/`, `.workspace/`), project docs (`CLAUDE.md`, `justfile`), and `.mws/config.toml`. Named `<project>-meta/`.
_Avoid_: meta repo (overloaded), workspace (generic).

**Working copy**:
A non-git directory containing native repo clones and symlinks back to a **Meta workspace**. Where you actually edit code and run agents. Every top-level entry in the meta is symlinked into the working copy (discovered dynamically at init/clone time).
_Avoid_: project root, checkout.

**Promote**:
Moving a file or directory created in a **Working copy** into the **Meta workspace** and replacing it with a symlink, so the item becomes shared across peers and versioned by the meta's git. The `mws promote <path>` subcommand does this and backfills the symlink into every existing peer.
_Avoid_: share, sync (overloaded), publish.

**Native repo**:
An independent git repository (e.g., `frontend/`, `backend/`) cloned inside a **Working copy**. Has its own `.git/`; never a submodule, never sharing storage with peers in a way that affects refs/index.
_Avoid_: subrepo, nested repo (implies submodule), child repo.

**Peer working copies**:
Multiple **Working copies** sitting side-by-side on disk, all symlinking into the same **Meta workspace**. Created via `mws clone` for parallel work.
_Avoid_: duplicates, branches (overloaded with git).

**Skeleton**:
The template content embedded in the `mws` binary via `//go:embed`. Rendered into a fresh **Meta workspace** on `mws init`.
_Avoid_: template (overloaded with go templates and GitHub template repos), boilerplate.

**mws config**:
`.mws/config.toml` in the **Meta workspace**. Holds project name, description, and the list of **Native repos** (folder + URL). Symlinked into each **Working copy** as `.mws/`.
_Avoid_: manifest, project file.

## Relationships

- A **Meta workspace** is referenced by zero or more **Working copies** via symlinks.
- A **Working copy** symlinks every top-level entry of its sibling **Meta workspace** (discovered dynamically at init/clone time, excluding `.git/`). New entries in the meta are picked up by subsequent `mws clone` runs.
- A **Working copy** contains zero or more **Native repos**, each an independent git clone.
- **Peer working copies** symlink to the same **Meta workspace** but each has its own **Native repo** clones.
- The **mws config** is owned by the **Meta workspace** and reachable through any **Working copy**'s `.mws/` symlink.

## Example dialogue

> **User:** "Can I run a refactor in `my-project/` while a bug fix is happening in `my-project-bug-fix/`?"
> **Me:** "Yes -- they're peer working copies sharing one meta workspace. The native repos (`frontend`, `backend`) are independent clones in each copy, so the agents don't conflict on git ops."
> **User:** "What about journal entries in `.workspace/journal/`?"
> **Me:** "Those write to the meta workspace through the symlink, so both copies see the same files. Last-write-wins on simultaneous edits, but in practice peers work on different scopes."
> **User:** "And `cd my-project && git status`?"
> **Me:** "Working copies aren't git repos at the top level. `cd ../my-project-meta && git status` shows meta state. The native repos still respond to `git status` from inside `my-project/frontend/`."
> **User:** "What if I create `RUNBOOK.md` directly in `my-project/`?"
> **Me:** "It lives only in that working copy until you `mws promote RUNBOOK.md`, which moves it into the meta and replaces it with a symlink. After promotion it's visible to every peer."

## Flagged ambiguities

- "meta repo" was used loosely early in the design conversation; resolved as **Meta workspace** to distinguish it from this CLI's own source repository.
- "clone" is overloaded: a git operation vs. a working copy as a unit. Resolved: we say **Working copy** for the directory and reserve "clone" for git operations (or the `mws clone` subcommand, which creates a working copy via git clone operations).
- "template" was used early for both the GitHub-template-repo distribution model and the embedded **Skeleton**. Resolved: we use **Skeleton** for the embedded content; "template" only appears in references to Go's `text/template` or external GitHub template repos.

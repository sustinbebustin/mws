# mws

A Go CLI (`mws`) that manages "meta workspaces" -- a directory layout for spinning up an AI-harness layer around one or more native git repos, with cheap parallel working copies that share the harness via symlinks.

## Language

**Meta workspace**:
A git-versioned directory containing the **Harness dir**, the **mws config**, and (as untracked children) any **Working copies**. Named after the project (e.g. `my-project/`). Its working tree is allowlist-gitignored so only the harness and config are tracked; working copies are intentionally invisible to its git.
_Avoid_: meta repo (overloaded), workspace (generic), meta dir.

**Harness dir**:
The fan-out source inside a **Meta workspace**, at `.mws/`. Every top-level entry here is symlinked into every **Working copy** -- no exclusions. Holds the AI harness (`.claude/`, `.workspace/`), shared project docs (`CLAUDE.md`, `justfile`), and anything else that should appear in every copy.
_Avoid_: shared dir, mws dir (collides with the CLI name).

**Working copy**:
A non-git directory living inside its **Meta workspace**, containing **Native repo** clones and symlinks to every entry in the meta's **Harness dir**. Where you actually edit code and run agents.
_Avoid_: project root, checkout.

**Promote**:
Moving a file or directory created in a **Working copy** into the meta's **Harness dir** and replacing it with a symlink, so the item becomes shared across peers and versioned by the meta's git. The `mws promote <path>` subcommand does this and backfills the symlink into every existing peer.
_Avoid_: share, sync (overloaded), publish.

**Native repo**:
An independent git repository (e.g., `frontend/`, `backend/`) cloned inside a **Working copy**. Has its own `.git/`; never a submodule, never sharing storage with peers in a way that affects refs/index.
_Avoid_: subrepo, nested repo (implies submodule), child repo.

**Peer working copies**:
Multiple **Working copies** living side-by-side inside the same **Meta workspace**, all symlinking the same **Harness dir**. Created via `mws clone` for parallel work.
_Avoid_: duplicates, branches (overloaded with git).

**Skeleton**:
The template content embedded in the `mws` binary via `//go:embed`. Rendered into a fresh **Meta workspace** on `mws init`.
_Avoid_: template (overloaded with go templates and GitHub template repos), boilerplate.

**mws config**:
A single TOML file at the root of the **Meta workspace** (e.g. `.mws.toml`). Holds project name, description, the list of **Native repos** (folder + URL), and per-repo **Env staging** mappings. Discovered by walking up from cwd. Not symlinked into working copies.
_Avoid_: manifest, project file.

**Env staging**:
A dedicated directory inside the **Meta workspace** (`.envs/`, organized per **Native repo**) holding canonical env files. Per-repo entries in the **mws config** map staged files to target paths inside each clone's native repo. Files are **copied** into a **Working copy** on `mws clone` (not symlinked), so peers can diverge freely (different ports, different external service keys, etc.). `mws sync-env [clone]` re-pushes from staging when you want to reset a clone to defaults. Not tracked by the meta's git.
_Avoid_: env vault (sounds like secrets management), env dir (ambiguous).

**Setup commands**:
Per-**Native repo** shell commands listed in the **mws config** under `[[repos.setup]]`. Executed at the end of `mws clone` (after clone + env-copy) via `sh -c`, with cwd set to the repo's directory in the new **Working copy**, so a fresh peer can be made ready-to-use in one step. Opt-in: the user is prompted before execution and can override the prompt with `--setup` / `--no-setup`.
_Avoid_: hooks (implies a broader lifecycle that does not exist), scripts (overloaded with package.json's name-keyed semantics).

## Relationships

- A **Meta workspace** is a git repository at its root, containing the **Harness dir** and **mws config** as tracked content, and zero or more **Working copies** as untracked children.
- A **Working copy** symlinks every top-level entry of its parent meta's **Harness dir**. New entries in the harness are picked up by subsequent `mws clone` runs (and `mws relink` for existing copies).
- A **Working copy** contains zero or more **Native repos**, each an independent git clone.
- **Peer working copies** share one **Harness dir** but each has its own **Native repo** clones.
- The **mws config** is reachable from any **Working copy** by walking up to the **Meta workspace** root.
- **Env staging** is owned by the **Meta workspace** and copied into a **Working copy**'s **Native repos** on `mws clone` per the mappings in the **mws config**. Env files in a working copy are independent of staging after the copy -- edits do not flow back.
- **Setup commands** are owned per **Native repo** in the **mws config** and executed by `mws clone` against the new **Working copy** after clone and env-copy complete. They are the last step of clone; intra-repo commands are sequential and stop at the first failure, but a failure in one repo does not block other repos.

## Example dialogue

> **User:** "Can I run a refactor in `my-project/main/` while a bug fix is happening in `my-project/bug-fix/`?"
> **Me:** "Yes -- they're peer working copies inside the same meta workspace. The native repos (`frontend`, `backend`) are independent clones in each copy, so the agents don't conflict on git ops."
> **User:** "What about journal entries in `.workspace/journal/`?"
> **Me:** "Those write through the symlink into the harness dir, so both copies see the same files. Last-write-wins on simultaneous edits, but in practice peers work on different scopes."
> **User:** "And `cd my-project/main && git status`?"
> **Me:** "Working copies aren't git repos at the top level. `cd my-project && git status` shows the meta's git state. The native repos still respond to `git status` from inside `my-project/main/frontend/`."
> **User:** "What if I create `RUNBOOK.md` directly in `my-project/main/`?"
> **Me:** "It lives only in that working copy until you `mws promote RUNBOOK.md`, which moves it into the harness dir and replaces it with a symlink. After promotion it's visible to every peer."

## Flagged ambiguities

- "meta repo" was used loosely early in the design conversation; resolved as **Meta workspace**. The meta workspace *is* a git repo at its root, but the term **Meta workspace** captures the broader role (container for harness, config, and working copies) and avoids confusion with this CLI's own source repository.
- "clone" is overloaded: a git operation vs. a working copy as a unit. Resolved: we say **Working copy** for the directory and reserve "clone" for git operations (or the `mws clone` subcommand, which creates a working copy via git clone operations).
- "template" was used early for both the GitHub-template-repo distribution model and the embedded **Skeleton**. Resolved: we use **Skeleton** for the embedded content; "template" only appears in references to Go's `text/template` or external GitHub template repos.
- "meta dir" was used loosely for both the **Meta workspace** as a whole and `.mws/`. Resolved: the workspace is the **Meta workspace**; the inner `.mws/` is the **Harness dir**.
- The path `.mws/` shares a name with the `mws` CLI. Resolved by separating the concept (**Harness dir**) from the path; the CLI's own state lives in the **mws config** file at the meta root, not in `.mws/`.

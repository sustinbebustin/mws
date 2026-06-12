# Configuration: `.mws.toml`

The mws config lives at the **Meta workspace** root in a single file, `.mws.toml`. It is the source of truth for the project name, the list of **Native repos**, per-repo **Env staging** mappings, and per-repo **Setup commands**. Discovered by walking up from cwd, so any subcommand works from inside a working copy or a native repo.

For the domain vocabulary, see [CONTEXT.md](../CONTEXT.md). For the design decisions behind each section, see the ADRs in [adr/](./adr/).

## Schema

```toml
project_name        = "<string>"  # required; used in skeleton-rendered docs
description         = "<string>"  # optional; free-form
working_copies_dir  = "<segment>" # optional; single path segment under the meta root

[[repos]]                         # zero or more
  folder = "<relative dir>"       # required; folder name inside a working copy
  url    = "<git URL>"            # required; remote used by `mws clone` fallback

  [[repos.envs]]                  # optional; zero or more per repo
    source = "<flat name>"        # file under .envs/<folder>/<source>
    target = "<repo-relative>"    # destination inside the cloned repo

  [[repos.setup]]                 # optional; zero or more per repo
    cmd = "<shell string>"        # passed to `sh -c`, cwd = the cloned repo

[[optional_repos]]                # zero or more; NOT cloned into every copy
  folder = "<relative dir>"       # required; unique across repos + optional_repos
  url    = "<git URL>"            # required
  # supports the same [[optional_repos.envs]] and [[optional_repos.setup]] tables
```

| Top-level key        | Type              | Required | Notes |
|----------------------|-------------------|----------|-------|
| `project_name`       | string            | yes      | Used by `mws init` skeleton templates and surfaced in `mws list`. |
| `description`        | string            | no       | Free-form. Empty by default. |
| `working_copies_dir` | string            | no       | Single path-safe segment validated by the same rules as `project_name` (letters, digits, `-`, `_`, `.`; no leading `.` or `-`; no `/`). When set, `mws clone` and `mws init` place working copies at `<meta>/<working_copies_dir>/<name>/` instead of `<meta>/<name>/`. Empty (the default) keeps working copies at the meta root. |
| `repos`              | array of tables   | no       | Each entry adds one **Native repo** to every **Working copy**. |
| `optional_repos`     | array of tables   | no       | On-demand **Native repos** that are *not* cloned into every copy. Pulled into a specific copy via the `mws clone` prompt, `mws clone --with <folder>`, or `mws include <folder>`. Same shape as `[[repos]]`. |

### `[[repos]]`

| Key      | Type                | Required | Notes |
|----------|---------------------|----------|-------|
| `folder` | string              | yes      | Unique within the config. Determines the clone target: `<working-copy>/<folder>/`. |
| `url`    | string              | yes      | Used by `git clone` when no peer working copy is available. `mws clone` prefers `git clone --local` from an existing peer's clone of the same repo and falls back to this URL. |
| `envs`   | array of tables     | no       | See [`[[repos.envs]]`](#reposenvs). Omitted when empty. |
| `setup`  | array of tables     | no       | See [`[[repos.setup]]`](#repossetup). Omitted when empty. |

### `[[repos.envs]]`

Maps a staged env file in `.envs/<folder>/` to a target inside the cloned repo. Materialised on `mws clone` (copy, not symlink) and re-pushed by `mws sync-env <copy>`. Captured into staging by `mws stage-env <copy>`.

| Key      | Type   | Required | Notes |
|----------|--------|----------|-------|
| `source` | string | yes      | Flat filename. Looked up at `<meta>/.envs/<folder>/<source>`. |
| `target` | string | yes      | Path inside the repo working tree (relative to `<working-copy>/<folder>/`). Parent dirs are created on copy. |

Design rationale and trade-offs: [ADR-0005](./adr/0005-env-staging-via-copy.md).

### `[[repos.setup]]`

Shell commands run by `mws clone` against the cloned repo as the last step (after env copy succeeds for every repo). Each command runs via `sh -c <cmd>` with `cwd = <working-copy>/<folder>` and the user's `PATH` and environment inherited unchanged.

| Key   | Type   | Required | Notes |
|-------|--------|----------|-------|
| `cmd` | string | yes      | Shell string. Pipes, env-var expansion, `&&` chains all work. |

Behavior:

- Within one repo: commands run in array order and stop at the first non-zero exit.
- Across repos: a failure in one repo does not block another. Each repo's setup runs independently.
- The user is prompted once with a grouped list of every command before any run. Override with `--setup` (run without prompting) or `--no-setup` (skip entirely). The two flags are mutually exclusive.
- Child stdout and stderr stream live to the terminal; long-running `pnpm install` / `go build` do not appear hung.
- If any setup command failed, `mws clone` exits non-zero with a summary of `<folder>: <cmd>` failures. The working copy and its clones still exist; the user can read the streamed output and re-run the failing command by hand.
- If the clone or env-copy phase failed for any repo, setup is skipped entirely.

Design rationale: [ADR-0006](./adr/0006-post-clone-setup-commands.md).

### `[[optional_repos]]`

An **Optional repo** registered for on-demand inclusion. It is *not* cloned by `mws clone` into every working copy; instead it is pulled into a specific copy when asked for:

- During `mws clone`, when any `[[optional_repos]]` exist, a multiselect prompt offers them (defaulting to none). `mws clone --with <folder>` (repeatable) selects specific ones non-interactively and skips the prompt.
- `mws include <folder> [working-copy]` clones one into an existing copy (the current copy by default).

Each entry uses the **same keys** as `[[repos]]` (`folder`, `url`, plus optional `[[optional_repos.envs]]` and `[[optional_repos.setup]]` tables). `folder` must be unique across both `[[repos]]` and `[[optional_repos]]` -- it maps to a single clone-target directory. Register entries with `mws add-repo --optional <url> [folder]`, which appends here without cloning into existing copies.

Design rationale: [ADR-0009](./adr/0009-optional-repos.md).

## Worked example

```toml
project_name = "pitch-app"
description  = ""

[[repos]]
  folder = "backend"
  url    = "git@github.com:lgcypower/lgcy-proposals.git"

  [[repos.setup]]
    cmd = "go build ./..."

[[repos]]
  folder = "frontend"
  url    = "git@github.com:lgcypower/pitch-proposal-room.git"

  [[repos.envs]]
    source = "internal.env"
    target = "apps/internal/.env"
  [[repos.envs]]
    source = "public.env"
    target = "apps/public/.env"
  [[repos.envs]]
    source = "supabase.env"
    target = "supabase/.env"

  [[repos.setup]]
    cmd = "pnpm install --frozen-lockfile"
  [[repos.setup]]
    cmd = "pnpm build"

[[optional_repos]]
  folder = "worker"
  url    = "git@github.com:lgcypower/lgcy-worker.git"
```

What `mws clone peer` does with this config:

1. Creates `<meta>/peer/` and symlinks every entry of `<meta>/.mws/` into it.
2. Clones `backend` and `frontend` into `peer/` from their configured URLs.
3. Copies the three frontend env files from `<meta>/.envs/frontend/*` into `peer/frontend/apps/...` and `peer/frontend/supabase/`.
4. Offers `worker` in an "Include optional repos?" prompt (none selected by default). `mws clone peer --with worker` would clone it without prompting; `mws include worker peer` would add it to an existing `peer/`.
5. Prompts: "Run setup commands?" listing all three commands. On confirm: runs `go build ./...` in `peer/backend/`, runs `pnpm install --frozen-lockfile` then `pnpm build` in `peer/frontend/`.

## Editing the config

Most fields are written by `mws` subcommands:

- `mws init <name>` writes the initial file with `project_name` and an empty `[[repos]]` list.
- `mws add-repo` appends a `[[repos]]` entry interactively (and clones it into every working copy).
- `mws add-repo --optional` appends an `[[optional_repos]]` entry without cloning it anywhere; pull it into a copy later with `mws clone --with` or `mws include`.

`[[repos.envs]]` and `[[repos.setup]]` are hand-edited today; there are no dedicated subcommands. The schema is round-trip-safe: `mws` reads and writes the file using `BurntSushi/toml`, preserving every key it recognises. Unknown keys are ignored on parse but dropped on the next write -- avoid adding fields that mws does not understand.

Changing `working_copies_dir` after the fact does **not** relocate any existing working copies. The new value only affects subsequent `mws clone` (and `mws init`'s first copy on a fresh workspace). Move existing peers by hand if you want them under the new prefix, then run `mws relink` from the meta root.

The file is tracked by the meta workspace's git. Commit changes alongside the harness updates that motivate them.

## Related

- [CONTEXT.md](../CONTEXT.md) - domain language (Meta workspace, Working copy, Native repo, Env staging, Setup commands)
- [ADR-0003](./adr/0003-independent-native-repos.md) - why native repos are independent clones, not submodules
- [ADR-0004](./adr/0004-meta-at-root-architecture.md) - why the meta workspace sits at the project root
- [ADR-0005](./adr/0005-env-staging-via-copy.md) - env staging design
- [ADR-0006](./adr/0006-post-clone-setup-commands.md) - setup commands design
- [ADR-0007](./adr/0007-working-copies-subdir.md) - configurable working-copies subdirectory
- [ADR-0009](./adr/0009-optional-repos.md) - optional repos pulled into copies on demand

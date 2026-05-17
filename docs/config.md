# Configuration: `.mws.toml`

The mws config lives at the **Meta workspace** root in a single file, `.mws.toml`. It is the source of truth for the project name, the list of **Native repos**, per-repo **Env staging** mappings, and per-repo **Setup commands**. Discovered by walking up from cwd, so any subcommand works from inside a working copy or a native repo.

For the domain vocabulary, see [CONTEXT.md](../CONTEXT.md). For the design decisions behind each section, see the ADRs in [adr/](./adr/).

## Schema

```toml
project_name = "<string>"        # required; used in skeleton-rendered docs
description  = "<string>"        # optional; free-form

[[repos]]                        # zero or more
  folder = "<relative dir>"      # required; folder name inside a working copy
  url    = "<git URL>"           # required; remote used by `mws clone` fallback

  [[repos.envs]]                 # optional; zero or more per repo
    source = "<flat name>"       # file under .envs/<folder>/<source>
    target = "<repo-relative>"   # destination inside the cloned repo

  [[repos.setup]]                # optional; zero or more per repo
    cmd = "<shell string>"       # passed to `sh -c`, cwd = the cloned repo
```

| Top-level key  | Type              | Required | Notes |
|----------------|-------------------|----------|-------|
| `project_name` | string            | yes      | Used by `mws init` skeleton templates and surfaced in `mws list`. |
| `description`  | string            | no       | Free-form. Empty by default. |
| `repos`        | array of tables   | no       | Each entry adds one **Native repo** to every **Working copy**. |

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
```

What `mws clone peer` does with this config:

1. Creates `<meta>/peer/` and symlinks every entry of `<meta>/.mws/` into it.
2. Clones `backend` and `frontend` into `peer/` (preferring `git clone --local` from an existing peer when available).
3. Copies the three frontend env files from `<meta>/.envs/frontend/*` into `peer/frontend/apps/...` and `peer/frontend/supabase/`.
4. Prompts: "Run setup commands?" listing all three commands. On confirm: runs `go build ./...` in `peer/backend/`, runs `pnpm install --frozen-lockfile` then `pnpm build` in `peer/frontend/`.

## Editing the config

Most fields are written by `mws` subcommands:

- `mws init <name>` writes the initial file with `project_name` and an empty `[[repos]]` list.
- `mws addrepo` appends a `[[repos]]` entry interactively.

`[[repos.envs]]` and `[[repos.setup]]` are hand-edited today; there are no dedicated subcommands. The schema is round-trip-safe: `mws` reads and writes the file using `BurntSushi/toml`, preserving every key it recognises. Unknown keys are ignored on parse but dropped on the next write -- avoid adding fields that mws does not understand.

The file is tracked by the meta workspace's git. Commit changes alongside the harness updates that motivate them.

## Related

- [CONTEXT.md](../CONTEXT.md) - domain language (Meta workspace, Working copy, Native repo, Env staging, Setup commands)
- [ADR-0003](./adr/0003-independent-native-repos.md) - why native repos are independent clones, not submodules
- [ADR-0004](./adr/0004-meta-at-root-architecture.md) - why the meta workspace sits at the project root
- [ADR-0005](./adr/0005-env-staging-via-copy.md) - env staging design
- [ADR-0006](./adr/0006-post-clone-setup-commands.md) - setup commands design

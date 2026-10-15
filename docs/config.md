# Configuration: `.mws.toml`

The mws config lives at the **Meta workspace** root in a single file, `.mws.toml`. It is the source of truth for the project name, the list of **Native repos**, per-repo **Env staging** mappings, and per-repo **Setup commands**. Discovered by walking up from cwd, so any subcommand works from inside a working copy or a native repo.

For the domain vocabulary, see [GLOSSARY.md](../GLOSSARY.md). For the design decisions behind each section, see the ADRs in [adr/](./adr/).

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

[trash]                           # optional; whole table may be omitted
  retention_days = 7              # days a trashed copy is kept; 0 = forever
  disabled       = false          # true = `mws rm` deletes outright

[worktrees]                       # optional; whole table may be omitted
  dir           = ".wt"           # one segment, leading "."; holds <copy>/<dir>/<name>/
  branch_prefix = "wt/"           # default branch is <prefix><name>
  repo_setup    = true            # run [[repos.setup]] in each inner repo on create

  [[worktrees.setup]]             # optional; zero or more
    cmd = "<shell string>"        # passed to `sh -c`, cwd = the worktree root

[ports]                           # optional; whole table may be omitted
  stride = 10                     # slot k moves every port by k * stride
  slots  = 50                     # optional cap on the highest claimable slot

  [[ports.port]]                  # one or more when [ports] is present
    name = "<identifier>"         # exported as MWS_PORT_<NAME>
    base = 54321                  # port at slot 0 (the default ports); 1..65535
    var  = "<identifier>"         # optional alias exported alongside

[vars]                            # optional; derived strings
  <NAME> = "<string with ${PORT_VAR} or ${MWS_BUILTIN}>"

[[renders]]                       # optional; zero or more
  source = "<path>.render"        # under <meta>/.mws/
  target = "<file name>"          # at the working copy root

[[setup]]                         # optional; zero or more
  cmd = "<shell string>"          # passed to `sh -c`, cwd = the working copy root
```

| Top-level key        | Type              | Required | Notes |
|----------------------|-------------------|----------|-------|
| `project_name`       | string            | yes      | Used by `mws init` skeleton templates and surfaced in `mws list`. |
| `description`        | string            | no       | Free-form. Empty by default. |
| `working_copies_dir` | string            | no       | Single path-safe segment validated by the same rules as `project_name` (letters, digits, `-`, `_`, `.`; no leading `.` or `-`; no `/`). When set, `mws clone` and `mws init` place working copies at `<meta>/<working_copies_dir>/<name>/` instead of `<meta>/<name>/`. Empty (the default) keeps working copies at the meta root. |
| `repos`              | array of tables   | no       | Each entry adds one **Native repo** to every **Working copy**. |
| `optional_repos`     | array of tables   | no       | On-demand **Native repos** that are *not* cloned into every copy. Pulled into a specific copy via the `mws clone` prompt, `mws clone --with <folder>`, or `mws include <folder>`. Same shape as `[[repos]]`. |
| `trash`              | table             | no       | Tunes the soft-delete behaviour of `mws rm`. See [`[trash]`](#trash). Omit the table for the defaults. |
| `worktrees`          | table             | no       | Tunes `mws worktree`. See [`[worktrees]`](#worktrees). Omit the table for the defaults. |
| `ports`              | table             | no       | Named ports each working copy shifts by its **Port slot**. See [`[ports]`](#ports). |
| `vars`               | table             | no       | Derived strings exported with the ports. See [`[vars]`](#vars). |
| `renders`            | array of tables   | no       | Harness files rendered per working copy. See [`[[renders]]`](#renders). |
| `setup`              | array of tables   | no       | Commands run at the working copy root after every render. See [`[[setup]]`](#setup). |

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
- The user is prompted once with a grouped list of every command before any run. Override with `--setup` (run without prompting) or `--no-setup` (skip entirely). The two flags are mutually exclusive. Without a terminal on stdin one of them is required, and `mws clone` fails before creating anything when neither is given.
- Child stdout and stderr stream live to the terminal (both on mws's stderr, so stdout keeps only the new copy's path); long-running `pnpm install` / `go build` do not appear hung.
- If any setup command failed, `mws clone` exits non-zero with a summary of `<folder>: <cmd>` failures. The working copy and its clones still exist; the user can read the streamed output and re-run the failing command by hand.
- If the clone or env-copy phase failed for any repo, setup is skipped entirely.

Design rationale: [ADR-0006](./adr/0006-post-clone-setup-commands.md).

### `[[optional_repos]]`

An **Optional repo** registered for on-demand inclusion. It is *not* cloned by `mws clone` into every working copy; instead it is pulled into a specific copy when asked for:

- During `mws clone`, when any `[[optional_repos]]` exist, a multiselect prompt offers them (defaulting to none). `mws clone --with <folder>` (repeatable) selects specific ones and `--no-optional` selects none, both without prompting; without a terminal one of them is required.
- `mws include <folder> [working-copy]` clones one into an existing copy (the current copy by default).

Each entry uses the **same keys** as `[[repos]]` (`folder`, `url`, plus optional `[[optional_repos.envs]]` and `[[optional_repos.setup]]` tables). `folder` must be unique across both `[[repos]]` and `[[optional_repos]]` -- it maps to a single clone-target directory. Register entries with `mws add-repo --optional <url> [folder]`, which appends here without cloning into existing copies.

Design rationale: [ADR-0009](./adr/0009-optional-repos.md).

### `[trash]`

`mws rm` does not delete a **Working copy**. It moves it into `<meta>/.trash/`, where `mws restore <name>` can bring it back and an automatic sweep purges it once it passes the retention window. Omit the table entirely for the defaults below.

| Key              | Type | Required | Notes |
|------------------|------|----------|-------|
| `retention_days` | int  | no       | How many days a trashed working copy is kept. Defaults to `7`. `0` means keep forever -- nothing is ever purged automatically. Negative values are rejected at load time. |
| `disabled`       | bool | no       | `true` makes `mws rm` delete a working copy outright instead of trashing it. Defaults to `false`. `mws restore` and `mws trash` still operate on anything trashed earlier. |

Behavior:

- Purging is **opportunistic**: the sweep runs when `mws rm`, `mws restore`, or a `mws trash` subcommand touches the trash. A trashed copy therefore outlives its retention for as long as you do not run `mws` in that workspace. Force a sweep with `mws trash prune`.
- Trashing and restoring are single renames, so they are instant and preserve file modes, symlinks, and git object files exactly. The copy is not compressed.
- Harness symlinks inside a working copy are relative, so they dangle while the copy sits in `.trash/` at a different depth. `mws restore` repairs them on the way out; a dangling `ls -l` inside `.trash/` is expected, not corruption.
- `.trash/` sits at the meta root next to `.mws/` and `.envs/`, regardless of `working_copies_dir`, and the meta-root allowlist `.gitignore` keeps it untracked.
- `mws rm --purge` skips the trash for a single removal without changing the config. `mws trash empty` purges everything regardless of age.

Design rationale: [ADR-0010](./adr/0010-soft-delete-working-copies.md).

### `[worktrees]`

`mws worktree` (alias `mws wt`) creates a **Worktree** of a **Working copy** at `<copy>/<dir>/<name>/`. Omit the table entirely for the defaults below.

| Key             | Type   | Required | Notes |
|-----------------|--------|----------|-------|
| `dir`           | string | no       | Directory inside each working copy that holds its worktrees. Defaults to `.wt`. Must be one path segment: a single leading `.` followed by letters, digits, `-`, `_` or `.`. The dot keeps it out of working-copy enumeration and default searches; an invalid value is rejected at load time. |
| `branch_prefix` | string | no       | Prepended to a worktree's name to form its branch in every native repo. Defaults to `wt/`. Only a branch named `<prefix><name>` is ever deleted by `rm`/`prune`; a `--branch` override is always kept. |
| `repo_setup`    | bool   | no       | `true` (the default) runs each repo's `[[repos.setup]]` commands inside its worktree on create. `false` skips only those; `--no-setup` skips them and `[[worktrees.setup]]` both. |

`[[worktrees.setup]]` entries (`cmd`, run via `sh -c` with cwd = the worktree root) run after the per-repo setup, for root-level steps such as `direnv allow`. Setup output streams to stderr. A file a setup command writes at the root counts as work there: `rm` refuses it without `--force` and `prune` keeps the worktree.

Commands (non-interactive; run from anywhere inside a working copy or one of its worktrees; results on stdout, progress on stderr):

| Command | What it does |
|---|---|
| `mws wt new <name> [--branch <b>] [--base <ref>] [--no-setup]` | Create the worktree and print its absolute path as the last stdout line. `--branch` creates or reuses `<b>` in every native repo instead of `<prefix><name>`; `--base` is the start point for new branches (default: HEAD of the launching checkout). An existing branch that no worktree has checked out is reused. Re-running for an existing worktree prints its path and exits 0. |
| `mws wt ls [--all]` | List worktrees (`--all`: of every working copy) with per-repo `branch@sha`, dirty state, commits reachable from no other ref, pids running inside, and age. Also reports `orphan` (registered, directory gone) and `unmanaged` (no marker, or registered outside the worktrees dir) entries. With none, stdout is empty and `no worktrees in <copy>` goes to stderr. |
| `mws wt path <name>` | Print the worktree's absolute path. |
| `mws wt exec <name> -- <cmd> [args...]` | Run `<cmd>` with cwd = the worktree root and stdio inherited; mws exits with the command's exit code (4 when the worktree is not found, 1 when the command cannot start). |
| `mws wt rm <name>... [--force] [--keep-branch] [--dry-run]` | Remove worktrees. Checks every name first; one refusal removes nothing. `--force` discards modified and untracked files, commits on a detached HEAD that no branch reaches, and root files mws did not create or that changed since creation; `--keep-branch` keeps every branch; `--dry-run` runs every check and lists what would be removed. |
| `mws wt prune [--dry-run]` | Repair stale registrations, run `git worktree prune` in the meta repo and every native repo, then remove every clean, idle, fully merged worktree (except the one it runs in, and any with a native repo on a detached HEAD). Reports what it kept and why. |
| `mws wt hook create\|remove\|subagent-stop` | Hidden adapters for Claude Code's `WorktreeCreate`, `WorktreeRemove` and `SubagentStop` hooks; they read the hook's JSON payload on stdin. `mws init` wires them into the skeleton's `.claude/settings.json`. `mws wt hook --help` documents each. |

Exit codes: `0` ok, `1` failure, `2` usage, `3` refused by a safety check (unmanaged directory, modified or untracked files, unreachable detached commits, changed root files, live process, branch checked out elsewhere, repo mid-merge or rebase), `4` no worktree by that name (`path`, `exec`; `rm` only when every name is gone, otherwise it skips the missing ones and lists them under `not_found`). `hook create` and `hook remove` exit 1 for every error so Claude fails closed; `hook subagent-stop` always exits 0.

`--json` (a flag of `mws worktree`) makes `new`, `path`, `ls`, `rm` and `prune` print one JSON object on stdout; `exec` and `hook` do not print JSON.

```text
new     {name, path, copy, branch, reused, repos:[{folder, path, branch, base?}]}
path    {name, path, copy}
ls      {worktrees:[{name, path, copy, state, detail?, repos:[{folder, branch, sha?, dirty, unique_commits}],
                     root_files:[...], dirty, live:[pid], created?, age_seconds?}]}   # state: managed | orphan | unmanaged
rm      {removed:[{name, path, branches:[{folder, branch, deleted, unique_commits, reason?}]}], not_found:[name]}
rm --dry-run  {dry_run: true, removed:[{name, path}], not_found:[name]}
prune   {dry_run, repaired:[...], dropped:[...], removed:[...as rm...], kept:[{name, path, reasons:[...]}]}
error   {error:{code, message}}                  # code: usage | refused | not_found | failure | setup_failed
```

`base` in `new` is a full commit id (the branch tip for a reused branch) and is omitted for a reused worktree. A setup failure under `--json` prints the full result object plus `error` (`setup_failed`) and exits 1.

Related behavior of the other commands: `mws list` appends `(+N worktrees)` to a copy; `mws rm <copy>` refuses while one of its worktrees has a live process and lists its worktrees in the confirmation; `mws restore` repairs the worktrees' git links; `mws relink` refreshes harness links in worktree roots; `mws trash prune|empty` prune stale meta worktree registrations; `mws sync-env` updates only the copy, never its worktrees.

Design rationale: [ADR-0011](./adr/0011-worktrees-are-children-of-a-working-copy.md).

### `[ports]`

Declares the named ports of the workspace. A **Working copy** starts on the default ports (slot 0), where each port is its `base`. Holding a **Port slot** `k >= 1`, it owns `base + k * stride` for every port, recorded in its **Port lease**, `<copy>/.mws-ports.env`. Omit the table and the workspace has no port slots: no lease, no lock, nothing changes.

| Key      | Type            | Required | Notes |
|----------|-----------------|----------|-------|
| `stride` | int             | no       | Distance between one slot and the next. Defaults to `10`; must be at least `1`. |
| `slots`  | int             | no       | Caps the highest claimable slot; at least `1`. Defaults to the largest slot whose ports all stay at or below 65535. |
| `port`   | array of tables | yes      | One or more `[[ports.port]]` entries, below. |

`[[ports.port]]`:

| Key    | Type   | Required | Notes |
|--------|--------|----------|-------|
| `name` | string | yes      | An identifier (letters, digits, `_`, not starting with a digit). Exported as `MWS_PORT_<NAME>`, upper-cased, so names must be unique ignoring case. |
| `base` | int    | yes      | The port on the default ports, in `1..65535`. |
| `var`  | string | no       | An identifier exported alongside `MWS_PORT_<NAME>` with the same value, for names a project already uses. Must not repeat another port's variable, alias or a builtin. |

Every lease also exports four builtins: `MWS_SLOT`, `MWS_COPY` (the copy's directory name), `MWS_COPY_SLUG` (lower-cased, every character outside `[a-z0-9-]` replaced by `-`) and `MWS_META` (absolute meta root).

Slot arithmetic is exact. Two ports whose bases are congruent modulo the stride meet at some pair of slots: bases `a < b` with `b - a = d * stride` mean slot `d` of the first is slot 0 of the second. Validation rejects such a pair unless the slot limit (`slots`, or the 65535 bound) stays below `d`. The highest claimable slot is the smaller of `slots` and the last slot whose ports all stay at or below 65535.

### `[vars]`

Derived strings exported with the ports, one line each for URLs, project ids and cookie names. Keys are identifiers that no port, alias or builtin already exports. A value may reference port variables (`${MWS_PORT_<NAME>}` or the alias) and the builtins as `${NAME}`, and nothing else: not another `[vars]` entry, since TOML tables are unordered. Any other `$` sequence (`$HOME`, `${a:-b}`) is literal text.

### `[[renders]]`

A harness file that needs per-copy values stays in the **Harness dir** under a `.render` name and is rendered to its target at the working copy root, with `${VAR}` placeholders substituted for the copy's slot. The source is still symlinked into every copy like any harness entry; the target is a regular per-copy file.

| Key      | Type   | Required | Notes |
|----------|--------|----------|-------|
| `source` | string | yes      | Path relative to `<meta>/.mws/`, ending in `.render`. |
| `target` | string | yes      | A single file name at the working copy root. Must not start with `.mws-` (reserved for mws markers) or repeat another entry's target. |

### `[[setup]]`

Root-level setup commands: each `cmd` runs via `sh -c` with cwd = the working copy root and the lease's variables in its environment, after every render, behind the same `--setup` / `--no-setup` choice as `[[repos.setup]]`. `direnv allow` is the motivating entry, since direnv refuses a changed `.envrc` until it is allowed again. `[[repos.setup]]` runs only when a repo is cloned, never on a render.

| Key   | Type   | Required | Notes |
|-------|--------|----------|-------|
| `cmd` | string | yes      | Shell string. |

### Port examples

app, eight ports and ten derived vars. The highest base, 54323, caps the slot limit at 1121. Three pairs (3000/54320, 3001/54321, 3002/54322) are congruent modulo 10, but they first meet at slot 5132, past that limit.

```toml
[ports]
  stride = 10

  [[ports.port]]
    name = "internal"
    base = 3000
    var  = "APP_INTERNAL_PORT"
  [[ports.port]]
    name = "public"
    base = 3001
    var  = "APP_PUBLIC_PORT"
  [[ports.port]]
    name = "hs"
    base = 3002
    var  = "APP_HS_PORT"
  [[ports.port]]
    name = "backend"
    base = 8088
    var  = "APP_BACKEND_PORT"
  [[ports.port]]
    name = "supabase_api"
    base = 54321
    var  = "APP_SUPABASE_API_PORT"
  [[ports.port]]
    name = "supabase_db"
    base = 54322
    var  = "APP_SUPABASE_DB_PORT"
  [[ports.port]]
    name = "supabase_db_shadow"
    base = 54320
    var  = "APP_SUPABASE_DB_SHADOW_PORT"
  [[ports.port]]
    name = "supabase_studio"
    base = 54323
    var  = "APP_SUPABASE_STUDIO_PORT"

[vars]
  APP_SUPABASE_PROJECT_ID                 = "app-${MWS_COPY_SLUG}"
  APP_SUPABASE_API_URL                    = "http://localhost:${APP_SUPABASE_API_PORT}"
  APP_AUTH_SITE_URL                       = "http://127.0.0.1:${APP_INTERNAL_PORT}"
  APP_AUTH_REDIRECT_SITE                  = "https://127.0.0.1:${APP_INTERNAL_PORT}"
  APP_AUTH_REDIRECT_CALLBACK_IP           = "http://127.0.0.1:${APP_INTERNAL_PORT}/auth/callback"
  APP_AUTH_REDIRECT_CALLBACK_LOCALHOST    = "http://localhost:${APP_INTERNAL_PORT}/auth/callback"
  APP_AUTH_REDIRECT_HS_CALLBACK_IP        = "http://127.0.0.1:${APP_HS_PORT}/auth/callback"
  APP_AUTH_REDIRECT_HS_CALLBACK_LOCALHOST = "http://localhost:${APP_HS_PORT}/auth/callback"
  APP_SUPABASE_AZURE_REDIRECT_URI         = "http://localhost:${APP_SUPABASE_API_PORT}/auth/v1/callback"
  APP_AUTH_COOKIE_NAME                    = "app-auth-token-${APP_INTERNAL_PORT}"

[[renders]]
  source = ".mcp.json.render"
  target = ".mcp.json"

[[setup]]
  cmd = "direnv allow"
```

shop, a web app with Postgres in docker compose and no Go changes. The default stride is 10, and the 65535 bound caps the slot limit at 6010.

```toml
project_name = "shop"

[[repos]]
  folder = "web"
  url    = "git@github.com:example/shop-web.git"

  [[repos.envs]]
    source = ".env"
    target = ".env"

[ports]
  [[ports.port]]
    name = "web"
    base = 5173
  [[ports.port]]
    name = "db"
    base = 5432

[vars]
  COMPOSE_PROJECT_NAME = "shop-${MWS_COPY_SLUG}"
  DATABASE_URL         = "postgres://postgres:postgres@localhost:${MWS_PORT_DB}/shop"
```

with `.envs/web/.env`:

```sh
PORT=${MWS_PORT_WEB}
DB_HOST_PORT=${MWS_PORT_DB}
DATABASE_URL=${DATABASE_URL}
COMPOSE_PROJECT_NAME=${COMPOSE_PROJECT_NAME}
```

At slot 1, `web/.env` says `PORT=5183` and `DB_HOST_PORT=5442`, and the compose project of a copy named `two` is `shop-two`.

Design rationale: [ADR-0015](./adr/0015-per-copy-port-slots.md).

## Worked example

```toml
project_name = "app"
description  = ""

[[repos]]
  folder = "backend"
  url    = "git@github.com:example/api.git"

  [[repos.setup]]
    cmd = "go build ./..."

[[repos]]
  folder = "frontend"
  url    = "git@github.com:example/web.git"

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
  url    = "git@github.com:example/worker.git"
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
- `mws add-repo <url> [folder]` appends a `[[repos]]` entry (prompting for it when run with no arguments on a terminal) and clones it into every working copy. `mws init --repo <url>` registers repos at creation.
- `mws add-repo --optional` appends an `[[optional_repos]]` entry without cloning it anywhere; pull it into a copy later with `mws clone --with` or `mws include`.

`[[repos.envs]]` and `[[repos.setup]]` are hand-edited today; there are no dedicated subcommands. The schema is round-trip-safe: `mws` reads and writes the file using `BurntSushi/toml`, preserving every key it recognises. Unknown keys are ignored on parse but dropped on the next write -- avoid adding fields that mws does not understand.

Changing `working_copies_dir` after the fact does **not** relocate any existing working copies. The new value only affects subsequent `mws clone` (and `mws init`'s first copy on a fresh workspace). Move existing peers by hand if you want them under the new prefix, then run `mws relink` from the meta root.

The file is tracked by the meta workspace's git. Commit changes alongside the harness updates that motivate them.

## Related

- [GLOSSARY.md](../GLOSSARY.md) - domain language (Meta workspace, Working copy, Native repo, Env staging, Setup commands)
- [ADR-0003](./adr/0003-independent-native-repos.md) - why native repos are independent clones, not submodules
- [ADR-0004](./adr/0004-meta-at-root-architecture.md) - why the meta workspace sits at the project root
- [ADR-0005](./adr/0005-env-staging-via-copy.md) - env staging design
- [ADR-0006](./adr/0006-post-clone-setup-commands.md) - setup commands design
- [ADR-0007](./adr/0007-working-copies-subdir.md) - configurable working-copies subdirectory
- [ADR-0009](./adr/0009-optional-repos.md) - optional repos pulled into copies on demand
- [ADR-0010](./adr/0010-soft-delete-working-copies.md) - why `mws rm` trashes instead of deleting
- [ADR-0011](./adr/0011-worktrees-are-children-of-a-working-copy.md) - why worktrees live inside the working copy
- [ADR-0012](./adr/0012-headless-cli-contract.md) - prompts, output streams, exit codes and retries for agents and scripts
- [ADR-0015](./adr/0015-per-copy-port-slots.md) - per-copy port slots, rendering, `[vars]` and root `[[setup]]`

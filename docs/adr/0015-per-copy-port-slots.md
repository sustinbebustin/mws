# Per-copy port slots rendered at env staging time

> Status: ACCEPTED 2026-10-15, implemented. Ships in one release with the CLI review changes listed under Consequences.

A **Meta workspace** may declare named ports in the **mws config**. A
**Working copy** is in one of two states. On the **default ports** (slot 0,
where every copy starts) it renders each named port at its `base`. Holding a
**Port slot** `k >= 1`, taken with `mws ports claim`, it owns port
`base + k * stride` for every named port. The slot is recorded in a **Port
lease**, a generated dotenv file at the copy root, `<copy>/.mws-ports.env`,
which also exports every port, builtin and derived variable for the shell,
`just`, direnv and setup commands. The lease is the only record: allocation
enumerates the leases of live copies under a lock and starts at 1, `mws ports
release` returns a copy to the default ports, and `mws rm` releases before
trashing, so the trash only ever holds copies on the default ports and `mws
restore` brings one back the same way.

Ports reach files by **rendering**, never by rewriting. A staged env file
(ADR-0005) may contain `${VAR}` placeholders for any variable the lease exports;
env materialisation (`mws clone`, `mws include`, `mws env sync`, `mws restore`,
`mws ports claim|release`) substitutes them on the way into the copy, at slot 0
when the copy holds no lease, and leaves every other `$` byte alone. There is
one render path, in the `env` group; `ports claim` and `ports release` change
the lease and then call it. `mws clone --ports` clones and claims in one run. A harness file that needs per-copy values (`.mcp.json` for Claude
Code's MCP servers) stays in the harness under a `.render` name and is rendered to
its target name by the same step, so the fan-out rule of ADR-0004 keeps its "no
exclusions" and `mws relink` never sees the rendered file as a diverged harness
file.

Three config tables, each usable without the others:

```toml
[ports]                               # allocation: numbers
  stride = 10                         # default 10; slot k moves every port by k * stride
  slots  = 50                         # optional cap; default: the largest slot whose ports stay at or below 65535

  [[ports.port]]
    name = "api"                      # exported as MWS_PORT_API
    base = 54321
    var  = "SUPABASE_API_PORT"        # optional alias exported alongside MWS_PORT_API

[vars]                                # derivation: strings from ports and builtins
  SUPABASE_PROJECT_ID = "shop-${MWS_COPY_SLUG}"
  SUPABASE_API_URL    = "http://localhost:${MWS_PORT_API}"

[[renders]]                           # rendering: harness files written per copy
  source = ".mcp.json.render"           # under <meta>/.mws/
  target = ".mcp.json"                # under <copy>/

[[setup]]                             # root-level setup, cwd = the copy root
  cmd = "direnv allow"
```

Builtins exported in every lease and usable in `[vars]` and in any rendered
source:
`MWS_SLOT`, `MWS_COPY` (the copy's directory name), `MWS_COPY_SLUG`
(lower-cased, every character outside `[a-z0-9-]` replaced by `-`, for docker
and Supabase project names), `MWS_META` (absolute meta root).

The subcommand group is `mws ports`, non-interactive, results on stdout:

| Command | What it does |
|---|---|
| `mws ports [show] [copy] [--json] [--export]` | Print the copy's slot and every exported variable as `VAR=value` lines; `--export` prefixes `export ` so `eval "$(mws ports show --export)"` loads them into a shell without direnv (an agent's shell, for instance). |
| `mws ports ls [--json]` | Every live copy as `default` or `slot N`, conflicts (two leases, one slot), and the next free slot. |
| `mws ports claim [copy] [--slot N] [--dry-run] [--json] [--setup\|--no-setup]` | Allocate the lowest free slot from 1 when the copy has none (or move it to `N`), write the lease, re-render every env target and `[[renders]]` target, run root `[[setup]]`. Refuses (exit 3) a slot another copy holds. Re-running on a slotted copy re-renders and keeps its slot. |
| `mws ports release [copy] [--dry-run] [--json] [--setup\|--no-setup]` | Drop the lease, re-render at the default ports, run root `[[setup]]`. A copy already on the defaults exits 0 with nothing to do. |
| `mws ports check [copy] [--json]` | Bind-probe every port of the copy's slot and report listeners. |

`mws clone` gains `--ports`: clone, then claim, with one setup pass. Every
`[copy]` argument resolves through `ResolveCopy`, so inside a worktree the
commands act on its working copy. The group follows the `worktree` group's
contract (ADR-0012): group-level `--json`, `{"error": {code, message}}` on
failure, exit 4 for an unknown copy, one `would <verb> <object>` voice under
`--dry-run`.

## Why

Peer working copies exist so stacks can run side by side, and today they
cannot: every copy renders the same env files, so the second `supabase start`
or `next dev` collides with the first. A meta can work around this with two
harness scripts, `sync-ports` and `unsync-ports`, which allocate a slot from a
registry line, `sed`-rewrite the env files, append managed blocks, upsert a
`PORT` pin and regenerate `.mcp.json`. Such scripts show a list of failure
modes that a mechanism inside mws has to make impossible rather than patch:

- **Two copies of the port list drift.** A new app's port gets added to
  `sync-ports` and only later to `unsync-ports`; between the two the inverse
  is wrong. The inverse must not exist: rendering a source at slot 0 is the
  undo of rendering it at slot `k`.
- **Rewriting depends on history.** A copy that changes slot keeps the old
  ports when the `sed` matches only slot-0 values; the fix maps
  `prev -> 0 -> new` and reads the previous slot out of a dotenv file before
  overwriting it. A render is a pure function of (source, slot).
- **Absent vars fall back silently.** Cross-app URLs missing from `.env.local`
  default to slot-0 ports, so a copy quietly addresses another copy's apps.
  With placeholders, the staged file is the complete list.
- **The registry leaks and sits in the harness.** A registry under `.mws/`
  keeps lines for copies that were trashed, and because it lives in `.mws/`
  it is symlinked into every working copy. A lease inside the copy is
  dropped by `rm` and never fans out.
- **A forgotten sync is silent.** A copy with no registry line renders the
  default ports and collides with `main`, and nothing says so.
  Allocation stays opt-in (a copy on the shared default stack is a legitimate
  state), but `mws clone` and `mws ls` say which copies are on the defaults.
- **The harness symlink fights the generated file.** `sync-ports` replaces
  the `.mcp.json` symlink with a file; `mws relink` then sees a diverged
  harness file and `--prefer peer` would move one copy's ports into the
  harness. Rendering `.mcp.json.render` to `.mcp.json` gives the two
  different names.
- **Worktrees need a guard** because the scripts locate the copy from
  `$PWD`. `project.Locate` already resolves a worktree to its copy.
- **`.envrc` plumbing** (`source_up_if_exists`) is direnv policy for one
  project and belongs in its harness.

The mechanism also has to stay domain-free (CLAUDE.md): no project prefix,
no fixed port list, no fixed file list, no Supabase, no cookie names in Go.
Each of those becomes a line of `.mws.toml` or a line in a staged source.

## Considered options

- **Post-hoc rewriting of generated files (today's scripts, in Go).**
  Rejected. It needs an exact inverse that drifted twice, it depends on the
  previous state of each file, it cannot tell a hand-set host from a slot-0
  default without heuristics, and it fights the harness fan-out. Env staging is
  a byte copy (`atomicfile.Copy` in the clone, env sync and worktree carry paths),
  so one substitution step on that path reaches every consumer: clone,
  include, env sync, restore, and worktree creation (which copies the rendered
  files from the copy and renders nothing itself).
- **Env only, no rendering.** mws would write the lease and tools would read
  ports from the environment (Supabase `env()`, Next's dotenv-expand, compose
  `${VAR}`, `os.Getenv`). Staged files would stay identical across copies and
  `env stage` would round-trip cleanly. Rejected as the only mode: a process
  started outside direnv or `just` (an IDE launch, an agent's shell) sees empty
  values or a compiled-in default, which for a Go server on `:8088` is another
  copy's port. It remains available at zero cost: a staged file with no
  placeholders is copied byte for byte, and the lease is still exported.
- **`${VAR}` versus Go `text/template`.** `${VAR}` chosen, substituted only for
  the names the lease exports; any other `$` sequence, including bare `$NAME`,
  passes through untouched, so `.envrc` (bash) and dotenv-expand lines keep
  working. An unsubstituted `${UPPER_CASE}` placeholder is reported as
  `[WARN]` so a typo cannot ship silently. Docker compose's `.env`
  interpolation is the precedent. The skeleton's Go templates render once at
  `mws init` and never see these files.
- **A central registry at the meta root.** Rejected. It needs an edit on
  `rm`, `rm --purge` and `restore --as`, a repair path when lost or
  hand-edited, and it duplicates a fact the copy already carries. A lease in
  the copy is dropped by `rm`, copied into worktrees by the existing root-file
  carry, and found by enumerating live copies. Allocation runs under `flock`
  on the meta root directory, so two concurrent `mws clone` runs cannot pick
  one slot and no lock file is added. Two leases with one slot (a copy
  duplicated with `cp -r`) are reported by `mws ports ls` and refused by
  `mws ports claim` until `--slot` settles it.
- **Allocating on `mws clone`.** Rejected. It would remove the forgotten-sync
  collision, but it makes every copy a stack owner: the shared default stack
  stops being a state a copy can be in on purpose, `release` loses its
  meaning, and `restore` has to allocate. Opt-in keeps two honest states,
  default and slotted, with `clone` and `list` naming the default ones.
- **A trashed copy keeps its slot.** Rejected. Nothing can run from the
  trash, so a held slot has no value, and `restore` would face a lease whose
  number may have been reused. `mws rm` instead runs `release` before the
  rename (the same files `unsync-ports` produces today), so the trash only
  holds copies on the default ports and `restore` needs no port logic at all.
  `rm --purge` skips the render.
- **Slot 0 as an ordinary lease.** Rejected. Every copy without a lease already
  renders the slot-0 values, so a lease for 0 would be a second thing that
  renders identically and means something else. The allocator starts at 1 and
  `mws ports ls` shows copies without a lease as `default`.
- **Free-port probing at start (`get-port`, portless's 4000-4999 pool).**
  Rejected as the allocator: a port that changes between runs breaks every
  URL baked into an env file, every OAuth redirect URI registered with an
  identity provider, and every MCP server URL. Kept as a check: allocation
  skips a slot whose ports have a listener right now (`[WARN]`), `--slot`
  overrides, `mws ports check` reports listeners.
- **Conductor's `CONDUCTOR_PORT`.** Right shape (a stable block per
  workspace, exported as an env var, scripts read it), adopted for the stride
  and the export file. Insufficient alone: one base cannot cover a 3000-range
  and a 54320-range service, and nothing renders the files that tools read
  without consulting the environment.
- **Docker compose project names and Supabase `project_id`.** Both namespace
  containers and volumes, not host ports, and both take literal ports from the
  environment. `${MWS_COPY_SLUG}` exists so a derived var can feed them.
- **Per-copy hostnames instead of ports** (`notes.shop.localhost` through
  puma-dev or portless). Weighed seriously: a per-port cookie name
  (`NEXT_PUBLIC_AUTH_COOKIE_NAME=app-auth-token-<port>`) is a symptom of
  browsers scoping cookies by host and ignoring the port, and hostnames fix
  that properly. Rejected as the mechanism: it hides ports rather than
  removing them (each process still binds a distinct port the proxy must
  know), it does nothing for non-HTTP listeners (Postgres, the shadow
  database), it needs a resident daemon plus resolver or hosts-file work for
  anything that is not a browser (ADR-0010 ruled out resident processes), and
  identity providers reject wildcard redirect URIs just as they reject
  unregistered ports. It composes with this design: a derived var such as
  `HOST = "${MWS_COPY_SLUG}.shop.localhost"` and a proxy run from the harness
  need nothing from mws, and the cookie name stays one `[vars]` line.
- **Worktrees with their own slots.** Rejected, as ADR-0011 decided: the
  expensive stack belongs to the copy, worktrees are short-lived and share it,
  and the harness keeps them off database recipes. `mws ports` inside a
  worktree acts on the copy. Because a worktree carries copies of the lease
  and the env targets (`carry.go`), a re-render of the copy re-renders them
  too: each carried file whose content still matches the hash in
  `.mws-worktree.toml` is rewritten and its hash updated, and one the user
  edited is left alone with a `[WARN]`, the same rule `wt rm` uses to tell
  mws's files from the user's. `mws env sync` follows the same rule, which
  retires its "never refreshes worktrees" exception. The lease model allows
  an opt-in per-worktree mode later without redesign.
- **Everything under `[ports]`.** Rejected. Derived strings and harness
  renders are per-copy rendering, not allocation: a workspace with no ports
  can still render a file from `MWS_COPY`. Three tables keep each concept
  under its own name.
- **Derived vars referencing other derived vars.** Rejected: TOML tables are
  unordered, so evaluation order would be undefined. A `[vars]` entry may
  reference ports and builtins only; `Config.Validate` rejects anything else.
- **Root-level `[[setup]]`.** Added. `[[repos.setup]]` runs with cwd = the
  repo, so nothing could run `direnv allow` at the copy root; ADR-0006's
  objection to workspace-level commands was the missing implicit cwd, which
  the copy root supplies, and `[[worktrees.setup]]` is the precedent. It runs
  with the lease's variables in the environment after every render: on
  `clone` and `include` after the repos' setup, and on `env sync`, `ports
  claim` and `ports release` on its own, behind the same `--setup` /
  `--no-setup` pair (required headless when `[[setup]]` is non-empty).
  `[[repos.setup]]` never re-runs on a render; `pnpm install` is not a render
  concern. `direnv allow` is the motivating entry, because direnv refuses a
  changed `.envrc` until it is allowed again. direnv itself is a harness
  choice and never an mws dependency: the rendered files carry the values,
  `just` reads the lease directly, setup commands get it from mws, and a
  shell without direnv uses `eval "$(mws ports show --export)"`.

## Consequences

- **Slot arithmetic is exact, not block-based.** A copy owns
  `base + k * stride` for each named port and nothing in between. Two ports
  collide at some pair of slots only when their bases are congruent modulo
  the stride, and then at slot `d = (b - a) / stride`; `Config.Validate`
  rejects such a pair unless the effective limit (`slots`, or the last slot
  whose ports stay at or below 65535) is below `d`. The allocator stops at
  that limit. For the example below (bases 3000, 3001, 3002, 8088, 54320-54323;
  stride 10) the limit is 1121, set by 54323. Three pairs (3000/54320,
  3001/54321, 3002/54322) are congruent modulo 10 but first meet at slot
  5132, past the limit, so the config is valid.
- **Slot 0 is the committed defaults and is never leased.** A workspace that
  adds `[ports]` changes nothing for its existing copies until one claims a
  slot; `main` stays on today's ports by never claiming one.
- **Re-rendering has `env sync` semantics.** `mws ports claim`, `mws ports
  release` and `mws rm` overwrite every env target from staging, as `mws
  env sync` does (ADR-0005), so a hand edit in a rendered file is lost.
  Release after claim restores the exact bytes a fresh clone produces, because
  both are `render(source, 0)`. Env targets of an **Optional repo** present
  in the copy render too; before this, the env commands iterated
  `[[repos]]` only. `mws env stage` of a copy whose staged source holds
  placeholders would bake one slot's ports into the source, so it skips
  that file with a `[WARN]` and stages the rest; `--force` stages the
  rendered file over the source. `env stage` refuses (exit 3) inside a
  worktree, since `ResolveCopy` would capture the copy's files, not the
  worktree's. Any file that fails to render or stage makes the command exit 1.
- **`mws rm` trashes even when release fails.** If the render fails (a staged
  source is gone, a target directory is read-only), `rm` drops the lease,
  reports `[WARN]`, and trashes anyway; a port file that cannot be rewritten
  must not keep a copy out of the trash.
- **Setup commands see the ports.** `runSetup` adds the lease's variables to
  `os.Environ()` for `[[repos.setup]]`, `[[setup]]` and `[[worktrees.setup]]`.
- **The lease name is fixed**, `.mws-ports.env`, like `.mws-worktree.toml`:
  dot-prefixed, mws-generated, with a header saying so. The direnv and
  justfile lines that read it are the project's harness policy.
- **A workspace without `[ports]`, `[vars]` or `[[renders]]` is unchanged**:
  no lease, no lock, no substitution. `mws rm` and `mws restore` do nothing
  new.
- **`mws ls` gains a ports column** (`default` or `slot N`), `mws clone`
  ends with a `[WARN]` that the new copy shares the default ports when
  `[ports]` is configured and `--ports` was not given, and `mws rm --dry-run`
  and the confirmation name the slot being released. `mws restore` is
  untouched.
- **`mws relink`** treats a `[[renders]]` target as a per-copy regular
  file, which it already does for any root file with no same-named harness
  entry; the `.render` source itself is linked into every copy like any
  harness entry and ignored by tools. **`mws promote`** refuses (exit 3) an
  **mws marker** (`.mws-*`) and any `[[renders]]` target, and omits both
  from its candidate list; otherwise `mws promote .mcp.json` would move one
  copy's rendered ports into the harness beside its source.
- **Property-tested invariants** for `internal/ports` (rapid): after any
  sequence of claim, move, release, rm, restore and clone, no two live leases
  share a slot and no lease is 0; claiming on a copy that holds a slot keeps
  it; release then claim gives the lowest free slot; render is pure (`render(t, k)`
  then `render(t, 0)` equals `render(t, 0)` on a fresh copy); rendering never
  alters a byte outside a `${VAR}` for an exported `VAR`; allocation never
  exceeds the collision-free limit.
- **New vocabulary** for GLOSSARY.md: **Port slot**, **Port lease**,
  **Render**, **mws marker** (a `.mws-*` file mws writes at a copy or
  worktree root: the worktree marker, the lease). "Registry", "sync-ports",
  "unsync" and "sync" as a verb for claiming are avoided. The glossary
  reserves "template" for Go's `text/template`, so the per-copy harness file
  is a **Render** too: the table is `[[renders]]` and the source carries a
  `.render` suffix, never `.tmpl`, which the skeleton uses for files rendered
  once at `mws init`.
- **This ADR lands with the CLI review's changes** (`.scratch/2026-10-15-cli-design-review.md`):
  the `env` group (`sync`, `stage`, `diff`) that owns the render path, `ls`
  as the one list spelling, `--prefer copy|harness|skip`, `[copy]` as the
  positional name, usage exits from `ResolveCopy`, one dry-run voice,
  `add-repo` appending to the config instead of re-encoding it, `init`
  committing the skeleton, `promote` linking into worktrees, `trash --json`
  on every subcommand, `--json` on `clone`, `rm` and `restore`,
  `mws path <copy>`, and the removal of the sibling-meta converter.
- **Migrating from `sed`-based port scripts** is config plus placeholders:
  `[ports]` and `[vars]` tables, placeholders in the staged files,
  `.mws/.mcp.json` renamed to `.mws/.mcp.json.render`, `mws ports claim
  <copy> --slot N` once per slotted copy to adopt its current numbers (`main`
  stays on the defaults, no lease), and the registry and both scripts
  deleted. Three differences from such scripts' output are deliberate: a
  default copy writes explicit values rather than relying on a tool's own
  defaults file (same values); a port-derived name such as a cookie name
  changes on the defaults too (`app-auth-token-3000`, one re-login on
  `main`); a slug-derived project id for `main` becomes `app-main` (one
  `supabase stop` and start on `main`). There is no slot-0 special case in
  mws or in the config; a bare project id can be kept only by writing it
  without `${MWS_COPY_SLUG}`, which would then be shared by every copy.
- **Amended: the lease follows the config.** Every render of a copy that
  holds a slot rewrites its lease from the current `[ports]` and `[vars]`
  under the meta-root lock, so an edit to either reaches the lease (and the
  shell, `just` and direnv) on the next `mws env sync`. `Config.Validate`
  requires at least slot 1 to be claimable. A copy left above the limit by a
  later edit is flagged `out of range` by `mws ports ls` and refused (exit 3)
  by a claim without `--slot`. An unreadable lease is overwritten by `mws
  ports claim <copy> --slot N`, dropped by `mws ports release` and `mws rm`
  (with `previous_slot: null` under `--json`), and refuses (exit 3) a claim
  for any other copy, since the slot it holds is unknown and could be handed
  out twice.

### Example: a Next.js + Supabase monorepo

Three Next.js apps (`internal`, `public`, `hs`), a Go backend and a local
Supabase stack:

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

Staged sources then carry the values. `.envs/frontend/supabase.env.local`
becomes a plain list, `APP_SUPABASE_PROJECT_ID=${APP_SUPABASE_PROJECT_ID}`
and so on; `.envs/frontend/internal.env.local` holds
`NEXT_PUBLIC_SITE_URL=http://localhost:${APP_INTERNAL_PORT}`,
`NEXT_PUBLIC_SUPABASE_URL=${APP_SUPABASE_API_URL}`,
`NEXT_PUBLIC_API_URL=http://localhost:${APP_BACKEND_PORT}/api/v1`,
`NEXT_PUBLIC_AUTH_COOKIE_NAME=${APP_AUTH_COOKIE_NAME}`; `.envs/backend/.envrc`
holds `export PORT=${APP_BACKEND_PORT}`; `.mws/.mcp.json.render` holds
`"url": "http://localhost:${APP_SUPABASE_API_PORT}/mcp"`. The lease
`<copy>/.mws-ports.env` lists `MWS_SLOT`, `MWS_COPY`, `MWS_COPY_SLUG`,
`MWS_META`, `MWS_PORT_*` and `APP_*_PORT` for the eight ports, and the ten
derived vars; the justfile and `.mws/.envrc` read it.

### A second workspace, no Go changes

A single web app with Postgres in docker compose:

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

`.envs/web/.env`:

```sh
PORT=${MWS_PORT_WEB}
DB_HOST_PORT=${MWS_PORT_DB}
DATABASE_URL=${DATABASE_URL}
COMPOSE_PROJECT_NAME=${COMPOSE_PROJECT_NAME}
```

The repo's `docker-compose.yml` publishes `"${DB_HOST_PORT}:5432"` and compose
reads `web/.env` on its own. `mws clone two && mws ports claim two` gives slot
1: `web/.env` says `PORT=5183`, `DB_HOST_PORT=5442`, project `shop-two`;
`main`, which never claimed a slot, keeps 5173 and 5432. Nothing in mws knows what a web
app or Postgres is.

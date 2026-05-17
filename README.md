# mws

A small CLI for managing **meta workspaces** around one or more native git repos.

The problem: you have a project that spans several independent git repos (frontend, backend, infra, ...). You want one place for shared docs, AI-harness configuration, and project-level tooling -- and you want to spin up cheap parallel working copies for parallel work without git worktrees.

`mws` gives each project a single directory `<name>/` that IS a git repo at its root and hosts the harness under `.mws/`. Working copies live as untracked children (`<name>/main/`, `<name>/feature-x/`, ...) and pick up the harness via symlinks. Native repos inside each working copy are independent clones, so agents in different working copies never fight over the same `.git/`.

## Install

```bash
go install github.com/sustinbebustin/mws/cmd/mws@latest
```

The skeleton (the harness template content) ships embedded in the binary -- no separate template repo, no network at `init` time.

## Quick start

Greenfield:

```bash
cd ~/dev
mws init my-project              # writes ~/dev/my-project/ with .mws/, .mws.toml, main/
cd my-project
mws add-repo git@github.com:org/frontend.git
mws add-repo git@github.com:org/backend.git
mws clone feature-x              # creates ~/dev/my-project/feature-x/, sharing the harness
mws list                         # lists working copies inside the meta
```

Migrate from the old sibling-meta layout:

```bash
mws migrate ~/dev/my-project-meta
```

## Layout

```
~/dev/my-project/                # this directory IS a git repo (meta root)
  .git/                          # tracks .gitignore, .mws.toml, .mws/, README.md
  .gitignore                     # allowlist
  .mws.toml                      # mws config (project name, repos, env mappings)
  .mws/                          # harness; every entry symlinks into every working copy
    CLAUDE.md
    .claude/
    .workspace/
    justfile
  .envs/                         # env-file staging, per-repo (untracked)
  main/                          # first working copy, untracked
    frontend/                    # independent native git clone
    backend/                     # independent native git clone
    CLAUDE.md -> ../.mws/CLAUDE.md
    .claude/   -> ../.mws/.claude/
    ...
  feature-x/                     # any number of additional working copies
    frontend/                    # independent clone (own .git/)
    ...
```

Every top-level entry under `.mws/` is symlinked into each working copy -- discovered dynamically, no hardcoded list. New files added directly in a working copy stay local until `mws promote <path>` moves them into `.mws/` and backfills symlinks across every other working copy.

## Subcommands

| Command | What it does |
|---|---|
| `mws init [name]` | Create a new meta workspace with a first working copy at `<name>/main/`. |
| `mws add-repo <url> [folder]` | Register a native repo in `.mws.toml` and clone it into every working copy. |
| `mws clone <name>` | Create a new working copy inside the meta. Native repos clone via `git clone --local` when possible (hardlinked objects), fall back to the configured URL. Each repo is checked out on its default branch. |
| `mws promote <path>` | Move a top-level file/dir from the current working copy into `.mws/`, symlink it back, and backfill the symlink into every other working copy. |
| `mws list` | List working copies inside the meta. |
| `mws rm <name>` | Remove a working copy. Confirms before destruction. |
| `mws relink` | Refresh harness symlinks across every working copy; handles diverged content interactively. |
| `mws sync-env [name]` | Copy staged env files from `.envs/` into a working copy (overwrites). |
| `mws stage-env [name]` | Inverse of `sync-env`: capture a working copy's env files into staging as the new default. |
| `mws migrate <path>` | Convert an old sibling-meta layout to meta-at-root. Accepts either the `<name>-meta/` dir or one of its working copies. |

## Env staging

Each `[[repos.envs]]` entry in `.mws.toml` maps a flat-named staged source file to a target path inside the repo working tree:

```toml
[[repos]]
  folder = "frontend"
  url = "..."

  [[repos.envs]]
    source = "apps-internal.env"
    target = "apps/internal/.env"
```

On `mws clone <name>`, staged files are copied (not symlinked) into the new working copy at their target paths. Subsequent `mws sync-env <name>` re-pushes staged defaults; `mws stage-env <name>` captures live values back into staging. Env staging is automatically untracked by the meta's allowlist `.gitignore` -- secrets stay local.

## Design references

The architecture is captured in [`docs/adr/`](./docs/adr):

- [`0001-sibling-meta-architecture.md`](./docs/adr/0001-sibling-meta-architecture.md) -- original sibling-meta layout (superseded).
- [`0002-embedded-skeleton-distribution.md`](./docs/adr/0002-embedded-skeleton-distribution.md) -- why the skeleton ships in the binary via `//go:embed`.
- [`0003-independent-native-repos.md`](./docs/adr/0003-independent-native-repos.md) -- why each working copy gets its own `.git/` for native repos.
- [`0004-meta-at-root-architecture.md`](./docs/adr/0004-meta-at-root-architecture.md) -- current layout: meta IS the git repo, working copies are untracked children, harness lives in `.mws/`.
- [`0005-env-staging-via-copy.md`](./docs/adr/0005-env-staging-via-copy.md) -- env files via copy-on-clone with config-driven mapping.

## Development

```bash
go build ./...
go test ./...
go install ./cmd/mws
```

The skeleton lives at [`skeleton/`](./skeleton). Edits there ship in the next binary build.

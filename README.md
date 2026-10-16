<div align="center">
  <img src="docs/assets/mws.jpg" alt="mws" width="640">
</div>

## What Is mws?

A small Go CLI for managing **meta workspaces** -- a directory layout that wraps one or more native git repos with a shared AI-harness layer and cheap parallel working copies.

1. `mws init my-project` creates a meta workspace: a git repo at the root, with the harness under `.mws/` committed as its first commit, and a first working copy at `main/`. Init needs a git identity (`user.name`, `user.email`) and fails before creating anything without one.
2. `mws add-repo <url>` registers a native repo and clones it into every working copy.
3. `mws clone <peer>` spins up another working copy that shares the same harness via symlinks, with its own independent native git clones inside.

The result: parallel agents work side-by-side without fighting over the same `.git/`, and changes to the shared harness propagate via symlinks instead of branch syncs.

## Prerequisites

- [Git](https://git-scm.com/) -- the meta workspace and every native repo are git repositories.

## Install

### Homebrew (macOS, Linux)

```bash
brew install sustinbebustin/tap/mws
```

### Pre-built binary

Download the archive for your OS and architecture from the [latest release](https://github.com/sustinbebustin/mws/releases/latest), extract it, and drop `mws` somewhere on your `PATH`. Releases include binaries for darwin and linux on amd64 and arm64, plus a `checksums.txt`.

### Go install

Requires [Go](https://go.dev/) 1.21+ (newer Go toolchain auto-fetched via `GOTOOLCHAIN=auto`).

```bash
go install github.com/sustinbebustin/mws/cmd/mws@latest
```

The harness skeleton ships embedded in the binary -- no separate template repo, no network at `init` time. Run `mws version` to verify the install.

### Shell integration (optional)

```bash
mws shell-init --install    # then open a new shell
```

This adds one line to your shell's rc file so `mws clone` and `mws restore` cd into the working copy on success. `mws shell-init --status` checks it is loaded. Without it, `mws clone` prints the path as its last stdout line for you to `cd` into. See [ADR-0008](docs/adr/0008-shell-integration-for-auto-cd.md).

### Agent skill (optional)

[`skills/mws`](skills/mws/SKILL.md) teaches coding agents to drive mws headless. Copy it to `~/.claude/skills/mws`, or run `bunx skills add sustinbebustin/mws`.

## Quick start

1. Create a meta workspace.

```bash
cd ~/dev
mws init my-project
cd my-project
```

2. Register one or more native repos. Each one is cloned into every working copy.

```bash
mws add-repo git@github.com:org/frontend.git
mws add-repo git@github.com:org/backend.git
```

3. Spin up a peer working copy alongside `main/` for parallel work.

```bash
mws clone feature-x
```

Optionally register repos you only need in some copies, then pull them in on demand:

```bash
mws add-repo --optional git@github.com:org/worker.git   # register, don't clone
mws clone feature-y --with worker                        # include at clone time
mws include worker feature-x                             # add to an existing copy
```

4. See what working copies exist.

```bash
mws ls
```

## Layout

```
my-project/                       # this directory IS a git repo (meta root)
├── .gitignore                    # allowlist
├── CLAUDE.md                     # mws workspace notes, loaded from every working copy by walk-up
├── .mws.toml                     # Project name, native repos, env mappings
├── .mws/                         # Harness; every entry symlinks into every working copy
│   ├── CLAUDE.md
│   ├── .claude/
│   ├── .workspace/
│   └── justfile
├── .envs/                        # Env-file staging, per repo (untracked)
├── .trash/                       # Working copies removed by `mws rm` (untracked)
├── main/                         # First working copy (untracked)
│   ├── frontend/                 # Independent native git clone
│   ├── backend/                  # Independent native git clone
│   ├── CLAUDE.md -> ../.mws/CLAUDE.md
│   ├── .claude/   -> ../.mws/.claude/
│   └── ...
└── feature-x/                    # Additional working copy
    ├── frontend/                 # Independent clone (own .git/)
    ├── .mws-ports.env            # Port lease, once `mws ports claim` gave it a slot (untracked)
    ├── .wt/                      # Worktrees of this copy (untracked)
    │   └── 04/                   # `mws wt new 04`: frontend/ + backend/ on wt/04, harness links
    └── ...
```

Every top-level entry under `.mws/` is symlinked into each working copy -- discovered dynamically, no hardcoded list. New files created directly in a working copy stay local until `mws promote <path>` moves them into `.mws/` and backfills the symlink across every peer.

Working copies can optionally be grouped under a single subdirectory by setting `working_copies_dir` in `.mws.toml` (e.g. `working_copies_dir = "copies"`); subsequent `mws clone`s land at `<meta>/copies/<name>/` instead of `<meta>/<name>/`. See [`docs/config.md`](./docs/config.md) for the rules and caveats.

`mws rm` moves a working copy into `.trash/` rather than deleting it, so a mistaken removal costs nothing: `mws restore <copy>` puts it back with its uncommitted work and env files intact. Trashed copies are purged automatically once they pass the retention window (7 days by default, configurable via `[trash]`). Note that the sweep is opportunistic -- it runs when an mws command touches the trash, not on a timer.

For parallel agents inside one working copy, `mws wt new <name>` creates a **worktree** at `<copy>/.wt/<name>/`: a `git worktree` of every native repo on `wt/<name>`, the same harness symlinks, the copy's ports, env and MCP files, and setup run. `mws wt rm` refuses to lose uncommitted work or unmerged commits. The skeleton wires Claude Code's `WorktreeCreate`, `WorktreeRemove` and `SubagentStop` hooks to `mws wt hook ...`, so `claude -w` and `isolation: "worktree"` subagents use the same layout. See [`docs/config.md`](./docs/config.md#worktrees) and [ADR-0011](./docs/adr/0011-worktrees-are-children-of-a-working-copy.md).

## Commands

| Command | What it does |
|---|---|
| `mws init [name]` | Create a new meta workspace with a first working copy at `<name>/main/` and print its path. Prompts for anything not given; `--description`, `--copies-dir`, `--parent`, `--repo <url>` / `--repo <folder>=<url>` (repeatable) and `--yes` make it non-interactive. |
| `mws add-repo <url> [folder]` | Register a native repo and clone it into every working copy, appending it to `.mws.toml`. With `--optional`, register it under `[[optional_repos]]` without cloning anywhere. Re-running with the same URL fills in copies that lack it. |
| `mws clone <name>` | Create a new working copy inside this meta workspace and print its path. Native repos are freshly cloned from each configured URL and checked out on the default branch. When optional repos exist, prompts to include them; `--with <folder>` (repeatable) or `--no-optional` decides without prompting. `--ports` claims the lowest free port slot before the env render and setup; without it, a workspace with `[ports]` warns that the copy shares the default ports. Re-running for an existing copy clones only the repos it lacks and prints its path; with `--ports` it also claims a slot (keeping one the copy holds) and re-renders. `--json` prints `{copy, path, repos, setup, ports}` instead. |
| `mws include <folder> [copy]` | Clone a registered optional repo into a working copy (the current one by default). |
| `mws promote <path>` | Move a top-level file or directory from a working copy into the harness (`.mws/`), symlink it back, backfill the symlink into every other working copy, and link it into every copy's worktrees. Refuses a native repo folder, the worktrees dir (`[worktrees] dir`), an mws marker (`.mws-*`) and any `[[renders]]` target. |
| `mws ls` | List working copies in this meta workspace with each one's ports (`default` or `slot N`); `--json`. |
| `mws rm <copy>` | Remove a working copy. Confirms first (`--yes` skips), releases its port slot (re-rendering its env files at the default ports), then moves it to `.trash/` so it can be brought back on the defaults; `--purge` deletes outright without rendering, `--dry-run` previews. `--json` prints `{dry_run, copy, path, entry, purged, released_slot}`. |
| `mws restore [copy]` | Bring a trashed working copy back and repair its harness symlinks. `--as <newname>` restores under a different name. `--json` prints `{copy, path, entry}`. |
| `mws path [copy]` | Print the absolute path of a working copy (the current copy by default); exits `4` when there is none. `--json` prints `{copy, path}`. |
| `mws trash` | Inspect and manage trashed working copies. With no subcommand it runs `ls`. `--json` on all three subcommands. |
| `mws trash ls` | List trashed working copies. |
| `mws trash prune` | Purge trashed working copies past their retention window; `--dry-run` previews. |
| `mws trash empty` | Purge every trashed working copy, regardless of age; `--dry-run` previews, `--yes` skips the confirmation. |
| `mws relink` | Refresh harness symlinks in every working copy. Diverged content is asked about, or settled by `--prefer copy\|harness\|skip`; `--dry-run` previews. |
| `mws env` | Sync, stage and diff env files between staging and working copies. All three subcommands take `--json` and cover the optional repos present in the copy. |
| `mws env sync [copy]` | Render staged env files into a working copy (overwrites): from `.envs/` and the `[[renders]]` sources in `.mws/`, at the copy's port slot, refreshing its worktrees' unedited copies, then run root `[[setup]]` (`--setup` / `--no-setup`); `--all` for every copy, `--dry-run` previews. |
| `mws env stage [copy]` | Capture a working copy's env files into staging (overwrites) as the new default, the inverse of `env sync`; `--dry-run` previews. Skips a staged source holding `${VAR}` placeholders with a warning unless `--force`. Refuses inside a worktree. |
| `mws env diff [copy]` | Compare a working copy's env files with staging: each rendered file is `same`, `differs`, `missing in copy` or `missing in staging` against its rendered source; exits `1` unless all are the same. `--all` for every copy. |
| `mws ports [copy]` | Claim, release, show and check the Port slot of a working copy. Without a subcommand it runs `show`. `--json` on every subcommand. See [ADR-0015](./docs/adr/0015-per-copy-port-slots.md). |
| `mws ports show [copy]` | Print a working copy's slot and exported variables, one `VAR=value` per line; `--export` prefixes `export ` for `eval "$(mws ports show --export)"` in a shell without direnv. |
| `mws ports claim [copy]` | Give a working copy its own Port slot and re-render it: the lowest free slot, or `--slot N`. Refuses (exit `3`) a slot another copy holds. Re-renders env files, `[[renders]]` targets and worktrees, then runs root `[[setup]]` (`--setup` / `--no-setup`); `--dry-run` previews. |
| `mws ports release [copy]` | Put a working copy back on the default ports and re-render it. Takes `--setup` / `--no-setup` and `--dry-run` like `claim`. |
| `mws ports ls` | List every working copy's Port slot and the next free slot, flagging two leases that name one slot. |
| `mws ports check [copy]` | Report which of a working copy's ports have a listener. Changes nothing and exits `0` either way. |
| `mws worktree` (`mws wt`) | Create, list, run in and remove worktrees of a working copy. Non-interactive; `--json` on every subcommand but `exec`. |
| `mws worktree new <name>` | Create a worktree of this working copy and print its path. `--branch`, `--base` and `--no-setup` adjust it; re-running for an existing worktree prints its path. |
| `mws worktree ls` | List worktrees with their branches, state and liveness; `--all` covers every working copy. |
| `mws worktree path <name>` | Print the absolute path of a worktree. |
| `mws worktree exec <name> -- <cmd>` | Run a command in a worktree's root and exit with its status. |
| `mws worktree rm <name>...` | Remove worktrees, deleting their fully merged branches. Refuses to lose work; `--force` discards uncommitted files, unreachable detached commits and changed root files; `--keep-branch` keeps every branch; `--dry-run` previews. |
| `mws worktree prune` | Remove every clean, idle, fully merged worktree; `--dry-run` previews. |
| `mws shell-init [shell]` | Print, install or check the shell function that cds into a copy after `mws clone` or `mws restore`. `--install` adds it to the rc file; `--status` reports whether it is active. |
| `mws version` | Print mws version, commit, and build date. |

### Scripting and agents

Every command runs without prompts when given its arguments and flags, and `mws <command> --help` shows examples. Without a terminal on stdin mws never prompts: a missing input fails with exit `2` and the invocation that supplies it, and a confirmation needs `--yes`. `--json` is available wherever a command has a result: `ls`, `path`, `clone`, `rm`, `restore`, `trash`, `env`, `ports` and `wt`. Results go to stdout (paths, lists, `--json`), progress to stderr, so `cd "$(mws clone feature-x --no-optional --setup | tail -n1)"` works. Every `--dry-run` prints one `would <verb> <object>` line per planned change on stdout and nothing else there; under `--json` the same list sits inside `{"dry_run": true, ...}`. Exit codes: `0` ok, `1` failure, `2` usage, `3` refused by a safety check (under `mws ports`, a slot another copy holds), `4` not found. Re-running a command that half-succeeded finishes it, and removing something already gone exits `4` with a message saying so. See [ADR-0012](./docs/adr/0012-headless-cli-contract.md).

Output is coloured only when the stream is a terminal, and colour is the only difference: piped output carries the same text, `--json` is untouched, and `NO_COLOR` is respected. See [ADR-0013](./docs/adr/0013-colour-on-a-terminal-only.md).

## Configuration

The meta workspace's `.mws.toml` lists the native repos, optional env-file mappings, and optional setup commands. `mws clone` reads it to know what to clone, copy, and run.

```toml
project_name = "my-project"

[[repos]]
  folder = "frontend"
  url    = "git@github.com:org/frontend.git"

  [[repos.envs]]
    source = "apps-internal.env"      # staged at .envs/frontend/apps-internal.env
    target = "apps/internal/.env"     # copied here inside the clone

  [[repos.setup]]
    cmd = "pnpm install --frozen-lockfile"
  [[repos.setup]]
    cmd = "pnpm build"
```

On `mws clone`:

- **Env files** are rendered (not symlinked) into the new working copy at their target paths, with `${VAR}` placeholders substituted at the copy's port slot, so peers can hold different ports or service keys. `.envs/` is allowlist-gitignored -- secrets stay local. `mws env sync` re-pushes staged defaults; `mws env stage` captures the current working copy back into staging; `mws env diff` shows drift.
- **Setup commands** run via `sh -c` from the cloned repo's directory after env-copy completes, only for the repos that run clones; root `[[setup]]` commands then run at the copy root after every render (`clone`, `include`, `env sync`). You are prompted before any run; `--setup` skips the prompt, `--no-setup` skips execution. Without a terminal one of the two is required.

**Port slots** keep parallel copies from fighting over ports. Declare the ports once, and each copy can claim a slot that shifts all of them:

```toml
[ports]
  [[ports.port]]
    name = "web"
    base = 5173

[vars]
  APP_URL = "http://localhost:${MWS_PORT_WEB}"
```

`mws ports claim feature-x` (or `mws clone feature-x --ports`) gives the copy slot 1, so `web` becomes 5183, and re-renders its env files. The slot is recorded in the copy's `.mws-ports.env`; `mws ports show` prints the variables, `eval "$(mws ports show --export)"` loads them into a shell, and direnv can `dotenv .mws-ports.env` instead. A copy with no slot stays on the base ports, and `mws rm` releases the slot. Rendered env targets must be gitignored in the native repo, since they differ per copy.

Full schema and rationale: [`docs/config.md`](./docs/config.md).

## Development

```bash
go build ./...
go test ./...
go install ./cmd/mws
```

The skeleton lives at [`skeleton/`](./skeleton). Edits there ship in the next binary build.

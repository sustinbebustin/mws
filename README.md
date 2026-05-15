# mws

A small CLI for managing **meta workspaces** around one or more native git repos.

The problem: you have a project that spans several independent git repos (frontend, backend, infra, ...). You want one place for shared docs, AI-harness configuration, and project-level tooling -- and you want to spin up cheap parallel working copies for parallel work without git worktrees.

`mws` gives each project a `<name>-meta/` directory that holds the harness, and arbitrary peer working copies (`<name>/`, `<name>-bug-fix/`, ...) that symlink back into the meta. Native repos inside each working copy are independent clones, so agents in different peers never fight over the same `.git/`.

## Install

```bash
go install github.com/sustinbebustin/mws/cmd/mws@latest
```

The skeleton (the harness template content) ships embedded in the binary -- no separate template repo, no network at `init` time.

## Quick start

Greenfield:

```bash
cd ~/dev
mws init my-project           # asks a few questions, writes my-project-meta/ and my-project/
cd my-project
mws add-repo git@github.com:org/frontend.git
mws add-repo git@github.com:org/backend.git
mws clone bug-fix             # creates ~/dev/my-project-bug-fix/, symlinked to the same meta
mws list                      # lists peer working copies
```

Brownfield (you already have a meta-at-root layout):

```bash
mws migrate ~/dev/my-project  # moves harness into my-project-meta/, leaves native repos in place
```

## Layout

```
~/dev/
  my-project-meta/            # git-versioned. holds .claude/, .workspace/, CLAUDE.md, justfile, .mws/config.toml
  my-project/                 # working copy. .claude, .workspace, CLAUDE.md, justfile, .mws are SYMLINKS into the meta
    frontend/                 # independent git repo
    backend/                  # independent git repo
  my-project-bug-fix/         # peer working copy. same symlinks, independent native repo clones
    frontend/
    backend/
```

Every top-level entry in the meta (except `.git/`) is symlinked into each working copy. The list is discovered dynamically at `init`, `clone`, and `migrate` time -- no hardcoded list.

If you create a file directly in a working copy, it stays local until you run `mws promote <path>`. Promote moves it into the meta, replaces the original with a symlink, and backfills the symlink into every peer.

## Subcommands

| Command | What it does |
|---|---|
| `mws init [name]` | Create a new meta + working copy. Prompts collect everything, then a single confirm. |
| `mws add-repo <url> [folder]` | Register a native repo in the meta config and clone it into every peer. |
| `mws clone <new-peer>` | Create a new peer working copy. Native repos clone via `git clone --local` when possible (hardlinked objects), fall back to the configured URL. Checks out each repo's default branch. |
| `mws promote <path>` | Move a top-level file/dir from the current working copy into the meta, symlink it back, and backfill the symlink into every peer. |
| `mws list` | List peer working copies of the current meta. |
| `mws rm <peer>` | Remove a peer working copy. Confirms before destruction. |
| `mws migrate <path>` | Convert an existing meta-at-root directory into sibling-meta layout. |

## Design references

The architecture is captured in [`docs/adr/`](./docs/adr):

- [`0001-sibling-meta-architecture.md`](./docs/adr/0001-sibling-meta-architecture.md) -- why the meta lives as a sibling, not inside one working copy.
- [`0002-embedded-skeleton-distribution.md`](./docs/adr/0002-embedded-skeleton-distribution.md) -- why the skeleton ships in the binary via `//go:embed`.
- [`0003-independent-native-repos.md`](./docs/adr/0003-independent-native-repos.md) -- why peer working copies each get their own `.git/` for native repos.

## Development

```bash
go build ./...
go test ./...
go install ./cmd/mws
```

The skeleton lives at [`skeleton/`](./skeleton). Edits there ship in the next binary build.

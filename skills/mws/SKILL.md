---
name: mws
description: Use for any mws or meta workspace task, including creating a meta workspace, cloning or removing working copies, registering native repos, worktrees for parallel work, per-copy port slots, promoting files into the harness, env staging, and relinking harness symlinks. Use whenever the cwd sits inside a directory with a `.mws.toml` above it.
metadata:
  author: sustinbebustin
---

# mws

`mws` manages a **Meta workspace**: a git repo holding the **Harness dir** (`.mws/`) and the **mws config** (`.mws.toml`), plus untracked **Working copies** beside them. Each working copy holds independent **Native repo** clones and a symlink to every top-level harness entry. Every command finds the meta by walking up from cwd, so run it from anywhere inside.

```text
my-project/                 meta workspace (git repo; tracks .mws/ and .mws.toml)
  .mws.toml                 mws config
  .mws/                     harness dir: CLAUDE.md, .claude/, justfile, ...
  .envs/<repo>/             env staging (untracked)
  .trash/                   trashed working copies
  main/                     working copy (not a git repo)
    CLAUDE.md -> ../.mws/CLAUDE.md
    frontend/  backend/     native repos (each its own .git)
    .mws-ports.env          port lease, only once the copy claims a slot (generated; never edit)
    .wt/04/                 worktree of this copy
```

## Rules

- Pass every input as a flag. Your shell has no terminal, so mws never prompts: a missing choice fails with exit 2 before changing anything, and the message shows the flag to add. Destructive commands need `--yes`.
- Results go to stdout, progress to stderr. `init`, `clone` and `wt new` print the new path as the last stdout line; `ls`, `path`, `clone`, `rm`, `restore`, `trash`, `env`, `ports` and every `wt` command take `--json`, and a failure under it prints `{"error": {"code", "message"}}`. Every `--dry-run` prints only `would <verb> <object>` lines on stdout (under `--json`, `{"dry_run": true, ...}`).
- A retry is safe: re-running `clone`, `add-repo`, `include`, `promote`, `relink` or `wt new` finishes the job or reports it done. When a command fails partway, read the message and re-run the same command.
- Run git inside a native repo (`main/frontend/`) for code and at the meta root for harness and config. A working copy root is not a git repo.
- A harness entry in a working copy is a symlink: editing `main/CLAUDE.md` edits the shared file for every copy, and it is committed from the meta root. A file you create at a copy root stays local to that copy until promoted.
- `rm --purge`, `trash empty`, `wt rm --force` and `relink --prefer` destroy or overwrite work. Run them with `--dry-run` first, show the user what goes, and run the real command only on their go-ahead.

## Exit codes

| Code | Meaning | Next step |
|---|---|---|
| 0 | ok | |
| 1 | failure | Read the message; a setup failure keeps the copy or worktree, so fix the cause and re-run. |
| 2 | usage | Add the flag or argument the message names. |
| 3 | refused by a safety check | Report the reason (dirty repo, live process, branch checked out elsewhere) to the user; never reach for `--force` on your own. |
| 4 | not found | The message says whether the name was already trashed or restored; that is "already done", otherwise it is a typo. |

## Working copies

```bash
mws ls --json                                         # copies; "current" marks cwd's
mws clone feature-x --no-optional --setup             # new peer copy, default branches, setup run
mws clone feature-x --with docs --no-setup            # include optional repo "docs", skip setup
cd "$(mws clone feature-x --no-optional --setup | tail -n1)"
mws clone feature-x --no-optional --setup --ports     # also claim the lowest free port slot
mws clone feature-x --no-optional --setup --json      # {copy, path, repos, setup, ports}
mws path feature-x                                    # absolute path; exit 4 if no such copy (--json: {copy, path})
mws rm feature-x --yes                                # to .trash/, restorable for 7 days (--json: entry, released_slot)
mws restore feature-x --yes                           # back from the trash (--as <name> if taken; --json: {copy, path, entry})
mws trash ls --json
```

- `clone` needs `--setup` or `--no-setup` when the config has setup commands, and `--no-optional` or `--with <folder>` when it has optional repos. Choose `--setup` unless the user says otherwise: a copy without setup usually does not build.
- `clone --ports` needs a `[ports]` table in `.mws.toml` (exit 2 without one). Without `--ports`, a workspace with `[ports]` leaves the new copy on the default ports and warns that it shares them with every other unslotted copy.
- `rm` releases the copy's port slot before trashing it, so a restored copy is back on the default ports.
- A working copy is a separate line of work. For short-lived parallel work inside one copy, use a worktree.

## Ports

A copy starts on the default ports (slot 0). Claiming slot `k` moves every port declared under `[ports]` to `base + k * stride` and re-renders the copy's env files, `[[renders]]` targets and worktree copies. Run these inside a copy or a worktree, or name the copy.

```bash
mws ports ls --json                                   # every copy's slot, conflicts, next_free
mws ports claim feature-x --no-setup                  # lowest free slot; --slot N to pick; --dry-run previews
mws ports                                             # this copy's slot and variables (same as `ports show`)
eval "$(mws ports show --export)"                     # load MWS_SLOT, MWS_PORT_<NAME>, [vars] into this shell
mws ports check feature-x --json                      # which of its ports have a listener
mws ports release feature-x --no-setup                # back to the default ports
```

- Exit `3` from `claim`: another copy holds that slot, or two leases name one slot (settle it with `--slot`). Exit `4`: no copy by that name. Exit `2`: no `[ports]` table.
- `claim` and `release` need `--setup` or `--no-setup` when root `[[setup]]` is configured. A claim on a copy that already has a slot keeps it and re-renders.
- A busy port does not fail `claim`: it skips that slot with a `[WARN]` unless `--slot` is given.
- Direnv is optional. A project that uses it reads `.mws-ports.env` itself (`dotenv .mws-ports.env`); a shell without it uses the `eval` line above. Never edit the lease.

## Worktrees

```bash
mws wt new 04 --json                                  # every native repo on branch wt/04
mws wt new 04 --branch feat/save-roof --base main --json
mws wt ls --json
mws wt exec 04 -- git -C frontend status              # run with cwd = the worktree root
cd "$(mws wt path 04)"
mws wt rm 04 --dry-run --json                         # then without --dry-run
mws wt prune --dry-run                                # clean, idle, fully merged ones
```

- `wt new` runs repo setup and `[[worktrees.setup]]` unless `--no-setup`; a setup failure keeps the worktree and exits 1.
- `wt rm` deletes the `wt/<name>` branch only when its commits are reachable elsewhere, and reports the branches it kept. `--keep-branch` keeps all.
- The skeleton wires Claude Code's worktree hooks to `mws wt hook`, so a worktree-isolated subagent lands in an mws worktree. Leave those hooks in `.claude/settings.json`.

## Repos, harness and env

```bash
mws add-repo git@github.com:org/api.git               # register + clone into every copy
mws add-repo git@github.com:org/docs.git --optional   # register only
mws include docs --setup                              # clone an optional repo into this copy
mws promote .mcp.json                                 # copy-local file -> shared harness entry
mws relink --dry-run                                  # then `mws relink`
mws env diff feature-x                                # which env files differ from staging (exit 1 if any)
mws env sync feature-x                                # .envs/ -> copy (overwrites the copy's env files)
mws env stage feature-x                               # copy -> .envs/ (overwrites staging; skips ${VAR} templates unless --force)
```

- `mws shell-init --status` reports whether the cd wrapper is loaded in this shell. `mws shell-init --install` edits the user's shell rc file, so run it only when they ask. An agent never needs it: read the last stdout line of `clone` instead.
- After adding a harness entry under `.mws/` by hand, run `mws relink` to link it into every copy.
- Editors that save atomically replace a symlink with a regular file. `relink` restores identical files on its own; a diverged one needs `--prefer copy` (the copy's version wins), `harness` (the harness wins) or `skip`, which is the user's call.
- Env files are rendered, not linked: `${VAR}` placeholders get the copy's port values, so copies can differ (ports, keys). Treat `env sync` and `env stage` as overwrites (`--dry-run` previews), and read the env file contents only when the task needs them.
- `env sync` refreshes the copy and each worktree's carried env, render and lease files that were not edited there (an edited one is kept with a `[WARN]`); `env stage` refuses inside a worktree.
- Root `[[setup]]` runs at the copy root after every render (`clone`, `include`, `env sync`); headless, pass `--setup` or `--no-setup` when it is configured.

## New meta workspace

```bash
mws init demo --repo git@github.com:org/api.git --repo web=https://github.com/org/web --parent ~/dev --yes
```

Without a terminal, `--yes` is required and unset values take their defaults. The first working copy is `<parent>/demo/main/`. The `.mws.toml` schema is in the repo's [docs/config.md](https://github.com/sustinbebustin/mws/blob/main/docs/config.md); `mws <command> --help` covers each command with examples.

## Report anything that goes poorly

mws is built for agents, so your experience is how it improves. Speak up when a command fails unexpectedly, needed a workaround, gave output that was awkward to parse, or this skill was unclear or wrong. Tell the user and offer to open an issue; it posts from their GitHub account, so get their okay first.

```bash
gh issue list --repo sustinbebustin/mws --state all --search "<keywords>"   # add to an existing issue instead of duplicating it
gh issue create --repo sustinbebustin/mws --title "<what went wrong>" --body "<details>"
```

In the body, give `mws version`, the exact command, its exit code and output, what you expected, and any workaround. For a skill or docs problem, quote the passage and say what was wrong. Replace project names, repo URLs and paths with placeholders.

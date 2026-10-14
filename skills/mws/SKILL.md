---
name: mws
description: Use for any mws or meta workspace task, including creating a meta workspace, cloning or removing working copies, registering native repos, worktrees for parallel work, promoting files into the harness, env staging, and relinking harness symlinks. Use whenever the cwd sits inside a directory with a `.mws.toml` above it.
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
    .wt/04/                 worktree of this copy
```

## Rules

- Pass every input as a flag. Your shell has no terminal, so mws never prompts: a missing choice fails with exit 2 before changing anything, and the message shows the flag to add. Destructive commands need `--yes`.
- Results go to stdout, progress to stderr. `init`, `clone` and `wt new` print the new path as the last stdout line; `list`, `trash list` and every `wt` command take `--json`.
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
mws list --json                                       # copies; "current" marks cwd's
mws clone feature-x --no-optional --setup             # new peer copy, default branches, setup run
mws clone feature-x --with docs --no-setup            # include optional repo "docs", skip setup
cd "$(mws clone feature-x --no-optional --setup | tail -n1)"
mws rm feature-x --yes                                # to .trash/, restorable for 7 days
mws restore feature-x --yes                           # back from the trash (--as <name> if taken)
mws trash list --json
```

- `clone` needs `--setup` or `--no-setup` when the config has setup commands, and `--no-optional` or `--with <folder>` when it has optional repos. Choose `--setup` unless the user says otherwise: a copy without setup usually does not build.
- A working copy is a separate line of work. For short-lived parallel work inside one copy, use a worktree.

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
mws sync-env feature-x                                # .envs/ -> copy (overwrites the copy's env files)
mws stage-env feature-x                               # copy -> .envs/ (overwrites staging)
```

- After adding a harness entry under `.mws/` by hand, run `mws relink` to link it into every copy.
- Editors that save atomically replace a symlink with a regular file. `relink` restores identical files on its own; a diverged one needs `--prefer peer` (the copy's version wins), `meta` (the harness wins) or `skip`, which is the user's call.
- Env files are copied, not linked, so copies can differ (ports, keys). Treat both env commands as overwrites, and read the env file contents only when the task needs them.
- `sync-env` refreshes the copy, never its worktrees.

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

Include `mws version`, the command you ran, its exit code and output, and what you expected. Replace project names, repo URLs and paths with placeholders.

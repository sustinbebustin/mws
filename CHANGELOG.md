# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Worktrees. `mws worktree` (alias `mws wt`) creates a worktree of a working copy at `<copy>/.wt/<name>/`: a `git worktree` of every native repo on `wt/<name>` (started at the launching checkout's HEAD), the harness symlinks, the copy's root files and env targets copied in, and setup run. Subcommands: `new`, `ls [--all]`, `path`, `exec`, `rm [--force] [--keep-branch]` and `prune [--dry-run]`; all non-interactive, with `--json` and exit codes `0` ok, `1` failure, `2` usage, `3` refused by a safety check. `rm` never discards uncommitted work or an unmerged branch, and never removes a worktree a foreign process runs in. See ADR 0011.
- Optional `[worktrees]` table in `.mws.toml`: `dir` (default `.wt`), `branch_prefix` (default `wt/`), `repo_setup` (default `true`) and `[[worktrees.setup]]` commands. A config that omits the table round-trips unchanged.
- Hidden `mws wt hook create|remove|subagent-stop` adapt Claude Code's `WorktreeCreate`, `WorktreeRemove` and `SubagentStop` hooks, so `claude -w`, `EnterWorktree` and `isolation: "worktree"` subagents use the same layout; a finished subagent's worktree is removed when it holds no work. `mws init` now writes the three hook entries into the skeleton's `.claude/settings.json`; existing workspaces add them by hand.

- Every command runs headless. Without a terminal on stdin mws never prompts: a missing input fails with exit `2` and the invocation that supplies it, and a confirmation needs `--yes`. New flags: `mws init --description --copies-dir --parent --repo --yes`, `mws clone --no-optional`, `mws relink --prefer peer|meta|skip`. See ADR 0012.
- `--dry-run` on `mws rm`, `mws trash prune`, `mws trash empty`, `mws migrate`, `mws relink` and `mws wt rm`. `--json` on `mws list` and `mws trash list`. Aliases `mws ls`, `mws trash ls` and `mws wt list`. Every command's `--help` shows examples.

### Changed
- Results go to stdout and progress to stderr for every command. `mws init` and `mws clone` print the new working copy's path as the last stdout line; the `Next: cd <path>` hint is gone.
- One exit-code scheme for the whole CLI: `0` ok, `1` failure, `2` usage (unknown commands, including under `mws wt` and `mws trash`, unknown flags, bad arguments, conflicting flags), `3` refused, `4` not found. `mws rm`, `mws restore`, `mws wt path`, `wt exec` and `wt rm` exit `4` (JSON code `not_found`) for a missing name, instead of `1`; `rm` and `restore` say when an earlier run already removed or restored it.
- Retries finish the job: `clone` of an existing copy clones only the repos it lacks and runs their setup (all setup again with `--setup`), `add-repo` of the same URL fills in copies that lack it, `promote` of a promoted entry succeeds, `wt rm a b` skips names already removed, and `relink` no longer rewrites links that are already correct.
- `mws relink` inspects every copy first and refuses, changing nothing, when a diverged file would need a prompt without a terminal or when `--prefer peer` would overwrite one copy's edit with another's. It exits non-zero when any working copy fails, after relinking the others.
- `mws list` shows `(+N worktrees)` per working copy. `mws rm <copy>` refuses while one of its worktrees has a live process and lists its worktrees in the confirmation. `mws restore` repairs the worktrees' git links, `mws relink` refreshes harness links in worktree roots, and `mws trash prune|empty` prune stale meta worktree registrations. `mws sync-env` still updates only the working copy.

## [0.5.0] - 2026-08-11

### Added
- Soft-delete for working copies. `mws rm` now moves a working copy into `<meta>/.trash/` instead of deleting it, so an accidental removal no longer costs uncommitted work or staged env files. The move is a single rename -- instant, and file modes and symlinks are preserved exactly. See ADR 0010.
- `mws restore [name]` brings a trashed working copy back and repairs its harness symlinks. `--as <newname>` restores under a different name; restore never overwrites an existing copy. With no name, it prompts with the contents of the trash.
- `mws trash` (alias `mws trash list`) shows what is in the trash and when each entry expires. `mws trash prune` purges entries past their retention window; `mws trash empty` purges everything.
- Optional `[trash]` table in `.mws.toml`: `retention_days` (default `7`, `0` means keep forever) and `disabled` (`true` restores the old delete-outright behaviour). A config that omits the table round-trips unchanged.

### Changed
- `mws rm` gained `--purge` to delete a working copy outright, skipping the trash. Its confirmation prompt now states which of the two it is about to do.
- Trashed entries are purged opportunistically, when `mws rm`, `mws restore`, or `mws trash` runs -- there is no background timer, so an entry outlives its retention until mws is next run in that workspace. `mws trash prune` forces a sweep.

## [0.4.2] - 2026-08-04

### Changed
- Homebrew formula now publishes to the shared tap `sustinbebustin/homebrew-tap` instead of the project-specific `sustinbebustin/homebrew-mws`. Install with `brew install sustinbebustin/tap/mws`. GitHub redirects the old tap name, so existing installs keep working until they re-tap.

## [0.4.1] - 2026-08-04

### Changed
- The `mws clone` optional-repos prompt now shows the keybinding help footer (space to select, enter to confirm, none to skip) and labels each repo by folder name only, dropping the URL.

## [0.4.0] - 2026-06-12

### Added
- Optional repos: register native repos under `[[optional_repos]]` in `.mws.toml` that are not cloned into every working copy. Pull one into a copy on demand -- at `mws clone` time via a multiselect prompt or `--with <folder>` (repeatable), or into an existing copy with the new `mws include <folder> [copy]`. Register them with `mws add-repo --optional <url> [folder]`, which appends without cloning into existing copies. Optional repos reuse the repo shape, so `[[optional_repos.envs]]` and `[[optional_repos.setup]]` work as for `[[repos]]`. See ADR 0009.
- `mws shell-init <zsh|bash|fish>` prints a shell function that wraps `mws` and auto-cds the parent shell into the new working copy after a successful `mws clone`. Opt-in via `eval "$(mws shell-init zsh)"` (or the equivalent for bash/fish). Without the wrapper, `mws clone` prints a copy-pasteable `Next: cd <path>` hint instead. The handoff uses `MWS_CD_FILE` -- a documented contract any IDE or script can also use. See ADR 0008.

## [0.3.0] - 2026-05-19

### Changed
- **Breaking.** `mws clone <name>` now always runs a fresh `git clone <url> <dst>` for every registered native repo. The `git clone --local` optimisation from a sibling working copy (with post-clone `origin` retarget) has been removed, so the new copy never inherits divergent `.git/` state from a donor. `repo.url` is strictly required at clone time; a missing URL is a per-repo error (`"<folder>: no remote URL configured in .mws.toml"`).

### Removed
- Internal helpers `git.CloneLocal` and `git.SetRemoteURL`, and the donor-selection logic in `mws clone`, are gone with the `--local` optimisation. Recorded as an inline update on ADR 0003.

## [0.2.0] - 2026-05-19

### Added
- Optional `working_copies_dir` top-level key in `.mws.toml`. When set, `mws clone` and `mws init` place working copies at `<meta>/<working_copies_dir>/<name>/` instead of directly under the meta root; every other working-copy-aware subcommand (`list`, `rm`, `relink`, `add-repo`, `promote`, `sync-env`, `stage-env`) honors the same root. Default empty preserves prior behavior.
- `mws init` now prompts for the working-copies subdirectory (optional) alongside the project name, description, and parent directory.

### Changed
- `mws list`, `mws relink`, and `mws rm` now surface a clear parse error when `.mws.toml` is malformed instead of silently scanning the meta root with default values.

## [0.1.2] - 2026-05-19

### Fixed
- `mws clone` now retargets the new working copy's `origin` to the URL declared in `.mws.toml` after a `git clone --local` from a sibling. Previously the new clone's `origin` pointed at the local sibling directory, so `git push`/`pull`/`fetch` missed the canonical remote.

### Added
- Root `.gitignore` covering goreleaser output, the root-level `mws` binary, test/coverage artifacts, per-user agent dirs, and editor/OS cruft.

## [0.1.1] - 2026-05-17

### Changed
- Homebrew formula now lives at the canonical `Formula/mws.rb` path in the tap. Existing `brew install sustinbebustin/mws/mws` continues to work unchanged.

## [0.1.0] - 2026-05-17

### Added
- MIT license.
- `mws version` subcommand prints the version, commit SHA, and build date (populated at release time).
- GitHub Actions CI runs `go build`, `go test`, `golangci-lint`, and `govulncheck` on Linux and macOS for every pull request and push to `main`.
- Tag-driven release pipeline: pushing a `v*` tag publishes darwin and linux binaries (amd64 and arm64) to GitHub Releases via goreleaser, with checksums and a conventional-commit-grouped changelog.
- Homebrew tap: every release pushes a formula to `sustinbebustin/homebrew-mws`, enabling `brew install sustinbebustin/mws/mws`.

[Unreleased]: https://github.com/sustinbebustin/mws/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/sustinbebustin/mws/releases/tag/v0.5.0
[0.4.2]: https://github.com/sustinbebustin/mws/releases/tag/v0.4.2
[0.4.1]: https://github.com/sustinbebustin/mws/releases/tag/v0.4.1
[0.4.0]: https://github.com/sustinbebustin/mws/releases/tag/v0.4.0
[0.3.0]: https://github.com/sustinbebustin/mws/releases/tag/v0.3.0
[0.2.0]: https://github.com/sustinbebustin/mws/releases/tag/v0.2.0
[0.1.2]: https://github.com/sustinbebustin/mws/releases/tag/v0.1.2
[0.1.1]: https://github.com/sustinbebustin/mws/releases/tag/v0.1.1
[0.1.0]: https://github.com/sustinbebustin/mws/releases/tag/v0.1.0

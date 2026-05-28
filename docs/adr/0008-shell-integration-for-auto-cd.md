# Shell integration for auto-cd after `mws clone`

Add an optional shell-function wrapper, distributed via a new `mws shell-init <zsh|bash|fish>` subcommand. With the wrapper installed, `mws clone <name>` lands the user's shell inside the new **Working copy** on success. Without it, `mws clone` prints a copy-pasteable `Next: cd <abs path>` hint after the final `[OK]` line.

The handoff channel is a tempfile path passed through `MWS_CD_FILE`. The wrapper creates the tempfile, sets the env var, runs the binary, and on a zero exit reads the path and `cd`s into it. The binary writes to the file only when the env var is set; nothing is emitted to stdout/stderr for the wrapper to parse.

The wrapper only intercepts `clone`. Every other subcommand is a pure passthrough via `command mws`.

## Why

`cd <copies-root>/<name>/` is the universal next step after every successful clone. The path is already known at the moment the command finishes -- forcing the user to copy or retype it is friction with no upside. The standard solution across CLIs of this shape (`zoxide`, `mise`, `nvm`, `broot`, `nnn`, `pyenv`, ...) is a one-time-installed shell wrapper. A child process cannot change its parent shell's working directory, so this is the only mechanism that actually delivers auto-cd.

The opt-in shape keeps the binary's behavior identical for non-adopters (CI, scripts, IDE invocations, users who never run the install line). They get a hint line instead of an auto-cd; correctness and exit codes are unchanged.

## Considered options

- **Print a marker line on stdout/stderr that the wrapper greps for** (e.g. `__MWS_CD__:<path>`). Rejected -- couples machine-readable output with human output, breaks composition with pipes/`tee`, and forces the wrapper to capture and re-emit stdout to avoid swallowing it. The tempfile channel keeps these planes separate.
- **Spawn a nested subshell into the new dir** (`exec $SHELL` with cwd set after clone). Rejected -- nested shells break command history, surprise users who type `exit`, break non-interactive use, and trap users in a child process. Anti-pattern.
- **Hint-only (no wrapper at all).** Rejected -- the explicit goal is auto-cd for interactive users. A hint is the right fallback when the wrapper isn't installed, not the headline behavior.
- **Bundle wrapper code as a separate file shipped via Homebrew/release archive.** Out of scope. `mws shell-init` is the smallest distribution surface and keeps a single source of truth in the binary. Revisit if the wrapper grows large enough to warrant per-shell completion-style packaging.

## Consequences

- The shell wrapper detects `clone` by exact match on `$argv[1]` (POSIX `$1`). mws has no global flags today (see `internal/commands/root.go`), so `mws clone foo` and `mws clone --setup foo` both work. If a global flag is added later (e.g. `mws --quiet clone foo`), the wrapper will miss the dispatch and `clone` will not auto-cd -- the binary still emits the hint as a fallback. Update the wrapper template when introducing the first global flag.
- `MWS_CD_FILE` becomes a documented contract: any third-party process (IDE plugin, custom wrapper, build script) can set `MWS_CD_FILE=/some/path` before invoking `mws clone`, read the file afterwards, and use the path however it wants. Future commands that create a new directory the user wants to enter (none planned today) can adopt the same channel.
- Behavior diff is invisible in scripted/CI contexts -- those never set `MWS_CD_FILE` and won't `eval` the shell-init output. The new `Next: cd <path>` line appears as a `[dim]` Info line, consistent with the rest of mws's UI.
- The wrapper is the user's shell function, not mws code. If a user customises it (e.g. to also `git pull` on entry), they own the divergence -- mws doesn't track installed wrapper versions.

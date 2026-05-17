# Post-clone setup commands

Add an optional `[[repos.setup]]` array to each `[[repos]]` entry in the **mws config**. Each entry has a single `cmd` field (shell string). `mws clone` runs the configured commands as a final step, gated by a single prompt (or `--setup` / `--no-setup` flags), using `sh -c` with cwd set to the cloned native repo inside the new **Working copy**. Intra-repo commands are sequential and stop at the first failure; failures in one repo do not block other repos.

```toml
[[repos]]
  folder = "frontend"
  url    = "git@github.com:example/frontend.git"

  [[repos.setup]]
    cmd = "pnpm install --frozen-lockfile && pnpm build"

[[repos]]
  folder = "backend"
  url    = "git@github.com:example/backend.git"

  [[repos.setup]]
    cmd = "go build ./..."
```

## Why

The same toolchain steps (`pnpm install`, `go build`, codegen, ...) are run by hand after every `mws clone`. Capturing them in the meta workspace's config makes a peer working copy reproducibly ready-to-use in one command, without duplicating those steps into a justfile, ad-hoc bootstrap script, or operator memory.

## Considered options

- **Workspace-level commands (`[[setup]]` at the meta root).** Rejected -- the motivating example is per-repo (frontend vs backend toolchains), and per-repo gives implicit cwd. A workspace-level form would force an explicit `cwd` field on every entry and lose the regularity with `[[repos.envs]]`.
- **Argv arrays instead of a shell string.** Rejected -- users already think in shell syntax (`pnpm install && pnpm build`). Argv arrays would force splitting chains across many TOML entries and would lose env-var expansion and pipes. The cost is `sh -c`'s usual sharp edges (quoting, escaping), accepted as a worthwhile trade.
- **Lifecycle hooks (pre-clone, post-clone, pre-env, post-env, ...).** Rejected -- a single post-clone trigger covers every concrete use case raised. Introducing a hook framework before a second trigger exists is speculative generality and grows the config surface for no current payoff.
- **Per-command prompts or a multi-select UI.** Rejected -- users either trust their config and want everything to run, or want to skip entirely and run by hand. A single yes/no matches that bimodal intent and keeps the CLI terse.
- **Capture output, show only on failure.** Rejected -- long-running commands (`pnpm install`, `go build`) would appear hung. Streaming child stdout/stderr to the terminal as they run keeps the user informed and aborts naturally on Ctrl-C.
- **A `mws setup [copy]` re-run subcommand mirroring `mws sync-env`.** Out of scope. Re-runs are still possible by running the commands manually. Revisit only if real usage shows the re-run command is missed.

## Consequences

- Setup commands inherit the user's existing PATH and environment. mws does not own toolchain provisioning; if a working copy needs `node 20` but the shell has `node 18`, setup fails the same way running the command by hand would.
- `sh -c` is required at the runtime end. Windows-without-sh is unsupported for setup commands; this matches the rest of mws's posix-shell assumptions (git, symlinks, `git clone --local`).
- A non-zero exit from `mws clone` after a setup failure is intentional: the working copy was created and linked, but readiness was not achieved. The user can read the streamed output, fix the issue, and re-run the failing command manually.
- The schema is an array of tables (`[[repos.setup]]`) so commands have an ordered, easily-extended shape. Adding fields (timeout, env, shell override, name) later is non-breaking.
- The CLI source stays domain-free: setup commands are user-authored strings, never project-specific Go code paths.

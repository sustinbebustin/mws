# Headless CLI contract

Every mws command can run without a person at a terminal, which is how agents and scripts call it. The rules:

- **Prompts are a fallback.** Every input a command prompts for has a flag or argument. A command prompts only for what was not given, and only when stdin is a terminal (`canPrompt`). `mws init` gained `--description`, `--copies-dir`, `--parent`, `--repo <url>` / `--repo <folder>=<url>` and `--yes`; `mws clone` gained `--no-optional`; `mws relink` gained `--prefer peer|meta|skip`.
- **No terminal, no prompt.** When stdin is not a terminal and an input is missing, the command fails before changing anything, with exit 2 and the invocation that supplies the input (`noPrompt`). A confirmation counts as an input: destructive commands need `--yes`. Inputs with a documented default (init's description, copies dir and parent) take it instead.
- **Results on stdout, progress on stderr.** The console reporter and streamed git and setup output go to stderr. stdout carries the result: `init` and `clone` print the new working copy's path as the last line, `list` and `trash list` print their rows, and both take `--json`. The `worktree` commands already worked this way.
- **One error taxonomy.** Every command's errors carry an `internal/clierr` Kind: `0` ok, `1` failure, `2` usage, `3` refused by a safety check, `4` not found; `--json` error codes are the Kind's name. Unknown commands (at the root and under a group such as `mws wt`) and flags, bad arguments and conflicting flags are usage errors everywhere. When several errors are joined (relink across copies), any real failure makes the whole a failure, so it is never reported as a usage mistake.
- **Retries are safe.** Re-running a command that succeeded, or half-succeeded, finishes the job or says it is done: `clone` of an existing copy clones only the repos it lacks and runs their setup (all setup again with `--setup`), `add-repo` of the same URL and folder fills in copies that lack the repo, `include` and `promote` of something already there succeed, `relink` leaves current links alone. Removing what is already gone exits 4: `rm` and `restore` say when the name was trashed or restored before, so a retry can tell that from a typo; `wt rm` skips names already removed and exits 4 only when every name is gone.
- **Inspect, then change.** A command that touches several things decides everything first. `relink` scans every copy and refuses, changing nothing, when a diverged file would need a prompt without a terminal or when `--prefer peer` would move different versions of one file into the harness (only the last would survive). `clone` resolves optional repos and the setup answer before creating anything.
- **Preview before destroying.** `--dry-run` runs the same checks and changes nothing on `rm`, `trash prune`, `trash empty`, `migrate`, `relink` and `wt rm` (and, as before, `wt prune`).
- **Help teaches by example.** Every command's `--help` has an Examples block of real invocations.

## Why

mws's main callers are coding agents running it through a shell tool with no terminal. Before this, most commands fell back to a `huh` prompt for any missing input: without a terminal that failed with `could not open a new TTY`, which names no flag, and with a terminal device present it waited for input. `mws init` and `mws clone` with optional repos could not be completed at all, and `relink` could not settle a diverged file. Progress went to stdout mixed with results, so `$(mws clone ...)` was unusable, and only `mws wt` distinguished usage errors from failures.

## Considered options

- **Detecting the terminal by opening `/dev/tty`, as huh does.** Rejected. An agent's shell often has a controlling terminal it cannot type into; stdin being a terminal is the signal that someone is there to answer.
- **Defaulting every missing choice without a terminal** (no optional repos, skip setup, keep diverged files). Rejected for choices that change the outcome: an agent that forgot `--setup` would get a working copy that silently does not build. Failing with the flag to pass costs one retry. Values with a documented default, which the prompt pre-fills anyway, do take it.
- **Making `rm`, `restore` or `wt rm` of a missing name succeed.** Rejected. A typo would then look like a successful removal. Exit 4, with a message naming the earlier trash entry or restored copy when there is one, lets a retrying caller treat it as done while a person still sees the mistake.
- **Keeping the worktree error kinds in `internal/worktree`.** Rejected once every command needed them: a second, command-level usage type made `mws rm` produce exit 2 from one type and exit 3 from the other, and `--json` errors outside `wt` encoded as `failure`. `internal/clierr` is the one taxonomy; `worktree` and `commands` both tag errors with it.
- **A global `--json` on the root command.** Rejected for now. `--json` exists where a command has a structured result (`list`, `trash list`, every `wt` command); a flag that most commands would ignore is noise in `--help`.
- **Keeping cobra's `MarkFlagsMutuallyExclusive`.** Replaced with `exclusiveFlags`. Cobra returns its error before `RunE` and outside `FlagErrorFunc`, so it could not be tagged as a usage error.

## Consequences

- Scripts that parsed stdout from `clone` ("Working copy ready at ...", "Next: cd ...") must read the last stdout line instead; the progress lines moved to stderr and the `Next: cd` hint is gone. `MWS_CD_FILE` is unchanged.
- `mws wt path`, `wt exec` and `wt rm` now exit 4, not 1, for a name with no worktree; the JSON error code is `not_found`. `wt rm` output gains `not_found: [...]`, and `wt rm --dry-run` returns `{dry_run, removed: [{name, path}], not_found}` from `worktree.Check`. `mws rm` and `mws restore` of a missing name exit 4, not 1.
- `relink` now exits non-zero when any working copy fails, after relinking the rest. Before, it reported the failure and exited 0. `--prefer peer` refuses (exit 2) when copies hold different versions of one file.
- `mws wt bogus` and `mws trash bogus` exit 2; before, they printed the group's help and exited 0.
- `project.LinkHarnessIntoWorkingCopy` no longer rewrites a symlink that already points at the right target, and returns only the names it created or repointed. `project.PendingHarnessLinks` reports the same set without writing, for `relink --dry-run`.
- Tests that need the no-terminal path set it with `noTerminal(t)`; `canPrompt` is a package variable for that reason.

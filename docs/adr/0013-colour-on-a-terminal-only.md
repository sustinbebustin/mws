# Colour on a terminal only

mws colours its output per stream and changes nothing else about it. On a terminal, headings and the current working copy are blue, `[OK]`, `[WARN]` and `[FAIL]` are green, yellow and red, secondary lines are faint, and the `mws ...` invocation inside an error is bold. Off a terminal, the same bytes without the escape codes. `internal/ui` holds the decision (`ui.Detect(w)`, one per stream), and its test asserts the invariant for every op: stripping the escape codes from the coloured output gives the plain output.

The rules:

- **Per stream, not per process.** stdout decides how a result looks, stderr how progress looks, stdin whether mws may prompt (ADR-0012). `cd "$(mws clone ...)"` keeps coloured progress on a terminal stderr while stdout stays plain.
- **Rich is plain plus SGR codes.** No rendering adds, removes or rearranges text. A person and an agent describing `mws list` describe the same lines, and a paste into an issue reads like the terminal did.
- **The terminal's own palette.** Colours are ANSI 9 to 12 and faint, so they follow the user's theme and stay legible on light and dark backgrounds. `NO_COLOR` turns them off. mws never asks the terminal for its background colour: the query stalls or leaks escape sequences wherever nothing answers it.
- **Tokens never change.** `[OK]`, `[WARN]` and `[FAIL]` are styled, not replaced, so output greps the same everywhere. The final error now carries `[FAIL]` in both modes, matching the progress lines.
- **`--json` is untouched.** JSON never goes through `ui`.

## Why

Agents are the main callers, and ADR-0012 shaped the contract for them. People running mws by hand got cobra's default help, five colours decided by whether *stdout* was a terminal (so `$(mws clone ...)` lost colour on stderr), unthemed `huh` prompts, and `user aborted` on ctrl-c. The change fixes those and groups the help, and stops there.

## Considered options

- **A rich layout layer** (titled rules for headings, bordered panels for plan summaries, tables for `list`, `trash list` and `wt ls`, a workspace dashboard on bare `mws`, an exit-code trailer on errors), with plain output kept byte-identical for pipes. Built, reviewed, and removed. The two branches showed different content, so a person and an agent would describe one command differently; the tables ignored the terminal width and wrapped at 80 columns; the dashboard scrolled off under 50 lines of help; the exit-code trailer informed nobody, since only scripts care about exit codes and scripts never see a terminal; and every op had two implementations with tests for one. The structure also worked against the stated preference for low-noise output. git, gh and cargo colour their output and otherwise print the same text everywhere; that is the model kept.
- **A brand palette in truecolour**, sampled from the logo. Rejected: the lavender measured 1.7:1 on white, the slate under 4.5:1 on common dark themes, and fixed hex colours override the user's terminal theme. Adaptive light/dark colours were tried first and rejected because lipgloss resolves them by querying the terminal, which blocks for five seconds under a pseudo-terminal that does not answer.
- **Spinners that hide git and setup output.** Rejected. git prints its own progress and setup output is what you read when it fails. A step that takes a second or more gets its duration on its `[OK]` line instead.
- **An env flag or `--plain` to choose the mode.** Rejected. Terminal detection is the right default and needs no setup; `NO_COLOR` already exists.

## Consequences

- `Reporter` gains `Done(msg, took)` for timed steps and `Summary(title, rows)` for plan blocks; `nopReporter` and the test reporters implement both.
- Every prompt goes through `runForm`/`runField` in `prompt.go`, which apply the theme, show the key help and turn ctrl-c into `Cancelled.` with exit 1 (before: `user aborted`).
- `cmd/mws` prints the final error through `commands.PrintError`, so it starts with `[FAIL] ` in every mode.
- Root commands are grouped in help: Workspace, Working copies, Env files, Worktrees, Shell and info.
- Importing `huh` queries the terminal background once at process start, before any mws code runs; a terminal answers it instantly, a non-answering pseudo-terminal costs the five-second timeout. That predates this change and is not fixed by it.

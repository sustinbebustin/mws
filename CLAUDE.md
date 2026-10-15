# mws -- agent notes

A Go CLI for managing **meta workspaces** around independent native git repos. Intentionally domain-free: no project-specific code paths, no branching on project names. The same rule applies to `skeleton/`, since it ships in a public binary.

Use `go build ./...` to verify compilation and `go test ./...` for tests. Run `make ci` before pushing to mirror the full CI matrix (build, test, lint, govulncheck).

When changing public-facing behavior, check [README.md](./README.md) and [docs/config.md](./docs/config.md) to see if documentation needs updating. For non-trivial design decisions, add an ADR following the format of existing entries.

## Config schema

`.mws.toml` schema, rationale, and a worked example: [docs/config.md](./docs/config.md). Round-trip via `BurntSushi/toml`; unknown keys are dropped on write.

## Skeleton edits

`skeleton/` is the source of truth for the harness template, embedded via `embed.go` (`//go:embed all:skeleton` -- the `all:` prefix is required for dotfiles). `.tmpl` files use Go `text/template`; the extension is stripped on render. `.gitkeep` files are skipped. Edits ship in the next build.

## Subcommand conventions

- Resolve the meta with `project.Locate(cwd)`; never duplicate the walk-up logic.
- Reuse `project.LinkHarnessIntoWorkingCopy` for any symlink fan-out. It walks `<meta>/.mws/` and links every entry with no exclusions -- do not introduce a hardcoded list or skip filter.
- For commands that target a working copy, use `(*project.Workspace).ResolveCopy` (defaults to cwd's copy, rejects reserved names like `.mws`, `.envs`, `.git` via `project.ValidateName`).
- Never build a path under `<meta>/.trash/` by hand; go through `internal/trash`. Commands read the resolved `config.TrashPolicy()`, never the raw `*config.Trash` pointer.
- `huh` for prompts, through `runForm` / `runField` in `prompt.go`. Colour goes through `internal/ui`, bound per stream with `ui.Detect`; it only adds escape codes on a terminal and never changes the text (ADR-0013). Terse status lines with `[OK]` / `[WARN]` / `[FAIL]` prefixes.
- Errors as values; surface them through Cobra's `RunE`.
- Unit-test where breakage would be silent (parsing, symlinking, filesystem mutation). Skip TDD on Cobra glue and interactive prompts.

## Agent skills

### Issue tracker

Local markdown under `.scratch/<feature>/`. When work comes from a ticket, tick each acceptance checkbox as it passes and set `Status: resolved` at the end. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five roles (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`), used as `Status:` values. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout: [GLOSSARY.md](./GLOSSARY.md) at the repo root + ADRs in [docs/adr/](./docs/adr/). `GLOSSARY.md` is the canonical vocabulary (Meta workspace, Harness dir, Working copy, Native repo, Env staging, Setup commands, Skeleton, Promote). Use those terms verbatim in code, docs, and commit messages. See `docs/agents/domain.md`.

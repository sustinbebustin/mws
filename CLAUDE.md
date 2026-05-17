# mws -- agent notes

`mws` is a Go CLI for managing meta workspaces around independent native git repos. It is intentionally **domain-free**: no project-specific code paths, no branching on project names.

## Source layout

```
cmd/mws/main.go             # binary entry
embed.go                    # //go:embed all:skeleton -- SkeletonFS lives here
skeleton/                   # template content distributed to new meta workspaces
  .gitignore                # allowlist .gitignore for the meta repo root
  README.md.tmpl            # meta-root README
  .mws/                     # harness contents that fan out into working copies
internal/
  commands/                 # Cobra command wiring, prompts, output
  config/                   # .mws.toml load/save
  project/                  # locate meta, enumerate working copies, harness symlink helper
  git/                      # thin git wrappers
  skeleton/                 # render embedded skeleton to disk (text/template for *.tmpl)
docs/adr/                   # architecture decision records
```

## Layout produced by `mws init <name>`

```
<parent>/<name>/            # meta workspace (git repo at its root)
  .git/                     # tracks the harness and config only
  .gitignore                # allowlist: tracks .gitignore, .mws.toml, .mws/, README.md
  .mws.toml                 # mws config (project name, repos, env mappings)
  .mws/                     # harness; every entry symlinks into every working copy
    CLAUDE.md
    .claude/
    .workspace/
    justfile
    ...
  .envs/                    # env staging (per-repo flat-named files), untracked
  main/                     # first working copy, untracked
    <repo>/                 # native git clone(s) live inside working copies
    CLAUDE.md -> ../.mws/CLAUDE.md
    ...
```

Working copies do NOT carry a `.mws` symlink back to the meta. `project.Locate` walks up from cwd looking for `.mws.toml`.

## Conventions

- Keep the CLI source domain-free. No project-specific names, paths, or branches in Go source -- ever. The skeleton ships in a public binary, so skeleton text must also stay generic.
- Symlink fan-out is dynamic. `project.LinkHarnessIntoWorkingCopy` walks `<meta>/.mws/` and symlinks every entry into the working copy with NO exclusions (the pure fan-out rule). Do NOT introduce a hardcoded list or skip filter.
- Use `huh` for prompts, `lipgloss` for styled output. Output should stay terse -- minimal status lines, `[OK]` / `[WARN]` / `[FAIL]` prefixes.
- TOML via `BurntSushi/toml`.
- Errors as values; surface them up through Cobra's `RunE`.

## Tests

```bash
go test ./...
```

Unit tests cover:
- `internal/config` -- TOML round-trip including `[[repos.envs]]`
- `internal/project` -- meta location (walk-up for `.mws.toml`), working-copy enumeration (filter dotfile dirs), harness symlink discovery
- `internal/git` -- init / clone --local / branch resolution (skipped if `git` missing)
- `internal/skeleton` -- render the embedded skeleton end-to-end with sample data
- `internal/commands` -- non-interactive paths of `init`, `migrate`, `sync-env`, `stage-env`, and relink's divergence-repair

Interactive prompts are not unit-tested.

## Adding a subcommand

1. New file in `internal/commands/<name>.go` with `newXxxCmd() *cobra.Command`.
2. Wire it in `internal/commands/root.go` under `root.AddCommand(...)`.
3. Resolve the meta with `project.Locate(cwd)`; do not duplicate the search logic.
4. Reuse `project.LinkHarnessIntoWorkingCopy` for any symlink fan-out -- never hand-roll a hardcoded set of names.
5. If the command targets a working copy, use `resolveWorkingCopy` from `internal/commands/helpers.go` to validate and resolve the name (it defaults to the cwd's working copy and rejects reserved names like `.mws`/`.envs`/`.git`).
6. Tests where breakage would be silent (parsing, symlinking, filesystem mutation). Skip TDD on the Cobra glue and prompts.

## Editing the skeleton

`skeleton/` is the source of truth. The binary embeds it via `embed.go` (`//go:embed all:skeleton` -- the `all:` prefix is required for dotfiles to be embedded). Changes ship in the next build.

Placeholder syntax in `.tmpl` files is Go `text/template`: `{{.ProjectName}}`, `{{.Description}}`, `{{range .Repos}}...{{end}}`. The `.tmpl` extension is stripped on render. Files named `.gitkeep` are skipped (they only exist to keep otherwise-empty directories embeddable).

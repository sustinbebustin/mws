# mws -- agent notes

`mws` is a Go CLI for managing meta workspaces around independent native git repos. It is intentionally **domain-free**: no project-specific code paths, no branching on project names.

## Source layout

```
cmd/mws/main.go             # binary entry
embed.go                    # //go:embed skeleton/  -- SkeletonFS lives here
skeleton/                   # template content distributed to new meta workspaces
internal/
  commands/                 # Cobra command wiring, prompts, output
  config/                   # .mws/config.toml load/save
  project/                  # locate meta, enumerate peers, symlink discovery helper
  git/                      # thin git wrappers
  skeleton/                 # render embedded skeleton to disk (text/template for *.tmpl)
docs/adr/                   # architecture decision records
```

## Conventions

- Keep the CLI source domain-free. No project-specific names, paths, or branches in Go source -- ever. The skeleton ships in a public binary, so skeleton text must also stay generic.
- Symlink discovery is dynamic. `project.LinkMetaIntoWorkingCopy` walks the meta and links every top-level entry except `.git/`. Do NOT introduce a hardcoded list.
- Use `huh` for prompts, `lipgloss` for styled output. Output should stay terse -- minimal status lines, `[OK]` / `[WARN]` / `[FAIL]` prefixes.
- TOML via `BurntSushi/toml`.
- Errors as values; surface them up through Cobra's `RunE`.

## Tests

```bash
go test ./...
```

Unit tests cover:
- `internal/config` -- TOML round-trip
- `internal/project` -- meta location, peer enumeration, symlink discovery
- `internal/git` -- init / clone --local / branch resolution (skipped if `git` missing)
- `internal/skeleton` -- render the embedded skeleton end-to-end with sample data
- `internal/commands` -- non-interactive paths of `init` and `migrate`

Interactive prompts are not unit-tested.

## Adding a subcommand

1. New file in `internal/commands/<name>.go` with `newXxxCmd() *cobra.Command`.
2. Wire it in `internal/commands/root.go` under `root.AddCommand(...)`.
3. Resolve the meta with `project.Locate(cwd)`; do not duplicate the search logic.
4. Reuse `project.LinkMetaIntoWorkingCopy` for any symlink fan-out -- never hand-roll a hardcoded set of names.
5. Tests where breakage would be silent (parsing, symlinking, filesystem mutation). Skip TDD on the Cobra glue and prompts.

## Editing the skeleton

`skeleton/` is the source of truth. The binary embeds it via `embed.go`. Changes ship in the next build.

Placeholder syntax in `.tmpl` files is Go `text/template`: `{{.ProjectName}}`, `{{.Description}}`, `{{range .Repos}}...{{end}}`. The `.tmpl` extension is stripped on render. Files named `.gitkeep` are skipped (they only exist to keep otherwise-empty directories embeddable).

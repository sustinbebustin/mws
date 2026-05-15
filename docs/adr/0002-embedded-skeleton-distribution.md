# Embedded skeleton in a single Go binary

The **Skeleton** (template `.claude/`, `.workspace/`, `CLAUDE.md.tmpl`, etc.) is bundled into the `mws` binary via `//go:embed`. `go install github.com/sustinbebustin/mws@latest` installs the CLI and skeleton atomically. `mws init` writes the skeleton out to disk, rendering Go templates with values from prompts.

## Considered options

- **Separate template repo fetched from GitHub at init time.** Rejected: more moving parts, requires network at `mws init`, harder bootstrap on a fresh machine, drift risk between CLI versions and template repo versions.
- **`gh repo create --template` style distribution (no CLI).** Rejected once we added the parallel-working-copy command set -- a CLI is needed regardless of how content ships.

## Consequences

- Updating skeleton content requires cutting a new `mws` release. Acceptable given the user is the sole consumer and updates are expected to be infrequent.
- The repo has a `skeleton/` directory checked into source that is the source of truth; tests can run against it directly.

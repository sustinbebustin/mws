# Optional repos

Add a top-level `[[optional_repos]]` array to the **mws config**, holding **Native repo** entries (same shape as `[[repos]]`) that `mws clone` does *not* clone into every **Working copy**. They are pulled into a specific copy on demand:

- During `mws clone`, when any `[[optional_repos]]` exist, a multiselect prompt offers them, defaulting to none. `mws clone --with <folder>` (repeatable) selects specific ones non-interactively and skips the prompt.
- `mws include <folder> [working-copy]` clones one into an existing copy (the current copy by default).

They are registered with `mws add-repo --optional <url> [folder]`, which appends to `[[optional_repos]]` and -- unlike plain `add-repo` -- does **not** broadcast-clone into existing copies. A `folder` must be unique across both `[[repos]]` and `[[optional_repos]]`, since it maps to a single clone-target directory inside a copy. Optional repos reuse the full `Repo` shape, so `[[optional_repos.envs]]` and `[[optional_repos.setup]]` work exactly as for `[[repos]]`.

```toml
[[repos]]
  folder = "frontend"
  url    = "git@github.com:example/frontend.git"

[[optional_repos]]
  folder = "worker"
  url    = "git@github.com:example/worker.git"
```

## Why

The default repo set in `[[repos]]` is cloned into every working copy. But a given feature in one copy sometimes needs an additional repo -- a worker service, a sibling library, a one-off dependency -- that the other copies do not. Today those are cloned by hand (`git clone`) into the copy or referenced from an out-of-tree directory: untracked, unnamed, and easy to get wrong. Registering them as named optional repos makes the occasional repo first-class and reproducible without inflating the default clone set every copy pays for.

## Considered options

- **A `bool` "optional" flag on `Repo` inside the single `[[repos]]` list.** Rejected -- every place that loops `cfg.Repos` to clone (clone, add-repo's broadcast) would need a `!optional` filter, and forgetting one would silently clone an optional repo into every copy. A separate `OptionalRepos` field makes the default-clone set unrepresentable-as-wrong: no default-clone loop can ever touch an optional repo.
- **Auto-register on `mws include <folder> <url>`.** Rejected -- it blends registration into the include flow and makes the catalog implicit. Keeping registration explicit (`add-repo --optional`) keeps `.mws.toml` the single source of truth; `include`/`--with` on an unknown folder hard-errors and points at the register command.
- **A dedicated `add-optional-repo` command.** Rejected in favour of `add-repo --optional` -- it is the same data (a `Repo` entry) with the same URL/folder prompting and validation, and the flag keeps the top-level command list tight. The one behavioral difference (no broadcast clone) is documented in the command help.
- **Per-copy prompts listing every optional repo on every clone, defaulting to all.** Rejected -- optional repos are occasional by definition. The multiselect defaults to none so the common case (don't include any) is a single Enter, and `--with` covers the scripted case.
- **Tracking which optional repos a copy contains (e.g. in state).** Out of scope. A cloned optional repo is an ordinary native repo on disk; mws does not need a registry of per-copy membership. `mws sync-env`/`stage-env` still iterate `[[repos]]` only, so an optional repo's env is materialised at clone/include time but not re-synced later. Revisit only if real usage shows that is missed.

## Consequences

- `optional_repos` uses `omitempty`: existing configs load unchanged and the key is absent until an optional repo is registered. Unknown keys are still dropped on write (the standing round-trip caveat), so the field had to be a real struct member, which it now is.
- `folder` uniqueness is enforced across both lists at registration time (`AddRepo`/`AddOptionalRepo` share a `folderRegistered` check). A folder used by a default repo cannot be reused for an optional one and vice versa.
- Including an optional repo reuses the exact clone pipeline (`cloneNative` + env copy + setup) as default repos, so default-branch checkout, env materialisation, and setup-command behavior are identical. At `mws clone` time a selected optional repo's setup folds into the single setup confirmation alongside the default repos.
- `mws include` resolves the target copy with the same helper as the env commands, so it defaults to the current working copy and rejects reserved/dot names.
- The CLI stays domain-free: optional repos are user-authored config entries, never project-specific Go code paths.

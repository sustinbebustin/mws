# Env staging via copy with config-driven mapping

The **Meta workspace** holds an **Env staging** dir at `.envs/`, organized per **Native repo** (`.envs/<repo>/<flat-name>`). Each `[[repos.envs]]` entry in the **mws config** maps a staged source file to a target path inside a working copy's native repo:

```toml
[[repos]]
  folder = "frontend"
  url = "..."

  [[repos.envs]]
    source = "apps-internal.env"
    target = "apps/internal/.env"
```

On `mws clone`, staged files are **copied** (not symlinked) into the new working copy at their mapped targets. Each working copy's env files are independent of staging after creation -- subsequent edits in one copy do not affect siblings. `mws sync-env <copy>` re-pushes from staging to overwrite a copy's env files (resetting to staged defaults). `mws stage-env <copy>` does the inverse: captures a copy's env files into staging per the config mappings, making them the new default for future clones.

Env staging lives outside `.mws/` (so the pure fan-out rule for the harness is preserved) and is automatically untracked by the meta's allowlist gitignore. Env values are local-only; the meta repo never carries secrets.

## Considered options

- **Symlink env files instead of copy.** Each working copy's env file would be a symlink to the staged source. Rejected: forces locked-step values across all copies, which breaks legitimate per-copy divergence -- running two dev servers concurrently on different ports, or pointing one copy at staging credentials while another stays on local. Symlinks are the wrong primitive for env files specifically because divergence is a feature, not a bug.
- **Convention-mirror staging (no config mapping).** Mirror the native repo's directory tree under `.envs/`, e.g. `.envs/frontend/apps/internal/.env`. The staging path IS the mapping; no config entries needed. Rejected: deeper staging trees, every native-repo restructure has to be re-mirrored, and the staging layout becomes coupled to a specific moment in the native repos' history. Config-driven mapping decouples the two.
- **Versioning env files in the meta repo.** Track staged env files in git so they survive across machines and team members. Rejected: env files commonly contain secrets, and env values come from secrets managers / coworker handoff / manual copy. mws solves replication across local clones, not cross-machine distribution.
- **No mws involvement.** Document a convention and let the user manage env propagation by hand. Rejected: the entire motivation for env staging was to make replication painless; punting it back to a manual cp/scp loop defeats the purpose.

## Consequences

- A fresh `mws init` produces an empty `.envs/` -- mws cannot manufacture env values. The user populates env files in the first working copy by hand (paste from secrets manager, copy from another machine, hand-fill from a `.env.example`), then runs `mws stage-env main` to capture them into staging. Subsequent `mws clone` calls reproduce them automatically.
- Per-copy divergence is the default. Editing `.env` in one working copy never affects another. Reset via `mws sync-env <copy>` if you want to discard drift; promote drift to the new default via `mws stage-env <copy>`.
- `[[repos.envs]]` entries grow with each env file managed. The mapping is the source of truth for which env files mws touches -- files not listed in the config are not synced.
- Tools that follow symlinks (next.js, vite, supabase CLI, etc.) are not exercised on env paths under this design, removing a small class of "tool resolved a symlink to canonical path and got confused" failure modes.
- Env staging mirrors the harness convention of "per-repo subdir under a meta-root dotfile dir" (`.envs/<repo>/` vs `.mws/`), keeping the meta-root structure regular: one dotfile dir per cross-cutting concern.

# Soft-delete working copies

`mws rm` no longer deletes a **Working copy**. It moves the directory into `<meta>/.trash/<id>/copy/` with a single `os.Rename` and records a sibling `entry.toml` holding the original name and the deletion time. `mws restore <name>` moves it back and repairs its harness symlinks; `mws trash list|prune|empty` inspects and manages the area. Entries past `trash.retention_days` (7 by default) are purged by an opportunistic sweep. `mws rm --purge` and `trash.disabled = true` keep the old hard-delete behaviour.

```
<meta>/.trash/
  20260811T175936Z-main/
    entry.toml        # name = "main", deleted_at = 2026-08-11T17:59:36Z
    copy/             # the working copy, moved in verbatim
```

## Why

`os.RemoveAll` in `mws rm` was the only destructive path in the CLI, and the thing it destroyed was the one part of a **Meta workspace** that is not recoverable from git: uncommitted work in the **Native repos**, plus the env files materialised out of **Env staging**. The two existing guards (copies-root membership, and a harness-symlink check) stop `rm` removing the wrong *kind* of directory; nothing stopped it removing the right kind by mistake. A working copy is cheap to recreate but its local state is not, so the default should be reversible.

## Considered options

- **Compressing trashed copies to `tar.gz`.** Rejected. A working copy's bulk is `.git` pack files and dependency trees, which barely compress, so the trade is a multi-gigabyte read-plus-write in exchange for little disk. It also turns an instant, atomic rename into a long operation that has to reproduce modes, symlinks, and hardlinks by hand -- new failure modes on the *delete* path, which is exactly where they are least welcome. A rename preserves everything by construction.
- **A background daemon or cron sweep for retention.** Rejected -- mws is a CLI with no resident process and no install-time hooks, and adding one for a tidying task would be the largest new moving part in the project. The sweep runs when an mws command touches the trash. The honest consequence, documented rather than hidden, is that an entry outlives its retention for as long as mws is not run in that workspace; `mws trash prune` forces one.
- **Sweeping on every command, including read-only ones like `mws list`.** Rejected -- it purges sooner, but putting an irreversible deletion behind a command that only reads is a surprise the extra promptness does not pay for. The sweep runs on `rm`, `restore`, and `trash`, all of which the user already invoked to act on the trash.
- **Trash at `<copies-root>/.trash` rather than the meta root.** Rejected -- `working_copies_dir` is a mutable config key, so a trash under it would be stranded (or silently split in two) the moment the key changed. The meta root is the stable anchor, alongside `.mws/` and `.envs/`.
- **A flat `trash_retention_days` key instead of a `[trash]` table.** Rejected -- retention and the on/off switch are distinct facts, and folding them into one integer means overloading a sentinel (`0` or `-1`) to mean "disabled". That makes "keep forever" and "never trash" indistinguishable in the schema. A table with `retention_days` and `disabled` keeps both representable and reads better in `.mws.toml`.
- **Deriving name and deletion time by parsing the entry directory name.** Rejected -- the id exists to be readable and to sort chronologically, not to be a data format. `entry.toml` is the ground truth, so an id can be renamed by hand without corrupting anything.

## Consequences

- The `[trash]` table is a `*Trash` pointer with `omitempty`, so an existing `.mws.toml` that never mentions trash round-trips without gaining the table. All callers read the resolved `Config.TrashPolicy()` rather than the raw pointer, so no command reasons about nil-vs-zero. A negative `retention_days` is rejected by `Config.Validate` at load time -- a typo must not silently mean "purge everything".
- `retention_days = 0` means keep forever, which is deliberately distinct from `disabled = true` (never trash in the first place).
- Harness symlinks are relative (`project.LinkHarnessIntoWorkingCopy`), so they dangle while a copy sits in `.trash/` at a different depth. `mws restore` re-runs the same helper on the way out, which is also what repairs a copy restored under a different name via `--as`. A dangling `ls -l` inside `.trash/` is expected.
- `.trash` is dot-prefixed, so `Workspace.EnumerateCopies` skips it and `project.ValidateName`'s leading-dot ban means no working copy can ever collide with it. The meta-root allowlist `.gitignore` already ignores it, so no `.gitignore` change was needed and existing workspaces need no migration.
- Entry ids are timestamp-first and reserved with `os.Mkdir`, which fails if the name is taken. Removing the same name twice within one second yields two distinct entries rather than one clobbering the other, so `mws restore` can offer a choice between them.
- If the rename fails with `EXDEV` -- the working copy sitting on a different filesystem from the meta root -- `rm` reports that and removes nothing, pointing at `--purge`. It does not fall back to a recursive copy: a "delete" that quietly rewrites gigabytes is worse than an honest refusal.
- A failure to write `entry.toml` rolls the copy back to its original path, so a metadata problem can never cost the user their files.
- `trash.List` skips unreadable entries and names them in a `[WARN]` line instead of erroring, so a hand-mangled `.trash/` cannot brick `mws rm`.
- Disk usage grows: removed working copies now occupy space for up to the retention window. `mws trash list` shows what is held and `mws trash empty` reclaims it immediately.

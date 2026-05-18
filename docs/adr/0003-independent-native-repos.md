# Independent native repos with `git clone --local` for peer copies

Each **Working copy** holds its own clone of each **Native repo** with its own `.git/` directory (independent HEAD, index, branches). `mws clone <new-peer>` populates the new peer's native repos with `git clone --local <invoking-peer>/<folder> <new-peer>/<folder>`, falling back to a remote clone (URL from `.mws/config.toml`) if `--local` fails. After a successful local clone, the new peer's `origin` is rewritten to the URL from `.mws.toml` so `git push`/`pull`/`fetch` target the canonical remote, not the invoking peer's directory. The new peer's native repos check out each repo's default branch, not the invoking peer's current branch.

## Why

Agents in peer working copies must not share `.git/` state. Two agents on the same `.git/` directory would conflict on index updates, ref writes, and reflog entries. Independent clones eliminate the conflict by construction. `git clone --local` hardlinks `.git/objects` so disk usage stays roughly constant regardless of peer count -- only divergent blobs cost real disk.

## Considered options

- **Shared `.git/` across working copies.** Rejected -- concurrent git ops conflict.
- **Git worktrees.** Rejected per user preference.
- **Submodules.** Wrong primitive -- pins commits in the parent repo, which doesn't apply when there is no parent repo tracking native repo state.
- **Remote-only clone for every new peer.** Rejected as default -- slow over the network, but kept as fallback when `--local` is unavailable or fails.

## Consequences

- Spinning up a new peer is fast (sub-second per native repo) and disk-cheap until divergence.
- Default-branch checkout on `mws clone` is a deliberate "fresh start for new work" semantic. A `--from-current-branch` flag can be added later if the no-default case ever matters.
- Hardlinked objects survive deletion of the source clone (the linked blocks are reference-counted by the filesystem).

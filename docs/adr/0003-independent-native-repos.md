# Independent native repos with `git clone --local` for peer copies

> Superseded in part by the Update 2026-05-19 below: the `--local` optimisation
> has been removed. Read the update first; the body of this ADR is preserved
> for history.

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

## Update 2026-05-19

The `git clone --local` optimisation has been removed. `mws clone <name>` now
runs `git clone <repo.url> <dst>` unconditionally for each registered native
repo -- no donor selection, no local-clone attempt, no fallback chain. `repo.url`
becomes strictly required at clone time; an empty URL is a hard error per repo
(`"<folder>: no remote URL configured in .mws.toml"`).

The original body above is kept for history. What changed in practice:

- The "independent `.git/` per working copy" invariant is unchanged -- that's
  still the whole point. Only the *seeding* mechanism for a fresh copy is
  different.
- `--local` hardlinks `.git/objects` from the donor, so the new copy inherits
  whatever divergent / stale / shallow / alternates-bearing state that donor
  happens to have. The only guarantee that the new copy reflected the canonical
  remote was a post-clone `git remote set-url`, which left every object in
  `.git/` sourced from the donor. The intent was "fresh start" -- so we make it
  actually fresh.
- `origin` is now set correctly by `git clone` itself at clone time, instead of
  being fixed up after the fact. The `chooseInvokingCopy`, `git.CloneLocal`, and
  `git.SetRemoteURL` helpers all go away with this change.
- The speed/disk win from `--local` is not worth the risk of inheriting donor
  `.git/` state. Network bandwidth is cheap; silent divergence is not.

The "Considered options" entry for "Remote-only clone for every new peer" is
the option now in effect.

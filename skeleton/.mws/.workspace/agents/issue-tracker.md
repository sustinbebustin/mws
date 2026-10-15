# Issue tracker: local markdown, copy-scoped

Issues and specs are markdown files under `.scratch/` at the **working copy root** -- the directory that contains the native repos and the `.workspace` symlink. That is normally your current working directory.

No CLI, no hosted tracker. Do not create, close, or label issues on the native repos' remotes.

`.scratch/` is deliberately not symlinked from `.mws/`, so it belongs to one working copy only. Removing a copy discards its tickets rather than leaving them for every other copy to trip over.

## Layout

```
<copy-root>/.scratch/
└── <feature-slug>/
    ├── spec.md                  # /to-spec output
    ├── map.md                   # /wayfinder map, if the effort uses one
    └── issues/
        ├── 01-<slug>.md
        └── 02-<slug>.md
```

## Conventions

- One feature or effort per directory: `.scratch/<feature-slug>/`.
- One file per ticket at `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01` in dependency order (blockers first). Never a single combined tickets file.
- Header lines near the top of each issue file:
  - `Type:` -- `bug` or `enhancement`
  - `Status:` -- a triage role or lifecycle state from `triage-labels.md`
  - `Blocked by:` -- the numbers of the tickets that gate this one, or `None`. A ticket is unblocked when every ticket it lists is `resolved`.
- Acceptance criteria are `- [ ]` checkboxes in the body. They are the ticket's definition of done; the `Status:` line is what tells another agent whether to pick the ticket up.
- Comments and conversation history append to the bottom under a `## Comments` heading.
- Numbers are scoped to the feature directory, so two working copies can both have an `01-` without colliding.

## When a skill says "publish to the issue tracker"

Write the file under `.scratch/<feature-slug>/`, creating the directory if needed. Then tell the user the paths you created -- there is no URL to hand them.

## When a skill says "fetch the relevant ticket"

Read the file at the referenced path. The user normally passes a path or a `<feature-slug>/NN` reference. A bare `#42` is meaningless here; ask which feature they mean rather than guessing or falling back to a hosted tracker.

## Working a ticket

Whenever work traces back to a `.scratch/` ticket -- `/implement` on a spec, a single ticket picked up by hand, an AFK run -- the ticket file is updated as part of the work, not after it:

1. **Tick as you verify.** Flip a criterion to `- [x]` when that criterion actually passes -- the test is green, the migration applies, the check came back clean. Ticking the list in one pass at the end records an intention rather than a result.
2. **Resolve.** When every criterion is `- [x]`, set `Status: resolved`.

A criterion that did not pass stays `- [ ]` and the ticket stays open. Report the gap; a ticket marked `resolved` is a claim that the next ticket may build on this one.

Anything learned along the way that a later ticket needs -- a deviation from the spec, work added beyond it, a known delta a downstream ticket must expect -- goes under `## Comments`, not into the checkbox line. A checkbox is a yes or a no.

## Out-of-scope knowledge base

`.workspace/out-of-scope/<concept>.md` -- one file per rejected concept, **shared**, not copy-local. A `wontfix` is a deliberate decision rather than in-flight work product, and it only prevents re-litigation if every working copy can see it. Write it there directly when `/triage` rejects an enhancement.

## Pull requests

PRs are **not** a triage request surface. `/triage` should never sweep them.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a file with one **child** file per ticket.

- **Map**: `.scratch/<effort>/map.md` -- the Notes / Decisions-so-far / Fog body.
- **Child ticket**: `.scratch/<effort>/issues/NN-<slug>.md`, with the question in the body. A `Type:` line records the ticket type (`research`/`prototype`/`grilling`/`task`).
- **Frontier**: scan `.scratch/<effort>/issues/` for files that are open, unblocked, and unclaimed; first by number wins.
- **Claim**: set `Status: claimed` and save before any work.
- **Resolve**: append the answer under an `## Answer` heading, set `Status: resolved`, then append a context pointer (gist + link) to the map's Decisions-so-far in `map.md`.

## Promotion on ship

When a working copy's work actually ships, anything worth keeping moves into the shared layer before the copy is removed. Glossary terms and ADRs: see `domain.md`.

Everything else dies with the copy. That is the point -- don't pre-emptively promote in-flight tickets.

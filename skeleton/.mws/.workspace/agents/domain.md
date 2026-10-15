# Domain Docs

Where the glossary and ADRs live, and how the engineering skills should read and write them.

This project is **single-context**: one canonical glossary covering every native repo in the meta workspace. There is no `GLOSSARY-MAP.md`; don't create one.

Domain docs are **two-tier**. The shared tier is canonical and durable; the copy-local tier is provisional and disposable.

| | Shared (canonical) | Copy-local (provisional) |
| --- | --- | --- |
| Glossary | `.workspace/GLOSSARY.md` | `<copy-root>/.scratch/GLOSSARY.md` |
| ADRs | `.workspace/adr/NNNN-<slug>.md` | `<copy-root>/.scratch/adr/<slug>.md` |

`.workspace/` is symlinked into every working copy from `.mws/`. `.scratch/` is a real directory owned by one copy.

## Before exploring, read these

- `.workspace/GLOSSARY.md` -- the canonical glossary.
- `<copy-root>/.scratch/GLOSSARY.md` -- terms this copy has resolved but not yet promoted. On conflict, the local file wins: it reflects the work in progress.
- `.workspace/adr/` -- ADRs touching the area you're about to work in. `.workspace/adr/README.md` carries the house format.
- `<copy-root>/.scratch/adr/` -- draft ADRs from this copy.

If any of these don't exist, **proceed silently**. Don't flag their absence or suggest creating them upfront. `/domain-modeling` creates them lazily when a term or decision actually gets resolved.

## Writing

Always write to the copy-local tier. Never edit `.workspace/GLOSSARY.md` or add to `.workspace/adr/` mid-flight -- that is the leak this layout exists to prevent.

When creating `<copy-root>/.scratch/GLOSSARY.md`, make sure `<copy-root>/CLAUDE.local.md` imports it so later sessions load it automatically. If the file doesn't exist, create it with the single line `@.scratch/GLOSSARY.md`; if it exists without that line, append it. The import can't go in the shared `CLAUDE.md`: that file is a symlink to `.mws/`, and imports resolve from the real file's location, so `.scratch/` would point at `.mws/.scratch/` instead of the copy's.

Draft ADRs are **unnumbered**: `.scratch/adr/<slug>.md`, not `0005-...`. Numbers are assigned only at promotion, so two working copies drafting concurrently can't both claim `0005`.

## Promotion

When the copy's work ships:

1. Merge the terms from `.scratch/GLOSSARY.md` into `.workspace/GLOSSARY.md`, resolving any drift against what actually landed.
2. For each ADR in `.scratch/adr/` still worth keeping, assign the next free `NNNN` by listing `.workspace/adr/` **at promotion time** (other copies may have promoted since you drafted), and move it there in the format that README documents.
3. Remove the copy with `mws rm <copy>`.

If the work is abandoned, skip all of it. Removing the copy is the whole cleanup.

## Use the glossary's vocabulary

When your output names a domain concept -- an issue title, a refactor proposal, a hypothesis, a test name -- use the term as `GLOSSARY.md` defines it. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal: either you're inventing language the project doesn't use (reconsider), or there's a real gap (note it for `/domain-modeling`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0002 (<title>) -- but worth reopening because..._

A shared ADR outranks a copy-local draft. If your draft contradicts a promoted one, say so before proceeding.

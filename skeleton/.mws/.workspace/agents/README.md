# agents/

Configuration the engineering skills (`to-tickets`, `to-spec`, `triage`, `domain-modeling`, `wayfinder`) read to learn where this project tracks issues, what its triage vocabulary is, and where its domain docs live.

| File | Answers |
|---|---|
| `issue-tracker.md` | Where issues and specs live, and how to publish one. |
| `triage-labels.md` | The label strings for the five canonical triage roles. |
| `domain.md` | Where `GLOSSARY.md` and ADRs live, and how to read them. |

## The shared / copy-local split

This directory is **shared** -- it lives in the meta's `.mws/` and is symlinked into every working copy, so the config is written once and every present and future copy inherits it.

What the config *points at* is mostly **copy-local**. Each working copy has a real, unsymlinked `.scratch/` at its root. In-flight artifacts -- tickets, provisional glossary terms, draft ADRs -- are written there, so removing a copy discards them instead of leaving orphans in the shared layer.

Durable artifacts live shared under `.workspace/`. Work moves from local to shared by **promotion**, and only when the copy's work actually ships. Each config file spells out the promotion step for its own artifacts.

Rule of thumb: **read both tiers, write the local one, promote on ship.**

Regenerate with `/setup-matt-pocock-skills`, or just edit these files directly.

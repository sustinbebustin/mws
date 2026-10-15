# Triage Labels

The skills speak in terms of five canonical triage roles. This file maps those roles to the actual strings used in this project's tracker.

Issues here are local markdown files (see `issue-tracker.md`), so a "label" is the value of the `Status:` line near the top of the issue file -- there is no label API to call.

| Canonical role | String in our tracker | Meaning |
| --- | --- | --- |
| `needs-triage` | `needs-triage` | Maintainer needs to evaluate this issue |
| `needs-info` | `needs-info` | Waiting on the reporter for more information |
| `ready-for-agent` | `ready-for-agent` | Fully specified, ready for an AFK agent |
| `ready-for-human` | `ready-for-human` | Requires human implementation |
| `wontfix` | `wontfix` | Will not be actioned |

Category roles (`bug`, `enhancement`) go on a `Type:` line, same file, same convention.

When a skill says "apply the `ready-for-agent` label", set `Status: ready-for-agent` in the issue file and save it.

## Completion state

The five roles above answer "what did triage decide about this issue". They say nothing about whether the work has happened, so a finished ticket left on `ready-for-agent` reads as available and gets picked up again. One further value answers that:

| String | Meaning |
| --- | --- |
| `resolved` | The work is done and its acceptance criteria all pass |

`Status:` carries exactly one value. A ticket sits on its triage role until the work is finished, then moves to `resolved` -- the same word `/wayfinder` uses for its child tickets, so `Blocked by` means one thing everywhere.

`resolved` is terminal: no separate close step, no file to move. `wontfix` is also terminal, reached from triage rather than from work.

When to set it, and what to do with the acceptance checkboxes, lives in `issue-tracker.md`.

# work/

Per-initiative artifacts: plans, research, audits, notes for an active branch or feature. Ephemeral by design -- once the initiative ships, move it to `_archive/`.

Each initiative is a folder using the shape in `_template/`:

```
<slug>/
  README.md      status, PR/branch link, owner, what + why
  research/      initial discovery
  plan/          specs, plans, design docs
  notes/         running learnings, audits, follow-ups
  artifacts/     screenshots, csv dumps, captured outputs
```

If a doc here turns out to be durable (still useful next sprint), promote it to `knowledge/`.

`_template/` is the scaffold to copy. `_archive/` holds shipped or abandoned initiatives. `_plans/` is the inbox for Claude Code plan-mode artifacts -- promote them into a real `<slug>/plan/` when they become a named initiative.

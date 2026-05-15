# knowledge/domains/

One folder per business flow we own. Each domain follows the same shape so an agent can load context predictably.

```
<domain>/
  README.md       one-paragraph orientation
  overview.md     how the flow works end-to-end
  invariants.md   rules that must not break (money, eligibility, auth)
  glossary.md     names that mean specific things in this domain
  gotchas.md      known sharp edges, past incidents
  key-files.md    curated reading list into the live repos
  diagrams/       optional
```

Not every file is required up front. Start with `README.md` + whichever of the others has signal.

# lab/

Executable exploration. Scripts to probe an API, reproduce a bug, capture a flow, simulate inputs. Mostly throwaway, but worth keeping the *learnings* -- promote those to `knowledge/integrations/<vendor>/learnings.md` or a domain folder.

Each experiment is a **folder**, not a loose file:

```
<vendor>/<experiment>/
  README.md      what this experiment proved (or didn't)
  script.py      latest version -- one canonical script per experiment
  fixtures/      inputs
  runs/          captured outputs (rotate; don't let this grow forever)
```

Avoid `v2.py`, `v3.py`, `v4.py` sprawl -- iterate on one script and use git (or a `_previous/` subfolder) for prior versions.

`_scratch/` is for one-off pokes that don't deserve a folder. Prune aggressively.

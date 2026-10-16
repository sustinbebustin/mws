package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/trash"
)

// wouldLines asserts stdout holds nothing but "would <verb> <object>" lines
// and returns them. It is the dry-run voice contract (ADR-0012).
func wouldLines(t *testing.T, res mwsResult) []string {
	t.Helper()
	if res.code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", res.code, res.stdout, res.stderr)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(res.stdout, "\n"), "\n") {
		if l == "" {
			continue
		}
		verb, object, ok := strings.Cut(strings.TrimPrefix(l, "would "), " ")
		if !strings.HasPrefix(l, "would ") || !ok || verb == "" || object == "" {
			t.Errorf("stdout line %q is not `would <verb> <object>`\nfull stdout:\n%s", l, res.stdout)
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		t.Errorf("stdout has no `would` lines\nstderr: %s", res.stderr)
	}
	return lines
}

func TestRmDryRunSaysWouldTrash(t *testing.T) {
	f := seedWtWorkspace(t)
	res := runMWS(t, f.meta, "rm", "c", "--dry-run")
	if got := wouldLines(t, res); len(got) != 1 || got[0] != "would trash "+f.copy {
		t.Fatalf("lines = %q, want one `would trash %s`", got, f.copy)
	}
	if _, err := os.Stat(f.copy); err != nil {
		t.Fatalf("dry run removed the copy: %v", err)
	}

	res = runMWS(t, f.meta, "rm", "c", "--purge", "--dry-run")
	if got := wouldLines(t, res); len(got) != 1 || got[0] != "would delete "+f.copy {
		t.Fatalf("--purge lines = %q, want one `would delete %s`", got, f.copy)
	}
}

func TestRelinkDryRunSaysWould(t *testing.T) {
	meta, _ := divergedMeta(t)
	got := wouldLines(t, runMWS(t, meta, "relink", "--dry-run", "--prefer", "harness"))
	if len(got) != 1 || got[0] != "would discard main/.mcp.json" {
		t.Fatalf("lines = %q, want `would discard main/.mcp.json`", got)
	}
	got = wouldLines(t, runMWS(t, meta, "relink", "--dry-run", "--prefer", "copy"))
	if len(got) != 1 || got[0] != "would move main/.mcp.json into the harness" {
		t.Fatalf("lines = %q, want `would move main/.mcp.json into the harness`", got)
	}
}

func TestTrashDryRunsSayWould(t *testing.T) {
	f := seedWtWorkspace(t)
	if res := runMWS(t, f.meta, "rm", "c", "--yes"); res.code != 0 {
		t.Fatalf("rm: %s", res.stderr)
	}
	ageTrashEntries(t, f.meta)

	for _, args := range [][]string{{"trash", "prune", "--dry-run"}, {"trash", "empty", "--dry-run"}} {
		got := wouldLines(t, runMWS(t, f.meta, args...))
		if len(got) != 1 || got[0] != "would purge c" {
			t.Errorf("mws %s: lines = %q, want `would purge c`", strings.Join(args, " "), got)
		}
	}
	if entries, _, _ := trash.List(f.meta); len(entries) != 1 {
		t.Fatalf("dry runs left %d trash entries, want 1", len(entries))
	}
}

// ageTrashEntries backdates every trash entry far past any retention window.
func ageTrashEntries(t *testing.T, meta string) {
	t.Helper()
	entries, _, err := trash.List(meta)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		writeFileAll(t, filepath.Join(e.Path, "entry.toml"), "name = \""+e.Name+"\"\ndeleted_at = 2020-01-01T00:00:00Z\n")
	}
}

func TestWtDryRunsSayWould(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	mustNew(t, f.copy, "y", "--no-setup")
	// y has an untracked file, so prune keeps it and says why.
	writeFileAll(t, filepath.Join(f.wtPath("y"), "frontend", "scratch.txt"), "wip\n")

	got := wouldLines(t, runWt(t, f.copy, "rm", "x", "--dry-run"))
	if len(got) != 1 || got[0] != "would remove "+f.wtPath("x") {
		t.Fatalf("wt rm lines = %q", got)
	}

	res := runWt(t, f.copy, "prune", "--dry-run")
	got = wouldLines(t, res)
	if len(got) != 1 || got[0] != "would remove "+f.wtPath("x") {
		t.Fatalf("wt prune lines = %q", got)
	}
	if !strings.Contains(res.stderr, "kept y") {
		t.Fatalf("wt prune stderr should say why y is kept:\n%s", res.stderr)
	}
}

// purgedJSON is the shape of `trash prune|empty --json`.
type purgedJSON struct {
	DryRun     bool `json:"dry_run"`
	Purged     []struct{ Name string }
	Unreadable []string `json:"unreadable"`
}

func decodePurged(t *testing.T, res mwsResult) purgedJSON {
	t.Helper()
	if res.code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", res.code, res.stdout, res.stderr)
	}
	var out purgedJSON
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, res.stdout)
	}
	return out
}

func TestTrashPruneAndEmptyHonourJSON(t *testing.T) {
	f := seedWtWorkspace(t)
	if res := runMWS(t, f.meta, "rm", "c", "--yes"); res.code != 0 {
		t.Fatalf("rm: %s", res.stderr)
	}
	ageTrashEntries(t, f.meta)

	for _, args := range [][]string{{"trash", "prune", "--dry-run", "--json"}, {"trash", "empty", "--dry-run", "--json"}} {
		out := decodePurged(t, runMWS(t, f.meta, args...))
		if !out.DryRun || len(out.Purged) != 1 || out.Purged[0].Name != "c" {
			t.Errorf("mws %s: %+v, want dry_run true listing c", strings.Join(args, " "), out)
		}
	}
	if entries, _, _ := trash.List(f.meta); len(entries) != 1 {
		t.Fatalf("dry runs left %d trash entries, want 1", len(entries))
	}

	out := decodePurged(t, runMWS(t, f.meta, "trash", "prune", "--json"))
	if out.DryRun || len(out.Purged) != 1 || out.Purged[0].Name != "c" {
		t.Fatalf("prune --json: %+v, want dry_run false listing c", out)
	}
}

func TestTrashEmptyJSONListsPurgedEntries(t *testing.T) {
	f := seedWtWorkspace(t)
	if res := runMWS(t, f.meta, "rm", "c", "--yes"); res.code != 0 {
		t.Fatalf("rm: %s", res.stderr)
	}
	out := decodePurged(t, runMWS(t, f.meta, "trash", "empty", "--yes", "--json"))
	if out.DryRun || len(out.Purged) != 1 || out.Purged[0].Name != "c" {
		t.Fatalf("empty --yes --json: %+v, want dry_run false listing c", out)
	}
	if entries, _, _ := trash.List(f.meta); len(entries) != 0 {
		t.Fatalf("trash still holds %d entries", len(entries))
	}
}

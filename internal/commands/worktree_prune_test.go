package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type pruneOut struct {
	DryRun   bool     `json:"dry_run"`
	Repaired []string `json:"repaired"`
	Dropped  []string `json:"dropped"`
	Removed  []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	} `json:"removed"`
	Kept []struct {
		Name    string   `json:"name"`
		Path    string   `json:"path"`
		Reasons []string `json:"reasons"`
	} `json:"kept"`
}

func runPrune(t *testing.T, cwd string, args ...string) pruneOut {
	t.Helper()
	res := runWt(t, cwd, append([]string{"prune", "--json"}, args...)...)
	if res.code != 0 {
		t.Fatalf("wt prune: exit %d\nstderr: %s", res.code, res.stderr)
	}
	var out pruneOut
	decodeJSON(t, res, &out)
	return out
}

// keptReasons returns the reasons prune gave for keeping name, or nil.
func (o pruneOut) keptReasons(name string) []string {
	for _, k := range o.Kept {
		if k.Name == name {
			return k.Reasons
		}
	}
	return nil
}

// seedPruneMix makes four worktrees: "done" (clean, merged), "dirty",
// "ahead" (an unmerged commit) and, on Linux, "busy" (a process inside).
func seedPruneMix(t *testing.T) wtFixture {
	t.Helper()
	f := seedWtWorkspace(t)
	for _, name := range []string{"done", "dirty", "ahead", "busy"} {
		mustNew(t, f.copy, name, "--no-setup")
	}
	writeFileAll(t, filepath.Join(f.wtPath("dirty"), "frontend", "wip.txt"), "unsaved")
	commitIn(t, filepath.Join(f.wtPath("ahead"), "backend"), "a.txt")
	if runtime.GOOS == "linux" {
		startIn(t, f.wtPath("busy"))
	} else if err := os.RemoveAll(f.wtPath("busy")); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestWtPruneRemovesOnlyCleanIdleMergedWorktrees(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("liveness is read from /proc")
	}
	f := seedPruneMix(t)

	out := runPrune(t, f.copy)
	if out.DryRun || len(out.Removed) != 1 || out.Removed[0].Name != "done" {
		t.Fatalf("removed = %+v, want only done", out.Removed)
	}
	assertRemoved(t, f, "done")
	if branchExists(t, f, "frontend", "wt/done") {
		t.Fatal("prune kept wt/done although it is fully merged")
	}
	for name, want := range map[string]string{"dirty": "frontend", "ahead": "backend:wt/ahead", "busy": "in use"} {
		reasons := strings.Join(out.keptReasons(name), "; ")
		if !strings.Contains(reasons, want) {
			t.Fatalf("kept %s for %q, want a reason mentioning %q", name, reasons, want)
		}
		if _, err := os.Stat(f.wtPath(name)); err != nil {
			t.Fatalf("prune removed %s: %v", name, err)
		}
	}

	plain := runWt(t, f.copy, "prune")
	if plain.code != 0 || !strings.Contains(plain.stdout, "kept ahead: backend:wt/ahead has 1 commit not merged anywhere") {
		t.Fatalf("plain prune: exit %d\n%s", plain.code, plain.stdout)
	}
}

func TestWtPruneKeepsTheWorktreeItRunsIn(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")

	out := runPrune(t, filepath.Join(f.wtPath("x"), "frontend"))
	if len(out.Removed) != 0 || len(out.keptReasons("x")) == 0 {
		t.Fatalf("prune = %+v, want x kept because prune runs inside it", out)
	}
}

func TestWtPruneDryRunChangesNothing(t *testing.T) {
	f := seedPruneMix(t)
	mustNew(t, f.copy, "gone", "--no-setup")
	if err := os.RemoveAll(f.wtPath("gone")); err != nil {
		t.Fatal(err)
	}
	before := gitT(t, f.meta, "worktree", "list", "--porcelain")

	out := runPrune(t, f.copy, "--dry-run")
	if !out.DryRun || len(out.Removed) != 1 || out.Removed[0].Name != "done" {
		t.Fatalf("dry run = %+v, want done as the only removal", out)
	}
	if len(out.Dropped) != 1 || out.Dropped[0] != f.wtPath("gone") {
		t.Fatalf("dropped = %v, want gone's registration", out.Dropped)
	}
	if _, err := os.Stat(f.wtPath("done")); err != nil {
		t.Fatalf("dry run removed done: %v", err)
	}
	if !branchExists(t, f, "frontend", "wt/done") {
		t.Fatal("dry run deleted wt/done")
	}
	if after := gitT(t, f.meta, "worktree", "list", "--porcelain"); after != before {
		t.Fatalf("dry run changed the meta registrations:\n%s\nwant\n%s", after, before)
	}
}

func TestWtPruneDropsOrphansAndRepairsStaleAnchors(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "gone", "--no-setup")
	if err := os.RemoveAll(f.wtPath("gone")); err != nil {
		t.Fatal(err)
	}
	// "moved": its registration points elsewhere, as after a restore; dirty so
	// prune keeps it once repaired.
	mustNew(t, f.copy, "moved", "--no-setup")
	writeFileAll(t, filepath.Join(f.wtPath("moved"), "frontend", "wip.txt"), "unsaved")
	writeFileAll(t, filepath.Join(f.meta, ".git", "worktrees", "moved", "gitdir"), filepath.Join(f.root, "elsewhere", ".git")+"\n")

	out := runPrune(t, f.copy)
	if len(out.Repaired) != 1 || out.Repaired[0] != f.wtPath("moved") {
		t.Fatalf("repaired = %v, want moved", out.Repaired)
	}
	if len(out.Dropped) != 1 || out.Dropped[0] != f.wtPath("gone") {
		t.Fatalf("dropped = %v, want gone", out.Dropped)
	}
	meta := gitT(t, f.meta, "worktree", "list", "--porcelain")
	if strings.Contains(meta, f.wtPath("gone")) || !strings.Contains(meta, f.wtPath("moved")) {
		t.Fatalf("meta registrations after prune:\n%s\nwant moved registered and gone dropped", meta)
	}
	if strings.Contains(gitT(t, f.repo("frontend"), "worktree", "list", "--porcelain"), f.wtPath("gone")) {
		t.Fatal("frontend still registers gone's inner worktree")
	}
	if reasons := out.keptReasons("moved"); len(reasons) == 0 {
		t.Fatalf("kept = %+v, want moved kept as dirty", out.Kept)
	}
}

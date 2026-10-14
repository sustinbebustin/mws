package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/trash"
)

// assertWorktreeIntact checks that the worktree at root works after a move:
// its anchor and inner repos are registered at root, the inner repos are
// still on their wt/ branch, and the untracked file seeded in frontend is
// still there.
func assertWorktreeIntact(t *testing.T, f wtFixture, root, name string) {
	t.Helper()
	if got := gitT(t, root, "rev-parse", "--show-toplevel"); got != root {
		t.Fatalf("anchor toplevel = %q, want %q", got, root)
	}
	meta := gitT(t, f.meta, "worktree", "list", "--porcelain")
	if !strings.Contains(meta, "worktree "+root+"\n") || strings.Contains(meta, "prunable") {
		t.Fatalf("meta registrations do not hold %s cleanly:\n%s", root, meta)
	}
	for _, folder := range []string{"backend", "frontend"} {
		dir := filepath.Join(root, folder)
		if got := gitT(t, dir, "branch", "--show-current"); got != "wt/"+name {
			t.Fatalf("%s branch = %q, want wt/%s", folder, got, name)
		}
		if got := gitT(t, dir, "rev-parse", "--show-toplevel"); got != dir {
			t.Fatalf("%s toplevel = %q, want %q", folder, got, dir)
		}
	}
	if got := gitT(t, filepath.Join(root, "frontend"), "status", "--porcelain"); got != "?? wip.txt" {
		t.Fatalf("frontend status = %q, want the untracked wip.txt", got)
	}
}

func TestRestoreRepairsWorktrees(t *testing.T) {
	for _, tc := range []struct{ name, as string }{{"same name", ""}, {"as", "c2"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := seedWtWorkspace(t)
			mustNew(t, f.copy, "x", "--no-setup")
			writeFileAll(t, filepath.Join(f.wtPath("x"), "frontend", "wip.txt"), "unsaved")

			withCwd(t, f.meta, func() {
				if err := runRm(t.Context(), nopReporter{}, "c", true, false); err != nil {
					t.Fatalf("runRm: %v", err)
				}
				if err := runRestore(t.Context(), nopReporter{}, "c", tc.as, true); err != nil {
					t.Fatalf("runRestore: %v", err)
				}
			})
			dest := f.copy
			if tc.as != "" {
				dest = filepath.Join(f.meta, tc.as)
			}
			assertWorktreeIntact(t, f, filepath.Join(dest, ".wt", "x"), "x")
		})
	}
}

// `mws trash prune` and `mws wt prune` run `git worktree prune` on the meta
// repo; a copy waiting in the trash must survive that.
func TestRestoreSurvivesAMetaPruneWhileTrashed(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	writeFileAll(t, filepath.Join(f.wtPath("x"), "frontend", "wip.txt"), "unsaved")

	withCwd(t, f.meta, func() {
		if err := runRm(t.Context(), nopReporter{}, "c", true, false); err != nil {
			t.Fatalf("runRm: %v", err)
		}
		gitT(t, f.meta, "worktree", "prune")
		if err := runRestore(t.Context(), nopReporter{}, "c", "", true); err != nil {
			t.Fatalf("runRestore: %v", err)
		}
	})
	assertWorktreeIntact(t, f, f.wtPath("x"), "x")
}

func TestRelinkRepairsHarnessSymlinksInWorktrees(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	root := f.wtPath("x")
	if err := os.Remove(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	gitignore := readFile(t, filepath.Join(root, ".gitignore"))

	withCwd(t, f.meta, func() {
		if err := runRelink(nopReporter{}); err != nil {
			t.Fatalf("runRelink: %v", err)
		}
	})
	if got := readFile(t, filepath.Join(root, "CLAUDE.md")); got != "# harness" {
		t.Fatalf("worktree CLAUDE.md = %q, want the harness file", got)
	}
	if got := readFile(t, filepath.Join(root, ".gitignore")); got != gitignore {
		t.Fatalf("relink replaced the anchor's .gitignore with %q", got)
	}
	if got := gitT(t, root, "status", "--porcelain"); got != "" {
		t.Fatalf("anchor status after relink not clean:\n%s", got)
	}
}

func TestRmConfirmationListsWorktrees(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	mustNew(t, f.copy, "y", "--no-setup")
	cfg, err := config.Load(f.meta)
	if err != nil {
		t.Fatal(err)
	}
	ws := &project.Workspace{MetaRoot: f.meta}
	wts, err := copyWorktrees(t.Context(), nopReporter{}, ws, cfg, f.copy)
	if err != nil {
		t.Fatal(err)
	}

	for _, toTrash := range []bool{true, false} {
		got := rmConfirmDescription(f.meta, "c", toTrash, wts)
		if !strings.Contains(got, "worktrees x, y") {
			t.Fatalf("toTrash=%v: confirmation %q does not list worktrees x, y", toTrash, got)
		}
	}
	if got := rmConfirmDescription(f.meta, "c", true, nil); strings.Contains(got, "worktree") {
		t.Fatalf("confirmation for a copy without worktrees mentions them: %q", got)
	}
}

func TestRmRefusesWhileAWorktreeIsLive(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process cwds are read from /proc")
	}
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	agent := startIn(t, filepath.Join(f.wtPath("x"), "frontend"))

	var err error
	withCwd(t, f.meta, func() { err = runRm(t.Context(), nopReporter{}, "c", true, false) })
	if err == nil {
		t.Fatal("rm removed a copy whose worktree has a live process")
	}
	for _, want := range []string{"x", strconv.Itoa(agent.Process.Pid), "nothing was removed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
	if _, err := os.Stat(f.wtPath("x")); err != nil {
		t.Fatalf("refused rm moved the copy: %v", err)
	}
	if entries, _, _ := trash.List(f.meta); len(entries) != 0 {
		t.Fatalf("refused rm trashed %d entries", len(entries))
	}
}

func TestListCountsWorktreesPerCopyWithoutListingThem(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "a", "--no-setup")
	mustNew(t, f.copy, "b", "--no-setup")

	rec := &recordingReporter{}
	withCwd(t, f.wtPath("a"), func() {
		if err := runList(rec); err != nil {
			t.Fatalf("runList: %v", err)
		}
	})
	want := []string{"* c  (+2 worktrees)"}
	if !slices.Equal(rec.infos, want) {
		t.Fatalf("list lines = %q, want %q", rec.infos, want)
	}
}

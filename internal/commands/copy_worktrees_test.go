package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
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
				if err := runRm(t.Context(), discardOut, "c", rmFlags{yes: true}); err != nil {
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
		if err := runRm(t.Context(), discardOut, "c", rmFlags{yes: true}); err != nil {
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
		if err := runRelink(discardOut, relinkFlags{}); err != nil {
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

func TestTrashPurgeDropsMetaWorktreeRegistrations(t *testing.T) {
	for _, tc := range []struct {
		name  string
		purge func(t *testing.T) error
	}{
		{"prune", func(t *testing.T) error { return runTrashPrune(t.Context(), discardOut, false) }},
		{"empty", func(t *testing.T) error {
			return runTrashEmpty(t.Context(), discardOut, confirmFlags{yes: true})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := seedWtWorkspace(t)
			mustNew(t, f.copy, "x", "--no-setup")
			withCwd(t, f.meta, func() {
				if err := runRm(t.Context(), discardOut, "c", rmFlags{yes: true}); err != nil {
					t.Fatalf("runRm: %v", err)
				}
				entries, _, err := trash.List(f.meta)
				if err != nil || len(entries) != 1 {
					t.Fatalf("trash.List = %v, %v; want one entry", entries, err)
				}
				// Past the default 7-day retention.
				writeFileAll(t, filepath.Join(entries[0].Path, "entry.toml"), "name = \"c\"\ndeleted_at = 2000-01-01T00:00:00Z\n")
				if err := tc.purge(t); err != nil {
					t.Fatalf("purge: %v", err)
				}
			})
			if got := gitT(t, f.meta, "worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 1 {
				t.Fatalf("meta registrations after purge:\n%s\nwant only the meta root", got)
			}
		})
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
	wts, err := copyWorktrees(t.Context(), nopReporter{}, f.meta, cfg, f.copy)
	if err != nil {
		t.Fatal(err)
	}

	for _, toTrash := range []bool{true, false} {
		got := rmConfirmDescription(f.meta, "c", toTrash, 0, wts)
		if !strings.Contains(got, "worktrees x, y") {
			t.Fatalf("toTrash=%v: confirmation %q does not list worktrees x, y", toTrash, got)
		}
	}
	if got := rmConfirmDescription(f.meta, "c", true, 0, nil); strings.Contains(got, "worktree") {
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
	withCwd(t, f.meta, func() { err = runRm(t.Context(), discardOut, "c", rmFlags{yes: true}) })
	if err == nil {
		t.Fatal("rm removed a copy whose worktree has a live process")
	}
	if code := ExitCode(err); code != 3 {
		t.Fatalf("exit %d, want 3 (refused), as wt rm", code)
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

	var out, jsonOut bytes.Buffer
	withCwd(t, f.wtPath("a"), func() {
		if err := runList(output{stdout: &out, stderr: io.Discard, json: false}); err != nil {
			t.Fatalf("runList: %v", err)
		}
		if err := runList(output{stdout: &jsonOut, stderr: io.Discard, json: true}); err != nil {
			t.Fatalf("runList --json: %v", err)
		}
	})
	if got, want := out.String(), "* c  default  (+2 worktrees)\n"; got != want {
		t.Fatalf("list stdout = %q, want %q", got, want)
	}
	var got struct {
		Copies []copyJSON `json:"copies"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &got); err != nil {
		t.Fatalf("list --json: %v\n%s", err, jsonOut.String())
	}
	want := []copyJSON{{Name: "c", Path: f.copy, Current: true, Ports: "default", Worktrees: 2}}
	if !slices.Equal(got.Copies, want) {
		t.Fatalf("list --json copies = %+v, want %+v", got.Copies, want)
	}
}

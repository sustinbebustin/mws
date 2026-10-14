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

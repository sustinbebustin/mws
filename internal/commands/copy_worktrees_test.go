package commands

import (
	"slices"
	"testing"
)

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

package commands

import (
	"path/filepath"
	"strings"
	"testing"
)

// assertUsage fails unless res exited 2 with every one of want on stderr.
func assertUsage(t *testing.T, res mwsResult, want ...string) {
	t.Helper()
	if res.code != 2 {
		t.Fatalf("exit %d, want 2\nstderr: %s", res.code, res.stderr)
	}
	for _, w := range want {
		if !strings.Contains(res.stderr, w) {
			t.Errorf("stderr does not name %q:\n%s", w, res.stderr)
		}
	}
}

func TestCopyCommandsFromMetaRootWithoutCopyAreUsageErrors(t *testing.T) {
	f := seedWtWorkspace(t)
	for _, tc := range []struct {
		args []string
		fix  string
	}{
		{[]string{"include", "extra"}, "mws include extra <copy>"},
		{[]string{"sync-env"}, "mws sync-env <copy>"},
		{[]string{"stage-env"}, "mws stage-env <copy>"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			assertUsage(t, runMWS(t, f.meta, tc.args...), tc.fix)
		})
	}
}

func TestPromoteFromMetaRootIsUsageError(t *testing.T) {
	f := seedWtWorkspace(t)
	assertUsage(t, runMWS(t, f.meta, "promote", "notes.md"), "cd <copy> && mws promote notes.md")
}

func TestPromoteInsideWorktreeIsUsageErrorNamingTheCopy(t *testing.T) {
	f := seedWtWorkspace(t)
	if res := runWt(t, f.copy, "new", "x", "--no-setup"); res.code != 0 {
		t.Fatalf("wt new: exit %d\nstderr: %s", res.code, res.stderr)
	}
	wt := f.wtPath("x")
	writeFileAll(t, filepath.Join(wt, "notes.md"), "local")

	res := runMWS(t, wt, "promote", "notes.md")
	assertUsage(t, res, "acts on the working copy", "cd "+f.copy+" && mws promote notes.md")
	if strings.Contains(res.stderr, "nested") {
		t.Errorf("stderr blames a nested path:\n%s", res.stderr)
	}
	if got := readFile(t, filepath.Join(wt, "notes.md")); got != "local" {
		t.Errorf("worktree notes.md = %q, want it left in place", got)
	}
}

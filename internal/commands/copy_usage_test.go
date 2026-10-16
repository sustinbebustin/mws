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
		{[]string{"env", "sync"}, "mws env sync <copy>"},
		{[]string{"env", "stage"}, "mws env stage <copy>"},
		{[]string{"env", "diff"}, "mws env diff <copy>"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			assertUsage(t, runMWS(t, f.meta, tc.args...), tc.fix)
		})
	}
}

func TestCopyCommandsExitFourForAnUnknownCopyAndTwoForAnInvalidName(t *testing.T) {
	f := seedEnvWorkspace(t)
	for _, args := range [][]string{
		{"env", "sync"},
		{"env", "diff"},
		{"include", "extra"},
		{"ports", "show"},
		{"path"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			res := runMWS(t, f.meta, append(args, "nope")...)
			if res.code != 4 || !strings.Contains(res.stderr, `"nope"`) {
				t.Errorf("unknown copy: exit %d, want 4 naming it\nstderr: %s", res.code, res.stderr)
			}
			assertUsage(t, runMWS(t, f.meta, append(args, ".mws")...), ".mws")
		})
	}
}

func TestCopyCommandsNameTheCopyArgumentInUsage(t *testing.T) {
	dir := t.TempDir()
	for cmd, use := range map[string]string{
		"rm":        "mws rm <copy>",
		"restore":   "mws restore [copy]",
		"include":   "mws include <folder> [copy]",
		"env sync":  "mws env sync [copy]",
		"env stage": "mws env stage [copy]",
		"env diff":  "mws env diff [copy]",
	} {
		res := runMWS(t, dir, append(strings.Fields(cmd), "--help")...)
		if res.code != 0 {
			t.Fatalf("mws %s --help: exit %d\nstderr: %s", cmd, res.code, res.stderr)
		}
		if !strings.Contains(res.stdout, "Usage:\n  "+use+" ") {
			t.Errorf("mws %s --help usage line is not %q:\n%s", cmd, use, res.stdout)
		}
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

package commands

import (
	"strings"
	"testing"
)

func TestWtExecRunsInRootAndPassesExitCode(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")

	res := runWt(t, f.repo("frontend"), "exec", "x", "--", "sh", "-c", "pwd; echo oops >&2; exit 4")
	if res.code != 4 {
		t.Fatalf("exit %d, want the command's 4\nstderr: %s", res.code, res.stderr)
	}
	if got := strings.TrimSpace(res.stdout); got != f.wtPath("x") {
		t.Fatalf("command ran in %q, want the worktree root %q", got, f.wtPath("x"))
	}
	if strings.TrimSpace(res.stderr) != "oops" {
		t.Fatalf("stderr = %q, want only the command's own output", res.stderr)
	}

	if res := runWt(t, f.copy, "exec", "x", "--", "true"); res.code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", res.code, res.stderr)
	}
}

func TestWtExecErrors(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")

	for name, tc := range map[string]struct {
		args []string
		code int
	}{
		"no command":       {[]string{"exec", "x"}, 2},
		"missing dash":     {[]string{"exec", "x", "true"}, 2},
		"unknown worktree": {[]string{"exec", "nope", "--", "true"}, 1},
		"not executable":   {[]string{"exec", "x", "--", "no-such-command-mws"}, 1},
	} {
		if res := runWt(t, f.copy, tc.args...); res.code != tc.code {
			t.Fatalf("%s: exit %d, want %d\nstderr: %s", name, res.code, tc.code, res.stderr)
		}
	}

	res := runWt(t, f.copy, "exec", "x", "--json", "--", "no-such-command-mws")
	if res.code != 1 {
		t.Fatalf("--json not executable: exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	if e := decodeError(t, res); e.Code != "failure" || !strings.Contains(e.Message, "no-such-command-mws") {
		t.Fatalf("error = %+v, want a failure naming the command", e)
	}
}

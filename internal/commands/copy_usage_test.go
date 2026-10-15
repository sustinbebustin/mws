package commands

import (
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

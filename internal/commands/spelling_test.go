package commands

import (
	"strings"
	"testing"
)

func TestListIsSpelledLsAtEveryLevel(t *testing.T) {
	f := seedWtWorkspace(t)

	for _, args := range [][]string{{"list"}, {"trash", "list"}, {"wt", "list"}} {
		cwd := f.copy
		res := runMWS(t, cwd, args...)
		if res.code != 2 {
			t.Errorf("mws %s: exit %d, want 2 (unknown command)\nstderr: %s", strings.Join(args, " "), res.code, res.stderr)
		}
	}
	for _, args := range [][]string{{"ls"}, {"trash", "ls"}, {"wt", "ls"}} {
		res := runMWS(t, f.copy, args...)
		if res.code != 0 {
			t.Errorf("mws %s: exit %d, want 0\nstderr: %s", strings.Join(args, " "), res.code, res.stderr)
		}
	}
}

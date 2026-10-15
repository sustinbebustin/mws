package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

// discardOut is an output whose stdout and stderr go nowhere, for tests that
// call a run function directly and assert on its effects alone.
var discardOut = output{stdout: io.Discard, stderr: io.Discard}

func TestListIsSpelledLsAtEveryLevel(t *testing.T) {
	f := seedWtWorkspace(t)

	for _, args := range [][]string{{"list"}, {"trash", "list"}, {"wt", "list"}} {
		res := runMWS(t, f.copy, args...)
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

// divergedMeta is a meta workspace whose only copy holds a .mcp.json that
// differs from the harness one.
func divergedMeta(t *testing.T) (meta, copyDir string) {
	t.Helper()
	meta, copyDir = setupRelinkTree(t)
	if err := config.Save(meta, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(meta, project.HarnessDirName, ".mcp.json"), "harness")
	mustWriteFile(t, filepath.Join(copyDir, ".mcp.json"), "copy")
	return meta, copyDir
}

func TestRelinkPreferMetaIsAUsageError(t *testing.T) {
	meta, _ := divergedMeta(t)
	for _, v := range []string{"meta", "peer", "bogus"} {
		res := runMWS(t, meta, "relink", "--prefer", v)
		if res.code != 2 {
			t.Errorf("--prefer %s: exit %d, want 2\nstderr: %s", v, res.code, res.stderr)
		}
	}
}

func TestRelinkPreferCopyHarnessSkip(t *testing.T) {
	for _, tc := range []struct {
		prefer     string
		wantStderr string
		wantBody   string // content of <harness>/.mcp.json afterwards
		wantLink   bool   // the copy's .mcp.json is a symlink afterwards
	}{
		{"copy", "copy", "copy", true},
		{"harness", "harness", "harness", true},
		{"skip", "left diverged", "harness", false},
	} {
		t.Run(tc.prefer, func(t *testing.T) {
			meta, copyDir := divergedMeta(t)
			res := runMWS(t, meta, "relink", "--prefer", tc.prefer)
			if res.code != 0 {
				t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
			}
			if !strings.Contains(res.stderr, tc.wantStderr) {
				t.Errorf("stderr lacks %q:\n%s", tc.wantStderr, res.stderr)
			}
			for _, banned := range []string{"peer", "meta"} {
				if strings.Contains(res.stderr, banned) {
					t.Errorf("stderr still says %q:\n%s", banned, res.stderr)
				}
			}
			if got := readFile(t, filepath.Join(meta, project.HarnessDirName, ".mcp.json")); got != tc.wantBody {
				t.Errorf("harness .mcp.json = %q, want %q", got, tc.wantBody)
			}
			st, err := os.Lstat(filepath.Join(copyDir, ".mcp.json"))
			if err != nil {
				t.Fatal(err)
			}
			if isLink := st.Mode()&os.ModeSymlink != 0; isLink != tc.wantLink {
				t.Errorf("copy .mcp.json symlink = %v, want %v", isLink, tc.wantLink)
			}
		})
	}
}

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

func TestRunPromoteMovesEntryAndSymlinks(t *testing.T) {
	root := t.TempDir()
	meta := filepath.Join(root, "demo")
	main := filepath.Join(meta, "main")
	feature := filepath.Join(meta, "feature")
	mustMkdir(t, filepath.Join(meta, project.HarnessDirName))
	mustMkdir(t, main)
	mustMkdir(t, feature)
	if err := config.Save(meta, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatal(err)
	}
	// File to promote lives only in `main`.
	mustWriteFile(t, filepath.Join(main, "NOTES.md"), "shared notes")

	withCwd(t, main, func() {
		if err := runPromote(nopReporter{}, "NOTES.md"); err != nil {
			t.Fatalf("runPromote: %v", err)
		}
	})

	// Harness now owns the file.
	body, err := os.ReadFile(filepath.Join(meta, project.HarnessDirName, "NOTES.md"))
	if err != nil {
		t.Fatalf("read harness file: %v", err)
	}
	if string(body) != "shared notes" {
		t.Fatalf("harness content = %q", string(body))
	}

	// Source working copy has a symlink back into the harness.
	st, err := os.Lstat(filepath.Join(main, "NOTES.md"))
	if err != nil {
		t.Fatalf("lstat main/NOTES.md: %v", err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("main/NOTES.md is not a symlink")
	}

	// Peer working copy has the same symlink backfilled.
	pSt, err := os.Lstat(filepath.Join(feature, "NOTES.md"))
	if err != nil {
		t.Fatalf("lstat feature/NOTES.md: %v", err)
	}
	if pSt.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("feature/NOTES.md is not a symlink")
	}
}

// assertRefused fails unless res exited 3 with every one of want on stderr.
func assertRefused(t *testing.T, res mwsResult, want ...string) {
	t.Helper()
	if res.code != 3 {
		t.Fatalf("exit %d, want 3\nstderr: %s", res.code, res.stderr)
	}
	for _, w := range want {
		if !strings.Contains(res.stderr, w) {
			t.Errorf("stderr does not name %q:\n%s", w, res.stderr)
		}
	}
}

func TestPromoteRefusesAnMwsMarker(t *testing.T) {
	f := seedWtWorkspace(t)
	writeFileAll(t, filepath.Join(f.copy, ".mws-ports.env"), "MWS_SLOT=1\n")

	assertRefused(t, runMWS(t, f.copy, "promote", ".mws-ports.env"), ".mws-ports.env", "mws marker")
	if got := readFile(t, filepath.Join(f.copy, ".mws-ports.env")); got != "MWS_SLOT=1\n" {
		t.Errorf("marker = %q, want it left in place", got)
	}
}

func TestPromoteRefusesARenderTarget(t *testing.T) {
	f := seedWtWorkspace(t)
	f.cfg.Renders = []config.Render{{Source: ".mcp.json" + config.RenderSuffix, Target: ".mcp.json"}}
	f.saveConfig(t)

	assertRefused(t, runMWS(t, f.copy, "promote", ".mcp.json"), ".mcp.json", "[[renders]] target")
	if got := readFile(t, filepath.Join(f.copy, ".mcp.json")); got != `{"mcpServers":{}}` {
		t.Errorf(".mcp.json = %q, want it left in place", got)
	}
}

func TestPromoteCandidatesOmitMarkersAndRenderTargets(t *testing.T) {
	f := seedWtWorkspace(t)
	f.cfg.Renders = []config.Render{{Source: ".mcp.json" + config.RenderSuffix, Target: ".mcp.json"}}
	f.saveConfig(t)
	writeFileAll(t, filepath.Join(f.copy, ".mws-ports.env"), "MWS_SLOT=1\n")

	res := runMWS(t, f.copy, "promote")
	assertUsage(t, res, "promotable: ", ".ports.env")
	for _, hidden := range []string{".mws-ports.env", ".mcp.json"} {
		if strings.Contains(res.stderr, hidden) {
			t.Errorf("candidate list names %s:\n%s", hidden, res.stderr)
		}
	}
}

func TestRunPromoteRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	meta := filepath.Join(root, "demo")
	main := filepath.Join(meta, "main")
	mustMkdir(t, filepath.Join(meta, project.HarnessDirName))
	mustMkdir(t, main)
	if err := config.Save(meta, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatal(err)
	}
	// Pre-existing symlink (e.g. already in harness).
	if err := os.Symlink("../.mws/foo", filepath.Join(main, "foo")); err != nil {
		t.Fatal(err)
	}
	withCwd(t, main, func() {
		err := runPromote(nopReporter{}, "foo")
		if err == nil {
			t.Fatalf("runPromote should reject symlinks")
		}
	})
}

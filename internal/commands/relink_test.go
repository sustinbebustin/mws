package commands

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

func setupRelinkTree(t *testing.T) (meta, peer string) {
	t.Helper()
	root := t.TempDir()
	meta = filepath.Join(root, "demo")
	peer = filepath.Join(meta, "main")
	mustMkdir(t, filepath.Join(meta, project.HarnessDirName))
	mustMkdir(t, peer)
	return meta, peer
}

func TestRepairDivergedFilesAutoRepairsIdentical(t *testing.T) {
	meta, peer := setupRelinkTree(t)

	body := []byte(`{"k":"v"}`)
	mustWriteFile(t, filepath.Join(meta, project.HarnessDirName, ".mcp.json"), string(body))
	mustWriteFile(t, filepath.Join(peer, ".mcp.json"), string(body))

	called := false
	prompt := func(string, string) (string, error) {
		called = true
		return "", errors.New("prompt should not be called for identical content")
	}
	if err := repairDivergedFiles(nopReporter{}, meta, peer, prompt); err != nil {
		t.Fatalf("repairDivergedFiles: %v", err)
	}
	if called {
		t.Fatal("prompt was called for identical content")
	}
	if _, err := os.Lstat(filepath.Join(peer, ".mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("expected peer .mcp.json removed, got err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(meta, project.HarnessDirName, ".mcp.json")); err != nil {
		t.Fatalf("harness .mcp.json should remain: %v", err)
	}
}

func TestRepairDivergedFilesPromptsAndPromotes(t *testing.T) {
	meta, peer := setupRelinkTree(t)

	mustWriteFile(t, filepath.Join(meta, project.HarnessDirName, ".mcp.json"), `{"old":true}`)
	mustWriteFile(t, filepath.Join(peer, ".mcp.json"), `{"new":true}`)

	prompt := func(string, string) (string, error) { return "peer", nil }
	if err := repairDivergedFiles(nopReporter{}, meta, peer, prompt); err != nil {
		t.Fatalf("repairDivergedFiles: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(meta, project.HarnessDirName, ".mcp.json"))
	if err != nil {
		t.Fatalf("read harness after promote: %v", err)
	}
	if string(body) != `{"new":true}` {
		t.Fatalf("harness not promoted; got %q", string(body))
	}
	if _, err := os.Lstat(filepath.Join(peer, ".mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("peer file should have been moved, got err=%v", err)
	}
}

func TestRunRelinkVisitsCopiesUnderWorkingCopiesDir(t *testing.T) {
	root := t.TempDir()
	meta := filepath.Join(root, "demo")
	harness := filepath.Join(meta, project.HarnessDirName)
	mustMkdir(t, harness)
	mustWriteFile(t, filepath.Join(harness, "CLAUDE.md"), "# harness")
	if err := config.Save(meta, &config.Config{
		ProjectName:      "demo",
		WorkingCopiesDir: "copies",
	}); err != nil {
		t.Fatal(err)
	}
	peer := filepath.Join(meta, "copies", "feature")
	mustMkdir(t, peer)

	withCwd(t, meta, func() {
		if err := runRelink(nopReporter{}); err != nil {
			t.Fatalf("runRelink: %v", err)
		}
	})

	st, err := os.Lstat(filepath.Join(peer, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("expected CLAUDE.md symlink in peer under copies/: %v", err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("peer CLAUDE.md is not a symlink")
	}
}

func TestRepairDivergedFilesSkipsSymlinksAndPeerOnlyFiles(t *testing.T) {
	meta, peer := setupRelinkTree(t)

	mustWriteFile(t, filepath.Join(meta, project.HarnessDirName, ".mcp.json"), `meta`)
	if err := os.Symlink("../.mws/.mcp.json", filepath.Join(peer, ".mcp.json")); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(peer, "notes.md"), `peer-only`)

	prompt := func(string, string) (string, error) {
		t.Fatal("prompt should not be called")
		return "", nil
	}
	if err := repairDivergedFiles(nopReporter{}, meta, peer, prompt); err != nil {
		t.Fatalf("repairDivergedFiles: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(peer, ".mcp.json")); err != nil {
		t.Fatalf("peer symlink should remain: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(peer, "notes.md")); err != nil {
		t.Fatalf("peer-only file should remain: %v", err)
	}
}

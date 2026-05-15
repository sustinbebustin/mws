package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

// setupWorkspace builds a fake sibling-meta layout under root:
//
//	root/
//	  demo-meta/.mws/config.toml
//	  demo/           (symlinks .mws -> ../demo-meta/.mws)
//	  demo-bug/       (symlinks .mws -> ../demo-meta/.mws)
//	  other/          (unrelated dir)
func setupWorkspace(t *testing.T) (root, meta, primary, peer string) {
	t.Helper()
	root = t.TempDir()
	meta = filepath.Join(root, "demo-meta")
	if err := os.MkdirAll(filepath.Join(meta, config.DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(meta, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatal(err)
	}

	mkPeer := func(name string) string {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		// Symlink .mws to the meta's .mws.
		if err := os.Symlink(filepath.Join(meta, config.DirName), filepath.Join(p, config.DirName)); err != nil {
			t.Fatal(err)
		}
		return p
	}

	primary = mkPeer("demo")
	peer = mkPeer("demo-bug")

	if err := os.MkdirAll(filepath.Join(root, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, meta, primary, peer
}

func TestLocateFromMeta(t *testing.T) {
	_, meta, _, _ := setupWorkspace(t)
	ws, err := Locate(meta)
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if ws.MetaRoot != meta {
		t.Fatalf("MetaRoot: got %q want %q", ws.MetaRoot, meta)
	}
	if ws.WorkingCopy != "" {
		t.Fatalf("WorkingCopy: got %q want empty", ws.WorkingCopy)
	}
}

func TestLocateFromWorkingCopy(t *testing.T) {
	_, meta, primary, _ := setupWorkspace(t)
	ws, err := Locate(primary)
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if ws.MetaRoot != meta {
		t.Fatalf("MetaRoot: got %q want %q", ws.MetaRoot, meta)
	}
	if ws.WorkingCopy != primary {
		t.Fatalf("WorkingCopy: got %q want %q", ws.WorkingCopy, primary)
	}
}

func TestLocateNotInWorkspace(t *testing.T) {
	root := t.TempDir()
	if _, err := Locate(root); err == nil {
		t.Fatal("expected error outside workspace")
	}
}

func TestEnumeratePeers(t *testing.T) {
	_, meta, primary, peer := setupWorkspace(t)
	peers, err := EnumeratePeers(meta)
	if err != nil {
		t.Fatalf("EnumeratePeers: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("got %d peers, want 2: %v", len(peers), peers)
	}
	// Sorted -- demo comes before demo-bug.
	if peers[0] != primary || peers[1] != peer {
		t.Fatalf("peers mismatch: got %v want [%s %s]", peers, primary, peer)
	}
}

func TestName(t *testing.T) {
	if got := Name("/x/y/demo-meta"); got != "demo" {
		t.Fatalf("got %q want demo", got)
	}
	if got := MetaDirName("foo"); got != "foo-meta" {
		t.Fatalf("got %q want foo-meta", got)
	}
}

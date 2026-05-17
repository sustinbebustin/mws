package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

// setupWorkspace builds a fake meta-at-root layout under a temp dir:
//
//	root/
//	  demo/                # meta workspace
//	    .mws.toml
//	    .mws/...
//	    .git/...
//	    main/              # working copy (untracked child)
//	    feature-x/         # peer working copy
//	    .envs/             # env staging (filtered out of peer enumeration)
//	  other/               # outside the meta entirely
func setupWorkspace(t *testing.T) (metaRoot, main, peer string) {
	t.Helper()
	root := t.TempDir()
	metaRoot = filepath.Join(root, "demo")
	if err := os.MkdirAll(metaRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(metaRoot, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".mws", ".git", ".envs"} {
		if err := os.MkdirAll(filepath.Join(metaRoot, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(name string) string {
		p := filepath.Join(metaRoot, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	main = mk("main")
	peer = mk("feature-x")
	return metaRoot, main, peer
}

func TestLocateFromMeta(t *testing.T) {
	meta, _, _ := setupWorkspace(t)
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
	meta, main, _ := setupWorkspace(t)
	ws, err := Locate(main)
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if ws.MetaRoot != meta {
		t.Fatalf("MetaRoot: got %q want %q", ws.MetaRoot, meta)
	}
	if ws.WorkingCopy != main {
		t.Fatalf("WorkingCopy: got %q want %q", ws.WorkingCopy, main)
	}
}

func TestLocateFromNestedDirInWorkingCopy(t *testing.T) {
	meta, main, _ := setupWorkspace(t)
	nested := filepath.Join(main, "frontend", "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := Locate(nested)
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if ws.MetaRoot != meta {
		t.Fatalf("MetaRoot: got %q want %q", ws.MetaRoot, meta)
	}
	if ws.WorkingCopy != main {
		t.Fatalf("WorkingCopy: got %q want %q (should be the top-level working copy)", ws.WorkingCopy, main)
	}
}

func TestLocateNotInWorkspace(t *testing.T) {
	if _, err := Locate(t.TempDir()); err == nil {
		t.Fatal("expected error outside workspace")
	}
}

func TestEnumerateWorkingCopies(t *testing.T) {
	meta, main, peer := setupWorkspace(t)
	peers, err := EnumerateWorkingCopies(meta)
	if err != nil {
		t.Fatalf("EnumerateWorkingCopies: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("got %d peers, want 2: %v", len(peers), peers)
	}
	// Sorted: feature-x before main.
	if peers[0] != peer || peers[1] != main {
		t.Fatalf("peers mismatch: got %v want [%s %s]", peers, peer, main)
	}
}

func TestEnumerateWorkingCopiesFiltersDotfileDirs(t *testing.T) {
	meta, _, _ := setupWorkspace(t)
	// Extra dotfile dir at meta root must not appear as a peer.
	if err := os.MkdirAll(filepath.Join(meta, ".extra"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A file at meta root must not appear either.
	if err := os.WriteFile(filepath.Join(meta, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	peers, err := EnumerateWorkingCopies(meta)
	if err != nil {
		t.Fatalf("EnumerateWorkingCopies: %v", err)
	}
	for _, p := range peers {
		base := filepath.Base(p)
		if base == ".extra" || base == ".mws" || base == ".envs" || base == ".git" || base == "README.md" {
			t.Fatalf("peer list contains forbidden entry: %s", p)
		}
	}
}

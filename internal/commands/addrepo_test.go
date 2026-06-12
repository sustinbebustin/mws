package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

func TestRunAddRepoOptionalRegistersWithoutCloning(t *testing.T) {
	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))
	if err := config.Save(metaRoot, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	// An existing working copy that must NOT receive a clone of the optional repo.
	peer := filepath.Join(metaRoot, "peer")
	mustMkdir(t, peer)

	withCwd(t, metaRoot, func() {
		if err := runAddRepo(context.Background(), nopReporter{}, "git@github.com:example/worker.git", "worker", true); err != nil {
			t.Fatalf("runAddRepo --optional: %v", err)
		}
	})

	cfg, err := config.Load(metaRoot)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(cfg.Repos) != 0 {
		t.Fatalf("optional registration must not touch [[repos]], got %+v", cfg.Repos)
	}
	if _, ok := cfg.OptionalRepo("worker"); !ok {
		t.Fatalf("worker not registered under optional_repos: %+v", cfg.OptionalRepos)
	}
	// No broadcast clone into existing copies.
	if _, err := os.Stat(filepath.Join(peer, "worker")); err == nil {
		t.Fatal("optional add-repo should not clone into existing working copies")
	}
}

func TestRunAddRepoOptionalRejectsDuplicateFolder(t *testing.T) {
	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))
	if err := config.Save(metaRoot, &config.Config{
		ProjectName: "demo",
		Repos:       []config.Repo{{Folder: "worker", URL: "u"}},
	}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	withCwd(t, metaRoot, func() {
		err := runAddRepo(context.Background(), nopReporter{}, "git@github.com:example/worker.git", "worker", true)
		if err == nil {
			t.Fatal("expected error registering an optional repo whose folder collides with a default repo")
		}
	})
}

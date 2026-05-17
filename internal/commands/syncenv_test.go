package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

// setupEnvLayout builds a meta-at-root workspace with one working copy "main",
// one repo "frontend" with two env mappings, and pre-populated staging files.
func setupEnvLayout(t *testing.T) (metaRoot, mainCopy string, cfg *config.Config) {
	t.Helper()
	root := t.TempDir()
	metaRoot = filepath.Join(root, "demo")
	mainCopy = filepath.Join(metaRoot, "main")

	mustMkdir(t, filepath.Join(metaRoot, project.HarnessDirName))
	mustMkdir(t, mainCopy)
	mustMkdir(t, filepath.Join(mainCopy, "frontend"))

	cfg = &config.Config{
		ProjectName: "demo",
		Repos: []config.Repo{{
			Folder: "frontend",
			URL:    "git@github.com:demo/frontend.git",
			Envs: []config.EnvMapping{
				{Source: "apps-internal.env", Target: "apps/internal/.env"},
				{Source: "root.env", Target: ".env"},
			},
		}},
	}
	if err := config.Save(metaRoot, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	return metaRoot, mainCopy, cfg
}

func TestRunSyncEnvAtMeta(t *testing.T) {
	meta, main, _ := setupEnvLayout(t)
	stagingFrontend := filepath.Join(meta, project.EnvStagingDirName, "frontend")
	mustMkdir(t, stagingFrontend)
	mustWriteFile(t, filepath.Join(stagingFrontend, "apps-internal.env"), "STAGED_INTERNAL=1")
	mustWriteFile(t, filepath.Join(stagingFrontend, "root.env"), "STAGED_ROOT=1")
	mustWriteFile(t, filepath.Join(main, "frontend", ".env"), "STALE")

	withCwd(t, meta, func() {
		if err := runSyncEnv(nopReporter{}, "main"); err != nil {
			t.Fatalf("runSyncEnv: %v", err)
		}
	})

	// .env at copy got overwritten with staged content.
	got, err := os.ReadFile(filepath.Join(main, "frontend", ".env"))
	if err != nil {
		t.Fatalf("read .env in copy: %v", err)
	}
	if string(got) != "STAGED_ROOT=1" {
		t.Fatalf(".env not overwritten with staged content: got %q", string(got))
	}

	// apps/internal/.env got created (with target subdir auto-created).
	internal, err := os.ReadFile(filepath.Join(main, "frontend", "apps", "internal", ".env"))
	if err != nil {
		t.Fatalf("read apps/internal/.env: %v", err)
	}
	if string(internal) != "STAGED_INTERNAL=1" {
		t.Fatalf("apps/internal/.env missing staged content: got %q", string(internal))
	}
}

func TestRunSyncEnvDefaultsToCurrentCopy(t *testing.T) {
	meta, main, _ := setupEnvLayout(t)
	stagingFrontend := filepath.Join(meta, project.EnvStagingDirName, "frontend")
	mustMkdir(t, stagingFrontend)
	mustWriteFile(t, filepath.Join(stagingFrontend, "root.env"), "DEFAULT_OK=1")

	withCwd(t, main, func() {
		if err := runSyncEnv(nopReporter{}, ""); err != nil {
			t.Fatalf("runSyncEnv: %v", err)
		}
	})

	got, err := os.ReadFile(filepath.Join(main, "frontend", ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if string(got) != "DEFAULT_OK=1" {
		t.Fatalf(".env not synced: got %q", string(got))
	}
}

func TestRunSyncEnvWarnsOnMissingStagedSource(t *testing.T) {
	meta, _, _ := setupEnvLayout(t)
	// No staged files at all -- both mappings should warn but not error.
	withCwd(t, meta, func() {
		if err := runSyncEnv(nopReporter{}, "main"); err != nil {
			t.Fatalf("runSyncEnv should warn but not fail: %v", err)
		}
	})
}

// withCwd temporarily changes the working directory for the duration of fn.
func withCwd(t *testing.T, dir string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()
	fn()
}

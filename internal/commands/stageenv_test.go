package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sustinbebustin/mws/internal/project"
)

func TestRunStageEnvCapturesIntoStaging(t *testing.T) {
	meta, main, _ := setupEnvLayout(t)

	// Working copy holds the real env values.
	mustMkdir(t, filepath.Join(main, "frontend", "apps", "internal"))
	mustWriteFile(t, filepath.Join(main, "frontend", "apps", "internal", ".env"), "LIVE_INTERNAL=1")
	mustWriteFile(t, filepath.Join(main, "frontend", ".env"), "LIVE_ROOT=1")

	withCwd(t, meta, func() {
		if err := runStageEnv(nopReporter{}, "main"); err != nil {
			t.Fatalf("runStageEnv: %v", err)
		}
	})

	stagingFrontend := filepath.Join(meta, project.EnvStagingDirName, "frontend")
	for _, p := range []struct{ name, want string }{
		{"apps-internal.env", "LIVE_INTERNAL=1"},
		{"root.env", "LIVE_ROOT=1"},
	} {
		got, err := os.ReadFile(filepath.Join(stagingFrontend, p.name))
		if err != nil {
			t.Fatalf("read staging %s: %v", p.name, err)
		}
		if string(got) != p.want {
			t.Fatalf("staging %s: got %q want %q", p.name, string(got), p.want)
		}
	}
}

func TestRunStageEnvOverwritesExistingStaging(t *testing.T) {
	meta, main, _ := setupEnvLayout(t)

	stagingFrontend := filepath.Join(meta, project.EnvStagingDirName, "frontend")
	mustMkdir(t, stagingFrontend)
	mustWriteFile(t, filepath.Join(stagingFrontend, "root.env"), "STALE_STAGED=1")
	mustWriteFile(t, filepath.Join(main, "frontend", ".env"), "FRESH_LIVE=1")

	withCwd(t, main, func() {
		if err := runStageEnv(nopReporter{}, ""); err != nil {
			t.Fatalf("runStageEnv: %v", err)
		}
	})

	got, err := os.ReadFile(filepath.Join(stagingFrontend, "root.env"))
	if err != nil {
		t.Fatalf("read staging: %v", err)
	}
	if string(got) != "FRESH_LIVE=1" {
		t.Fatalf("staging not overwritten: got %q", string(got))
	}
}

func TestRunStageEnvWarnsOnMissingLiveFile(t *testing.T) {
	meta, _, _ := setupEnvLayout(t)
	// No live env files at all; should warn but not fail.
	withCwd(t, meta, func() {
		if err := runStageEnv(nopReporter{}, "main"); err != nil {
			t.Fatalf("runStageEnv: %v", err)
		}
	})
}

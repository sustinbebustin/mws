package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

// seedMetaForInclude builds a meta workspace registering `optional` (its URL is
// filled in from a fresh bare upstream) and an empty `peer` working-copy
// directory ready to receive an include. Returns the meta root.
func seedMetaForInclude(t *testing.T, optional config.Repo) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))
	metaRoot, err := filepath.EvalSymlinks(metaRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(meta root): %v", err)
	}
	optional.URL = seedBareUpstream(t, root, "upstream-"+optional.Folder)
	cfg := &config.Config{
		ProjectName:   "demo",
		OptionalRepos: []config.Repo{optional},
	}
	if err := config.Save(metaRoot, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	mustMkdir(t, filepath.Join(metaRoot, "peer"))
	return metaRoot
}

func TestRunIncludeClonesIntoNamedCopy(t *testing.T) {
	metaRoot := seedMetaForInclude(t, config.Repo{Folder: "worker"})
	withCwd(t, metaRoot, func() {
		if err := runInclude(context.Background(), nopReporter{}, "worker", "peer", setupSkip); err != nil {
			t.Fatalf("runInclude: %v", err)
		}
	})
	if _, err := os.Stat(filepath.Join(metaRoot, "peer", "worker")); err != nil {
		t.Fatalf("worker not cloned into peer: %v", err)
	}
}

func TestRunIncludeDefaultsToCwdCopy(t *testing.T) {
	metaRoot := seedMetaForInclude(t, config.Repo{Folder: "worker"})
	peer := filepath.Join(metaRoot, "peer")
	// No copy name: the target is inferred from cwd (inside peer).
	withCwd(t, peer, func() {
		if err := runInclude(context.Background(), nopReporter{}, "worker", "", setupSkip); err != nil {
			t.Fatalf("runInclude: %v", err)
		}
	})
	if _, err := os.Stat(filepath.Join(peer, "worker")); err != nil {
		t.Fatalf("worker not cloned into cwd copy: %v", err)
	}
}

func TestRunIncludeUnknownFolderErrors(t *testing.T) {
	metaRoot := seedMetaForInclude(t, config.Repo{Folder: "worker"})
	var err error
	withCwd(t, metaRoot, func() {
		err = runInclude(context.Background(), nopReporter{}, "bogus", "peer", setupSkip)
	})
	if err == nil {
		t.Fatal("expected error for an unregistered optional folder")
	}
	if !strings.Contains(err.Error(), "no optional repo registered") || !strings.Contains(err.Error(), "worker") {
		t.Fatalf("error should name the registered optional repos: %v", err)
	}
}

func TestRunIncludeRunsSetup(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	metaRoot := seedMetaForInclude(t, config.Repo{
		Folder: "worker",
		Setup:  []config.SetupCommand{{Cmd: "touch ran-setup"}},
	})
	withCwd(t, metaRoot, func() {
		if err := runInclude(context.Background(), nopReporter{}, "worker", "peer", setupForceRun); err != nil {
			t.Fatalf("runInclude: %v", err)
		}
	})
	if _, err := os.Stat(filepath.Join(metaRoot, "peer", "worker", "ran-setup")); err != nil {
		t.Fatalf("optional repo setup did not run on include: %v", err)
	}
}

func TestRunIncludeCopiesEnv(t *testing.T) {
	metaRoot := seedMetaForInclude(t, config.Repo{
		Folder: "worker",
		Envs:   []config.EnvMapping{{Source: "worker.env", Target: ".env"}},
	})
	// Stage the env source the mapping points at.
	staging := filepath.Join(metaRoot, ".envs", "worker")
	mustMkdir(t, staging)
	mustWriteFile(t, filepath.Join(staging, "worker.env"), "TOKEN=abc\n")

	withCwd(t, metaRoot, func() {
		if err := runInclude(context.Background(), nopReporter{}, "worker", "peer", setupSkip); err != nil {
			t.Fatalf("runInclude: %v", err)
		}
	})
	got, err := os.ReadFile(filepath.Join(metaRoot, "peer", "worker", ".env"))
	if err != nil {
		t.Fatalf("env not copied into included repo: %v", err)
	}
	if string(got) != "TOKEN=abc\n" {
		t.Fatalf("env contents: got %q", string(got))
	}
}

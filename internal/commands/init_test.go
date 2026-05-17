package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

func TestExecuteInitGreenfieldNoRepos(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	parent := t.TempDir()
	p := &initPlan{
		ParentDir:   parent,
		ProjectName: "demo",
		Description: "demo project",
	}
	if err := executeInit(context.Background(), nopReporter{}, p); err != nil {
		t.Fatalf("executeInit: %v", err)
	}

	metaDir := filepath.Join(parent, "demo")
	mainCopy := filepath.Join(metaDir, "main")

	for _, p := range []string{
		filepath.Join(metaDir, ".mws.toml"),
		filepath.Join(metaDir, ".mws", "CLAUDE.md"),
		filepath.Join(metaDir, ".gitignore"),
		filepath.Join(metaDir, "README.md"),
		filepath.Join(metaDir, ".git"),
		mainCopy,
	} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}

	// Harness entry in main/ must be a symlink, NOT a .mws symlink (no back-link).
	if _, err := os.Lstat(filepath.Join(mainCopy, ".mws")); err == nil {
		t.Fatalf(".mws symlink in working copy is no longer used; should not exist")
	}
	claudeLink := filepath.Join(mainCopy, "CLAUDE.md")
	st, err := os.Lstat(claudeLink)
	if err != nil {
		t.Fatalf("Lstat %s: %v", claudeLink, err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to be a symlink", claudeLink)
	}

	c, err := config.Load(metaDir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if c.ProjectName != "demo" || c.Description != "demo project" {
		t.Fatalf("config not populated: %+v", c)
	}
}

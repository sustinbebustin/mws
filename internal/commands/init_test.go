package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

func TestExecuteInitGreenfieldNoRepos(t *testing.T) {
	parent := t.TempDir()
	p := &initPlan{
		ParentDir:   parent,
		ProjectName: "demo",
		Description: "demo project",
	}
	if err := executeInit(context.Background(), nopReporter{}, p); err != nil {
		t.Fatalf("executeInit: %v", err)
	}

	metaDir := filepath.Join(parent, "demo-meta")
	wc := filepath.Join(parent, "demo")

	for _, p := range []string{
		filepath.Join(metaDir, ".mws", "config.toml"),
		filepath.Join(metaDir, ".git"),
		filepath.Join(wc, ".mws"),
	} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}

	// Confirm .mws in working copy is a symlink and resolves to meta's .mws.
	st, err := os.Lstat(filepath.Join(wc, ".mws"))
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected .mws to be symlink, got %v err=%v", st.Mode(), err)
	}

	c, err := config.Load(metaDir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if c.ProjectName != "demo" || c.Description != "demo project" {
		t.Fatalf("config not populated: %+v", c)
	}
}

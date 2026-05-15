package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

func TestMigrateMetaAtRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	parent := t.TempDir()
	source := filepath.Join(parent, "demo")
	mustMkdir(t, source)

	// Harness content (top-level, no .git/).
	mustMkdir(t, filepath.Join(source, ".claude"))
	mustWriteFile(t, filepath.Join(source, ".claude", "agents.json"), `{}`)
	mustWriteFile(t, filepath.Join(source, "CLAUDE.md"), `# claude`)

	// Two native repos as subdirs with .git/.
	for _, name := range []string{"frontend", "backend"} {
		repo := filepath.Join(source, name)
		mustMkdir(t, repo)
		if err := exec.Command("git", "-C", repo, "init", "-q").Run(); err != nil {
			t.Fatal(err)
		}
	}

	plan, err := planMigrate(nopReporter{}, source)
	if err != nil {
		t.Fatalf("planMigrate: %v", err)
	}
	if plan.ProjectName != "demo" {
		t.Fatalf("project name: got %q", plan.ProjectName)
	}
	if len(plan.NativeRepos) != 2 {
		t.Fatalf("native repos: got %d want 2", len(plan.NativeRepos))
	}
	if len(plan.MetaEntries) != 2 {
		t.Fatalf("meta entries: got %d want 2", len(plan.MetaEntries))
	}

	if err := executeMigrate(context.Background(), nopReporter{}, plan); err != nil {
		t.Fatalf("executeMigrate: %v", err)
	}

	metaDir := filepath.Join(parent, "demo-meta")

	// .claude and CLAUDE.md moved into meta.
	for _, p := range []string{
		filepath.Join(metaDir, ".claude", "agents.json"),
		filepath.Join(metaDir, "CLAUDE.md"),
		filepath.Join(metaDir, ".mws", "config.toml"),
		filepath.Join(metaDir, ".git"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}

	// Native repos still in working copy.
	for _, name := range []string{"frontend", "backend"} {
		if _, err := os.Stat(filepath.Join(source, name, ".git")); err != nil {
			t.Fatalf("native repo %s missing: %v", name, err)
		}
	}

	// Working copy now has symlinks back to meta.
	for _, name := range []string{".claude", "CLAUDE.md", ".mws"} {
		linkPath := filepath.Join(source, name)
		st, err := os.Lstat(linkPath)
		if err != nil {
			t.Fatalf("missing symlink %s: %v", linkPath, err)
		}
		if st.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink", linkPath)
		}
	}

	cfg, err := config.Load(metaDir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("config repos: got %d want 2", len(cfg.Repos))
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

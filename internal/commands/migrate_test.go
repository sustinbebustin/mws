package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

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

// buildLegacySiblingMeta lays out the old sibling-meta shape under root:
//
//	root/
//	  demo-meta/.mws/config.toml + .git/ + CLAUDE.md + .claude/...
//	  demo/         (.mws -> ../demo-meta/.mws, plus old harness symlinks)
//	  demo-bug/     (.mws -> ../demo-meta/.mws, plus old harness symlinks)
//
// Returns the absolute path to <root>/demo-meta/ (old meta dir).
func buildLegacySiblingMeta(t *testing.T) (root, oldMeta, bareCopy, suffixedCopy string) {
	t.Helper()
	root = t.TempDir()
	oldMeta = filepath.Join(root, "demo-meta")
	mustMkdir(t, oldMeta)
	mustMkdir(t, filepath.Join(oldMeta, ".git"))
	mustMkdir(t, filepath.Join(oldMeta, ".mws"))
	mustMkdir(t, filepath.Join(oldMeta, ".claude"))
	mustWriteFile(t, filepath.Join(oldMeta, ".claude", "settings.json"), `{}`)
	mustWriteFile(t, filepath.Join(oldMeta, "CLAUDE.md"), "# demo")
	mustWriteFile(t, filepath.Join(oldMeta, ".mws", "config.toml"),
		"project_name = \"demo\"\n"+
			"description = \"demo workspace\"\n"+
			"\n"+
			"[[repos]]\n"+
			"  folder = \"frontend\"\n"+
			"  url = \"git@github.com:demo/frontend.git\"\n")

	mkCopy := func(name string) string {
		p := filepath.Join(root, name)
		mustMkdir(t, p)
		// Old peers symlink .mws back to the meta.
		if err := os.Symlink(filepath.Join(oldMeta, ".mws"), filepath.Join(p, ".mws")); err != nil {
			t.Fatal(err)
		}
		// Plus harness symlinks for entries directly at meta root.
		for _, name := range []string{".claude", "CLAUDE.md"} {
			if err := os.Symlink(filepath.Join(oldMeta, name), filepath.Join(p, name)); err != nil {
				t.Fatal(err)
			}
		}
		// A native repo lives in each copy.
		mustMkdir(t, filepath.Join(p, "frontend", ".git"))
		return p
	}

	bareCopy = mkCopy("demo")
	suffixedCopy = mkCopy("demo-bug")
	return root, oldMeta, bareCopy, suffixedCopy
}

func TestMigrateFromOldMetaPath(t *testing.T) {
	root, oldMeta, bareCopy, suffixedCopy := buildLegacySiblingMeta(t)

	plan, err := planMigrate(oldMeta)
	if err != nil {
		t.Fatalf("planMigrate: %v", err)
	}
	if plan.ProjectName != "demo" {
		t.Fatalf("project name: got %q want demo", plan.ProjectName)
	}
	if plan.NewMeta != filepath.Join(root, "demo") {
		t.Fatalf("new meta: got %q", plan.NewMeta)
	}
	if plan.Renames[bareCopy] != firstWorkingCopyName {
		t.Fatalf("bare copy rename: got %q want %q", plan.Renames[bareCopy], firstWorkingCopyName)
	}
	if plan.Renames[suffixedCopy] != "bug" {
		t.Fatalf("suffixed copy rename: got %q want %q", plan.Renames[suffixedCopy], "bug")
	}

	if err := executeMigrate(nopReporter{}, plan); err != nil {
		t.Fatalf("executeMigrate: %v", err)
	}

	newMeta := filepath.Join(root, "demo")
	for _, p := range []string{
		filepath.Join(newMeta, ".gitignore"),
		filepath.Join(newMeta, ".mws.toml"),
		filepath.Join(newMeta, ".git"),
		filepath.Join(newMeta, ".mws", ".claude", "settings.json"),
		filepath.Join(newMeta, ".mws", "CLAUDE.md"),
		filepath.Join(newMeta, "main"),
		filepath.Join(newMeta, "bug"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}

	// New config is loadable and preserves project name + repos.
	cfg, err := config.Load(newMeta)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.ProjectName != "demo" || len(cfg.Repos) != 1 || cfg.Repos[0].Folder != "frontend" {
		t.Fatalf("config not preserved: %+v", cfg)
	}

	// Allowlist .gitignore has the expected entries.
	gi, err := os.ReadFile(filepath.Join(newMeta, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(gi)
	for _, want := range []string{"/*", "!/.gitignore", "!/.mws.toml", "!/.mws/", "!/README.md"} {
		if !contains(body, want) {
			t.Fatalf("allowlist missing %q; got:\n%s", want, body)
		}
	}

	// Working copies have NO .mws symlink (no back-link in the new model).
	for _, copy := range []string{"main", "bug"} {
		if _, err := os.Lstat(filepath.Join(newMeta, copy, ".mws")); err == nil {
			t.Fatalf("%s/.mws should not exist", copy)
		}
	}

	// Working copies have refreshed harness symlinks pointing into ../.mws/.
	for _, copy := range []string{"main", "bug"} {
		linkPath := filepath.Join(newMeta, copy, "CLAUDE.md")
		st, err := os.Lstat(linkPath)
		if err != nil {
			t.Fatalf("Lstat %s: %v", linkPath, err)
		}
		if st.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s/CLAUDE.md is not a symlink", copy)
		}
		resolved, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			t.Fatalf("EvalSymlinks %s: %v", linkPath, err)
		}
		want, _ := filepath.EvalSymlinks(filepath.Join(newMeta, ".mws", "CLAUDE.md"))
		if resolved != want {
			t.Fatalf("%s/CLAUDE.md resolves to %s, want %s", copy, resolved, want)
		}
	}

	// Native repos preserved inside working copies.
	for _, copy := range []string{"main", "bug"} {
		if _, err := os.Stat(filepath.Join(newMeta, copy, "frontend", ".git")); err != nil {
			t.Fatalf("%s/frontend/.git missing: %v", copy, err)
		}
	}
}

func TestMigrateFromOldWorkingCopyPath(t *testing.T) {
	_, oldMeta, bareCopy, _ := buildLegacySiblingMeta(t)
	plan, err := planMigrate(bareCopy)
	if err != nil {
		t.Fatalf("planMigrate from working copy: %v", err)
	}
	if plan.OldMeta != oldMeta {
		t.Fatalf("old meta: got %q want %q", plan.OldMeta, oldMeta)
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func TestPlanMigrateRejectsNonMetaSuffix(t *testing.T) {
	// Setup: a directory with .mws/config.toml but NO -meta suffix. Must be rejected.
	root := t.TempDir()
	stray := filepath.Join(root, "looks-legit")
	mustMkdir(t, filepath.Join(stray, ".mws"))
	mustWriteFile(t, filepath.Join(stray, ".mws", "config.toml"), `project_name = "x"`+"\n")
	if _, err := planMigrate(stray); err == nil {
		t.Fatalf("planMigrate should refuse non -meta dir")
	}
}

func TestPlanMigrateRejectsPeerNameCollision(t *testing.T) {
	// Setup: two peers both want to map to the same new name.
	// projectName = "demo". Bare copy `demo` -> `main`. A peer literally named `main`
	// (with the same .mws backlink) also maps to `main`. Conflict.
	root := t.TempDir()
	oldMeta := filepath.Join(root, "demo-meta")
	mustMkdir(t, oldMeta)
	mustMkdir(t, filepath.Join(oldMeta, ".mws"))
	mustWriteFile(t, filepath.Join(oldMeta, ".mws", "config.toml"), `project_name = "demo"`+"\n")
	for _, name := range []string{"demo", "main"} {
		p := filepath.Join(root, name)
		mustMkdir(t, p)
		if err := os.Symlink(filepath.Join(oldMeta, ".mws"), filepath.Join(p, ".mws")); err != nil {
			t.Fatal(err)
		}
	}
	_, err := planMigrate(oldMeta)
	if err == nil || !strings.Contains(err.Error(), "both map to working-copy name") {
		t.Fatalf("expected peer-collision error, got %v", err)
	}
}

func TestExecuteMigrateReportsStagingOnFailure(t *testing.T) {
	// Force flattenStagingIntoHarness to fail by pre-creating an entry inside
	// the old meta's .mws/ that collides with a sibling at meta root.
	root := t.TempDir()
	oldMeta := filepath.Join(root, "demo-meta")
	mustMkdir(t, oldMeta)
	mustMkdir(t, filepath.Join(oldMeta, ".git"))
	mustMkdir(t, filepath.Join(oldMeta, ".mws"))
	mustWriteFile(t, filepath.Join(oldMeta, ".mws", "config.toml"), `project_name = "demo"`+"\n")
	// Both CLAUDE.md at meta root and CLAUDE.md inside .mws -- flatten will collide.
	mustWriteFile(t, filepath.Join(oldMeta, "CLAUDE.md"), "outer")
	mustWriteFile(t, filepath.Join(oldMeta, ".mws", "CLAUDE.md"), "inner")

	plan, err := planMigrate(oldMeta)
	if err != nil {
		t.Fatalf("planMigrate: %v", err)
	}
	err = executeMigrate(nopReporter{}, plan)
	if err == nil {
		t.Fatalf("executeMigrate should have failed on collision")
	}
	if !strings.Contains(err.Error(), "partial state at") {
		t.Fatalf("error should mention partial-state staging path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "-mws-migrate-") {
		t.Fatalf("error should name the staging path, got: %v", err)
	}
}

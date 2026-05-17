package project

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// setupHarness builds a meta-at-root layout with content inside .mws/ for fan-out.
func setupHarness(t *testing.T) (metaRoot, wc string) {
	t.Helper()
	root := t.TempDir()
	metaRoot = filepath.Join(root, "demo")
	wc = filepath.Join(metaRoot, "main")
	harness := filepath.Join(metaRoot, HarnessDirName)
	if err := os.MkdirAll(harness, 0o755); err != nil {
		t.Fatal(err)
	}
	// Harness contents (dirs and files alike).
	if err := os.MkdirAll(filepath.Join(harness, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(harness, ".workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"CLAUDE.md", "justfile", ".gitignore"} {
		if err := os.WriteFile(filepath.Join(harness, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return metaRoot, wc
}

func TestLinkHarnessIntoWorkingCopy(t *testing.T) {
	meta, wc := setupHarness(t)

	linked, err := LinkHarnessIntoWorkingCopy(meta, wc)
	if err != nil {
		t.Fatalf("LinkHarnessIntoWorkingCopy: %v", err)
	}

	want := []string{".claude", ".gitignore", ".workspace", "CLAUDE.md", "justfile"}
	slices.Sort(linked)
	if !slices.Equal(linked, want) {
		t.Fatalf("linked entries: got %v want %v", linked, want)
	}

	for _, name := range linked {
		linkPath := filepath.Join(wc, name)
		st, err := os.Lstat(linkPath)
		if err != nil {
			t.Fatalf("Lstat %s: %v", linkPath, err)
		}
		if st.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink", linkPath)
		}
		resolved, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			t.Fatalf("EvalSymlinks %s: %v", linkPath, err)
		}
		wantTarget, err := filepath.EvalSymlinks(filepath.Join(meta, HarnessDirName, name))
		if err != nil {
			t.Fatalf("EvalSymlinks meta entry: %v", err)
		}
		if resolved != wantTarget {
			t.Fatalf("%s resolves to %s, want %s", linkPath, resolved, wantTarget)
		}
	}
}

func TestLinkHarnessUsesRelativeTargets(t *testing.T) {
	meta, wc := setupHarness(t)
	if _, err := LinkHarnessIntoWorkingCopy(meta, wc); err != nil {
		t.Fatalf("LinkHarnessIntoWorkingCopy: %v", err)
	}
	link := filepath.Join(wc, "CLAUDE.md")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if filepath.IsAbs(target) {
		t.Fatalf("symlink target should be relative, got %q", target)
	}
	wantPrefix := filepath.Join("..", HarnessDirName)
	if !filepath.IsAbs(target) && filepath.Dir(target) != wantPrefix {
		t.Fatalf("symlink %q does not point through ../%s/", target, HarnessDirName)
	}
}

func TestLinkHarnessPreservesExistingNonSymlinks(t *testing.T) {
	meta, wc := setupHarness(t)
	// Pre-create a real file in wc at a name that also exists in the harness.
	if err := os.MkdirAll(wc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wc, "CLAUDE.md"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LinkHarnessIntoWorkingCopy(meta, wc); err != nil {
		t.Fatalf("LinkHarnessIntoWorkingCopy: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(wc, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "local" {
		t.Fatalf("local file overwritten: got %q want %q", string(got), "local")
	}
}

func TestLinkHarnessRefreshesStaleSymlinks(t *testing.T) {
	meta, wc := setupHarness(t)
	if err := os.MkdirAll(wc, 0o755); err != nil {
		t.Fatal(err)
	}
	// Existing symlink points somewhere bogus -- should be replaced.
	if err := os.Symlink("/nonexistent", filepath.Join(wc, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := LinkHarnessIntoWorkingCopy(meta, wc); err != nil {
		t.Fatalf("LinkHarnessIntoWorkingCopy: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(wc, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(meta, HarnessDirName, "CLAUDE.md"))
	if resolved != want {
		t.Fatalf("stale symlink not refreshed: got %s want %s", resolved, want)
	}
}

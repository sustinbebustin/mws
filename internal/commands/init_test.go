package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

func TestExecuteInitGreenfieldNoRepos(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	isolateGit(t, true)

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

func TestExecuteInitPlacesMainCopyUnderWorkingCopiesDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	isolateGit(t, true)

	parent := t.TempDir()
	p := &initPlan{
		ParentDir:        parent,
		ProjectName:      "demo",
		Description:      "demo project",
		WorkingCopiesDir: "copies",
	}
	if err := executeInit(context.Background(), nopReporter{}, p); err != nil {
		t.Fatalf("executeInit: %v", err)
	}

	metaDir := filepath.Join(parent, "demo")
	mainCopy := filepath.Join(metaDir, "copies", "main")

	if _, err := os.Stat(mainCopy); err != nil {
		t.Fatalf("expected main copy at %s: %v", mainCopy, err)
	}
	// And NOT at the meta root.
	if _, err := os.Stat(filepath.Join(metaDir, "main")); err == nil {
		t.Fatalf("main copy should not exist at meta root when working_copies_dir is set")
	}

	// Harness symlink fanned out into the nested copy.
	st, err := os.Lstat(filepath.Join(mainCopy, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("missing CLAUDE.md symlink in main copy: %v", err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("CLAUDE.md in nested main copy is not a symlink")
	}

	c, err := config.Load(metaDir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if c.WorkingCopiesDir != "copies" {
		t.Fatalf("config.WorkingCopiesDir: got %q want %q", c.WorkingCopiesDir, "copies")
	}
}

// isolateGit points git at a private global config so a test sees no ambient
// identity, and, when withIdentity is set, gives it a fixed one.
func isolateGit(t *testing.T, withIdentity bool) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	body := "[user]\n\tuseConfigOnly = true\n"
	if withIdentity {
		body = "[user]\n\tname = Test\n\temail = test@example.com\n"
	}
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("HOME", t.TempDir())
	for _, k := range []string{
		"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL",
	} {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInitCommitsTheTrackedAllowlist(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	isolateGit(t, true)
	parent := t.TempDir()

	res := runMWS(t, parent, "init", "demo", "--yes")
	if res.code != 0 {
		t.Fatalf("init exit %d\nstderr: %s", res.code, res.stderr)
	}

	meta := filepath.Join(parent, "demo")
	if log := gitT(t, meta, "log", "--oneline"); strings.Contains(log, "\n") || log == "" {
		t.Fatalf("want exactly one commit, got log:\n%s", log)
	}
	got := strings.Split(gitT(t, meta, "ls-tree", "-r", "--name-only", "HEAD"), "\n")
	for _, want := range []string{".gitignore", ".mws.toml", "README.md", "CLAUDE.md", ".mws/CLAUDE.md"} {
		if !slices.Contains(got, want) {
			t.Errorf("commit lacks %s; has %v", want, got)
		}
	}
	for _, f := range got {
		switch {
		case f == ".gitignore", f == ".mws.toml", f == "README.md", f == "CLAUDE.md", strings.HasPrefix(f, ".mws/"):
		default:
			t.Errorf("commit holds %s, outside the tracked allowlist", f)
		}
	}
	if status := gitT(t, meta, "status", "--porcelain"); status != "" {
		t.Errorf("meta workspace not clean after init:\n%s", status)
	}
}

func TestWtNewWorksInFreshWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	isolateGit(t, true)
	parent := t.TempDir()
	upstream := seedBareUpstream(t, t.TempDir(), "web")
	if res := runMWS(t, parent, "init", "demo", "--yes", "--repo", "web="+upstream); res.code != 0 {
		t.Fatalf("init exit %d\nstderr: %s", res.code, res.stderr)
	}

	res := runWt(t, filepath.Join(parent, "demo", "main"), "new", "04", "--no-setup")
	if res.code != 0 {
		t.Fatalf("wt new exit %d\nstderr: %s", res.code, res.stderr)
	}
}

func TestInitWithoutGitIdentityWritesNothing(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	isolateGit(t, false)
	parent := t.TempDir()

	res := runMWS(t, parent, "init", "demo", "--yes")
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	for _, want := range []string{"git config --global user.name", "git config --global user.email"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, res.stderr)
		}
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("init wrote %d entries before failing", len(entries))
	}
}

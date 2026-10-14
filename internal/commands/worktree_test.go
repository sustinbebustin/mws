package commands

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

// wtFixture is a meta workspace shaped like a real one: a git-versioned meta
// root with an allowlist .gitignore, a harness, env staging, and one working
// copy "c" holding two native repos on an integration branch.
type wtFixture struct {
	root string // parent of the meta root; holds the bare upstreams
	meta string
	copy string
	cfg  *config.Config
}

// repo returns the copy's checkout of a native repo.
func (f wtFixture) repo(folder string) string { return filepath.Join(f.copy, folder) }

// wtPath returns where `mws wt new name` places a worktree under the defaults.
func (f wtFixture) wtPath(name string) string { return filepath.Join(f.copy, ".wt", name) }

// saveConfig rewrites .mws.toml after a test tweaks f.cfg.
func (f wtFixture) saveConfig(t *testing.T) {
	t.Helper()
	if err := config.Save(f.meta, f.cfg); err != nil {
		t.Fatal(err)
	}
}

func seedWtWorkspace(t *testing.T) wtFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := wtFixture{root: root, meta: filepath.Join(root, "demo")}
	f.copy = filepath.Join(f.meta, "c")

	harness := filepath.Join(f.meta, project.HarnessDirName)
	writeFileAll(t, filepath.Join(harness, "CLAUDE.md"), "# harness")
	writeFileAll(t, filepath.Join(harness, ".claude", "settings.json"), "{}")
	// The harness ships its own .gitignore; the worktree root must keep the
	// tracked meta allowlist instead of linking this one.
	writeFileAll(t, filepath.Join(harness, ".gitignore"), "*.bak\n")
	writeFileAll(t, filepath.Join(f.meta, ".gitignore"), "/*\n!/.gitignore\n!/.mws.toml\n!/.mws/\n")
	writeFileAll(t, filepath.Join(f.meta, project.EnvStagingDirName, "frontend", "app.env"), "STAGED=1\n")

	f.cfg = &config.Config{
		ProjectName: "demo",
		Repos: []config.Repo{
			{Folder: "backend", URL: seedBareUpstream(t, root, "upstream-backend")},
			{
				Folder: "frontend",
				URL:    seedBareUpstream(t, root, "upstream-frontend"),
				Envs:   []config.EnvMapping{{Source: "app.env", Target: "app.env"}},
			},
		},
		OptionalRepos: []config.Repo{{Folder: "extra", URL: seedBareUpstream(t, root, "upstream-extra")}},
	}
	f.saveConfig(t)
	gitT(t, f.meta, "init", "-q", "-b", "main")
	gitT(t, f.meta, "add", "-A")
	gitT(t, f.meta, "commit", "-q", "-m", "meta init")

	mustMkdir(t, f.copy)
	if _, err := project.LinkHarnessIntoWorkingCopy(f.meta, f.copy); err != nil {
		t.Fatal(err)
	}
	for _, r := range f.cfg.Repos {
		cloneIntegration(t, r.URL, f.repo(r.Folder))
	}
	writeFileAll(t, filepath.Join(f.repo("frontend"), "app.env"), "SLOT=3\n")
	writeFileAll(t, filepath.Join(f.copy, ".ports.env"), "PITCH_SLOT=3\n")
	writeFileAll(t, filepath.Join(f.copy, ".mcp.json"), `{"mcpServers":{}}`)
	writeFileAll(t, filepath.Join(f.copy, ".scratch", "T-1.md"), "ticket")
	return f
}

// cloneIntegration clones url into dst and commits on a feat/integration
// branch, so a worktree based on the checkout's HEAD differs from main.
func cloneIntegration(t *testing.T, url, dst string) {
	t.Helper()
	gitT(t, "", "clone", "-q", url, dst)
	gitT(t, dst, "switch", "-q", "-c", "feat/integration")
	writeFileAll(t, filepath.Join(dst, ".gitignore"), "app.env\n")
	gitT(t, dst, "add", "-A")
	gitT(t, dst, "commit", "-q", "-m", "integration base")
}

// gitT runs git in dir with a fixed identity and returns trimmed stdout.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

// wtResult is one in-process invocation of `mws wt ...`.
type wtResult struct {
	stdout, stderr string
	code           int
}

// lastLine is the final non-empty stdout line: the path contract of `wt new`.
func (r wtResult) lastLine() string {
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	return lines[len(lines)-1]
}

// runWt executes `mws wt <args>` from cwd through the real root command and
// maps the returned error to the process exit code main would use.
func runWt(t *testing.T, cwd string, args ...string) wtResult {
	t.Helper()
	var out, errb bytes.Buffer
	var err error
	withCwd(t, cwd, func() {
		root := NewRootCmd()
		root.SetArgs(append([]string{"wt"}, args...))
		root.SetOut(&out)
		root.SetErr(&errb)
		err = root.ExecuteContext(context.Background())
	})
	if err != nil {
		errb.WriteString(err.Error())
	}
	return wtResult{stdout: out.String(), stderr: errb.String(), code: ExitCode(err)}
}

// writeFileAll writes body to p, creating p's parent directories.
func writeFileAll(t *testing.T, p, body string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(p))
	mustWriteFile(t, p, body)
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWtNewCreatesLayout(t *testing.T) {
	f := seedWtWorkspace(t)
	wt := f.wtPath("x")

	res := runWt(t, f.copy, "new", "x", "--no-setup")
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	if res.lastLine() != wt {
		t.Fatalf("last stdout line = %q, want %q", res.lastLine(), wt)
	}

	// Root: a detached linked worktree of the meta repo, clean, staging nothing.
	if got := gitT(t, wt, "rev-parse", "--show-toplevel"); got != wt {
		t.Fatalf("root git toplevel = %q, want the worktree root", got)
	}
	if got := gitT(t, wt, "branch", "--show-current"); got != "" {
		t.Fatalf("anchor should be detached, on %q", got)
	}
	if got := gitT(t, wt, "status", "--porcelain"); got != "" {
		t.Fatalf("root git status not empty:\n%s", got)
	}
	if got := gitT(t, wt, "add", "-A", "--dry-run"); got != "" {
		t.Fatalf("root git add -A would stage:\n%s", got)
	}
	if got := readFile(t, filepath.Join(wt, ".gitignore")); !strings.HasPrefix(got, "/*\n") {
		t.Fatalf("root .gitignore should be the tracked meta allowlist, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(wt, project.WorktreeMarkerFileName)); err != nil {
		t.Fatalf("marker missing: %v", err)
	}

	// Inner repos: one git worktree each, on wt/x, at the copy's HEAD.
	for _, folder := range []string{"backend", "frontend"} {
		inner := filepath.Join(wt, folder)
		if got := gitT(t, inner, "branch", "--show-current"); got != "wt/x" {
			t.Fatalf("%s branch = %q, want wt/x", folder, got)
		}
		if got, want := gitT(t, inner, "rev-parse", "HEAD"), gitT(t, f.repo(folder), "rev-parse", "HEAD"); got != want {
			t.Fatalf("%s based on %s, want copy HEAD %s", folder, got, want)
		}
		if got, want := gitT(t, inner, "rev-parse", "--git-common-dir"), filepath.Join(f.repo(folder), ".git"); got != want {
			t.Fatalf("%s common dir = %q, want the copy's repo %q", folder, got, want)
		}
	}

	// Harness: relative symlinks into .mws/.
	link, err := os.Readlink(filepath.Join(wt, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not a symlink: %v", err)
	}
	if link != filepath.Join("..", "..", "..", project.HarnessDirName, "CLAUDE.md") {
		t.Fatalf("CLAUDE.md -> %q, want a relative link into the harness", link)
	}
	if _, err := os.Stat(filepath.Join(wt, ".claude", "settings.json")); err != nil {
		t.Fatalf(".claude not reachable: %v", err)
	}

	// Per-copy state: root files copied, non-repo dirs linked, env from the copy.
	if got := readFile(t, filepath.Join(wt, ".ports.env")); got != "PITCH_SLOT=3\n" {
		t.Fatalf(".ports.env = %q", got)
	}
	if st, err := os.Lstat(filepath.Join(wt, ".ports.env")); err != nil || !st.Mode().IsRegular() {
		t.Fatalf(".ports.env should be a regular-file copy: %v", err)
	}
	if got := readFile(t, filepath.Join(wt, ".mcp.json")); got != `{"mcpServers":{}}` {
		t.Fatalf(".mcp.json = %q", got)
	}
	if link, err := os.Readlink(filepath.Join(wt, ".scratch")); err != nil || link != filepath.Join("..", "..", ".scratch") {
		t.Fatalf(".scratch -> %q (%v), want ../../.scratch", link, err)
	}
	if got := readFile(t, filepath.Join(wt, "frontend", "app.env")); got != "SLOT=3\n" {
		t.Fatalf("frontend/app.env = %q, want the copy's file, not staging", got)
	}
	if _, err := os.Lstat(filepath.Join(wt, ".wt")); err == nil {
		t.Fatal("the worktrees dir must not be linked into a worktree")
	}

	// `wt path` locates it.
	p := runWt(t, f.meta+"/c/frontend", "path", "x")
	if p.code != 0 || strings.TrimSpace(p.stdout) != wt {
		t.Fatalf("wt path: exit %d stdout %q stderr %s", p.code, p.stdout, p.stderr)
	}
}

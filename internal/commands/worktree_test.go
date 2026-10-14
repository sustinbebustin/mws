package commands

import (
	"bytes"
	"context"
	"encoding/json"
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
	return runWtIn(t, cwd, "", args...)
}

// runWtIn is runWt with stdin fed from the given string.
func runWtIn(t *testing.T, cwd, stdin string, args ...string) wtResult {
	t.Helper()
	var out, errb bytes.Buffer
	var err error
	withCwd(t, cwd, func() {
		root := NewRootCmd()
		root.SetArgs(append([]string{"wt"}, args...))
		root.SetIn(strings.NewReader(stdin))
		root.SetOut(&out)
		root.SetErr(&errb)
		err = root.ExecuteContext(context.Background())
	})
	if err != nil && !Silent(err) {
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

// mustNew runs `wt new` and fails the test unless it exits 0.
func mustNew(t *testing.T, cwd string, args ...string) wtResult {
	t.Helper()
	res := runWt(t, cwd, append([]string{"new"}, args...)...)
	if res.code != 0 {
		t.Fatalf("wt new %v: exit %d\nstderr: %s", args, res.code, res.stderr)
	}
	return res
}

func TestWtNewBranchAndBaseFlags(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "b", "--branch", "feat/b", "--base", "main", "--no-setup")

	for _, folder := range []string{"backend", "frontend"} {
		inner := filepath.Join(f.wtPath("b"), folder)
		if got := gitT(t, inner, "branch", "--show-current"); got != "feat/b" {
			t.Fatalf("%s branch = %q, want feat/b", folder, got)
		}
		if got, want := gitT(t, inner, "rev-parse", "HEAD"), gitT(t, f.repo(folder), "rev-parse", "main"); got != want {
			t.Fatalf("%s at %s, want main %s", folder, got, want)
		}
	}
}

func TestWtNewUnknownBaseIsUsageErrorAndCreatesNothing(t *testing.T) {
	f := seedWtWorkspace(t)
	res := runWt(t, f.copy, "new", "b", "--base", "no-such-ref", "--no-setup")
	if res.code != 2 {
		t.Fatalf("exit %d, want 2\nstderr: %s", res.code, res.stderr)
	}
	if _, err := os.Lstat(filepath.Join(f.copy, ".wt")); err == nil {
		t.Fatal("a refused create must not leave the worktrees dir behind")
	}
}

// Run from inside a worktree, a new worktree is based on that worktree's
// checkouts and still lands as a sibling under the copy.
func TestWtNewFromInsideWorktreeBasesOnIt(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	xFront := filepath.Join(f.wtPath("x"), "frontend")
	writeFileAll(t, filepath.Join(xFront, "feature.txt"), "x work")
	gitT(t, xFront, "add", "feature.txt")
	gitT(t, xFront, "commit", "-q", "-m", "x work")

	res := mustNew(t, filepath.Join(xFront, ".."), "y", "--no-setup")
	if res.lastLine() != f.wtPath("y") {
		t.Fatalf("nested worktree at %q, want sibling %q", res.lastLine(), f.wtPath("y"))
	}
	if got, want := gitT(t, filepath.Join(f.wtPath("y"), "frontend"), "rev-parse", "HEAD"), gitT(t, xFront, "rev-parse", "HEAD"); got != want {
		t.Fatalf("y/frontend based on %s, want x's HEAD %s", got, want)
	}
	if got := gitT(t, filepath.Join(f.wtPath("y"), "frontend"), "branch", "--show-current"); got != "wt/y" {
		t.Fatalf("y/frontend branch = %q, want wt/y", got)
	}
}

func TestWtNewIncludesOptionalRepoPresentInCopy(t *testing.T) {
	f := seedWtWorkspace(t)
	gitT(t, "", "clone", "-q", f.cfg.OptionalRepos[0].URL, f.repo("extra"))

	mustNew(t, f.copy, "x", "--no-setup")
	if got := gitT(t, filepath.Join(f.wtPath("x"), "extra"), "branch", "--show-current"); got != "wt/x" {
		t.Fatalf("extra branch = %q, want wt/x", got)
	}
}

func TestWtNewWarnsOnMissingEnvTarget(t *testing.T) {
	f := seedWtWorkspace(t)
	if err := os.Remove(filepath.Join(f.repo("frontend"), "app.env")); err != nil {
		t.Fatal(err)
	}

	res := mustNew(t, f.copy, "x", "--no-setup")
	if !strings.Contains(res.stderr, "app.env") {
		t.Fatalf("stderr should warn about the missing env target:\n%s", res.stderr)
	}
	if _, err := os.Stat(filepath.Join(f.wtPath("x"), "frontend", "app.env")); err == nil {
		t.Fatal("env target must not be invented, e.g. from staging")
	}
}

// An existing branch that no worktree holds is checked out as is, in the
// repos that have it; the others branch from HEAD.
func TestWtNewReusesUnattachedBranch(t *testing.T) {
	f := seedWtWorkspace(t)
	gitT(t, f.repo("frontend"), "branch", "wt/r", "main")
	mainSHA := gitT(t, f.repo("frontend"), "rev-parse", "main")

	mustNew(t, f.copy, "r", "--no-setup")
	if got := gitT(t, filepath.Join(f.wtPath("r"), "frontend"), "rev-parse", "HEAD"); got != mainSHA {
		t.Fatalf("frontend at %s, want the reused branch tip %s", got, mainSHA)
	}
	if got, want := gitT(t, filepath.Join(f.wtPath("r"), "backend"), "rev-parse", "HEAD"), gitT(t, f.repo("backend"), "rev-parse", "HEAD"); got != want {
		t.Fatalf("backend at %s, want copy HEAD %s", got, want)
	}
}

// newJSONOut mirrors the documented `wt new --json` shape.
type newJSONOut struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Copy   string `json:"copy"`
	Branch string `json:"branch"`
	Reused bool   `json:"reused"`
	Repos  []struct {
		Folder string `json:"folder"`
		Path   string `json:"path"`
		Branch string `json:"branch"`
		Base   string `json:"base"`
	} `json:"repos"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeNew(t *testing.T, res wtResult) newJSONOut {
	t.Helper()
	var out newJSONOut
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, res.stdout)
	}
	return out
}

func TestWtNewJSONShape(t *testing.T) {
	f := seedWtWorkspace(t)
	out := decodeNew(t, mustNew(t, f.copy, "x", "--no-setup", "--json"))

	if out.Name != "x" || out.Path != f.wtPath("x") || out.Copy != f.copy || out.Branch != "wt/x" || out.Reused || out.Error != nil {
		t.Fatalf("unexpected result: %+v", out)
	}
	if len(out.Repos) != 2 {
		t.Fatalf("repos = %+v, want backend and frontend", out.Repos)
	}
	for i, folder := range []string{"backend", "frontend"} {
		r := out.Repos[i]
		want := gitT(t, f.repo(folder), "rev-parse", "HEAD")
		if r.Folder != folder || r.Path != filepath.Join(f.wtPath("x"), folder) || r.Branch != "wt/x" || r.Base != want {
			t.Fatalf("repo %d = %+v, want %s on wt/x from %s", i, r, folder, want)
		}
	}
}

func TestWtNewIsIdempotent(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")

	plain := mustNew(t, f.copy, "x")
	if plain.lastLine() != f.wtPath("x") {
		t.Fatalf("rerun printed %q, want %q", plain.lastLine(), f.wtPath("x"))
	}
	out := decodeNew(t, mustNew(t, f.copy, "x", "--json"))
	if !out.Reused || out.Path != f.wtPath("x") || len(out.Repos) != 2 || out.Repos[0].Branch != "wt/x" {
		t.Fatalf("rerun --json = %+v, want reused x with its repos", out)
	}
	if n := len(strings.Split(gitT(t, f.meta, "worktree", "list"), "\n")); n != 2 {
		t.Fatalf("meta has %d worktrees after a rerun, want main + x", n)
	}

	// A reuse reports the branches the worktree is on, not the ones asked for.
	if out := decodeNew(t, mustNew(t, f.copy, "x", "--branch", "other", "--json")); out.Branch != "wt/x" {
		t.Fatalf("rerun --branch other reported branch %q, want x's own wt/x", out.Branch)
	}
	gitT(t, filepath.Join(f.wtPath("x"), "frontend"), "switch", "-q", "--detach")
	out = decodeNew(t, mustNew(t, f.copy, "x", "--json"))
	if out.Branch != "" || out.Repos[0].Branch != "wt/x" || out.Repos[1].Branch != "" {
		t.Fatalf("rerun with a detached frontend = %+v, want no common branch and frontend detached", out)
	}
}

// assertNothingCreated checks that a failed or refused `wt new name` left no
// directory, registration or branch behind.
func assertNothingCreated(t *testing.T, f wtFixture, name string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(f.copy, ".wt")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(f.copy, ".wt"))
		for _, e := range entries {
			if e.Name() == name {
				t.Fatalf("%s left behind", f.wtPath(name))
			}
		}
	}
	for _, dir := range []string{f.meta, f.repo("backend"), f.repo("frontend")} {
		for _, line := range strings.Split(gitT(t, dir, "worktree", "list", "--porcelain"), "\n") {
			if strings.HasPrefix(line, "worktree ") && strings.Contains(line, filepath.Join(".wt", name)) {
				t.Fatalf("%s still registers %s", dir, line)
			}
		}
	}
	for _, folder := range []string{"backend", "frontend"} {
		if got := gitT(t, f.repo(folder), "branch", "--list", "wt/"+name); got != "" {
			t.Fatalf("%s kept branch wt/%s", folder, name)
		}
	}
}

func TestWtNewRollsBackWhenAnInnerRepoFails(t *testing.T) {
	f := seedWtWorkspace(t)
	// frontend sorts after backend, so backend's worktree exists when this fires;
	// a failing post-checkout hook also leaves frontend's branch and registration.
	writeFileAll(t, filepath.Join(f.repo("frontend"), ".git", "hooks", "post-checkout"), "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(f.repo("frontend"), ".git", "hooks", "post-checkout"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := runWt(t, f.copy, "new", "x", "--no-setup")
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "removed") {
		t.Fatalf("error should say what happened to the partial worktree:\n%s", res.stderr)
	}
	assertNothingCreated(t, f, "x")
	if _, err := os.Lstat(filepath.Join(f.copy, ".wt")); err == nil {
		t.Fatal("the worktrees dir created by the failed run is still there")
	}
}

func TestWtNewRefusesBranchCheckedOutElsewhere(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")

	res := runWt(t, f.copy, "new", "y", "--branch", "wt/x", "--no-setup", "--json")
	if res.code != 3 {
		t.Fatalf("exit %d, want 3\nstderr: %s", res.code, res.stderr)
	}
	out := decodeNew(t, res)
	if out.Error == nil || out.Error.Code != "refused" || !strings.Contains(out.Error.Message, f.wtPath("x")) {
		t.Fatalf("error = %+v, want refused naming where wt/x is checked out", out.Error)
	}
	assertNothingCreated(t, f, "y")

	// The copy's own checkout counts as elsewhere too.
	res = runWt(t, f.copy, "new", "z", "--branch", "feat/integration", "--no-setup")
	if res.code != 3 || !strings.Contains(res.stderr, f.repo("backend")) {
		t.Fatalf("exit %d, want 3 naming the copy's checkout\nstderr: %s", res.code, res.stderr)
	}
}

func TestWtNewRefusesRepoMidMerge(t *testing.T) {
	f := seedWtWorkspace(t)
	front := f.repo("frontend")
	gitT(t, front, "switch", "-q", "-c", "side", "main")
	writeFileAll(t, filepath.Join(front, "side.txt"), "side")
	gitT(t, front, "add", "side.txt")
	gitT(t, front, "commit", "-q", "-m", "side")
	gitT(t, front, "switch", "-q", "feat/integration")
	gitT(t, front, "merge", "-q", "--no-ff", "--no-commit", "side")

	res := runWt(t, f.copy, "new", "x", "--no-setup")
	if res.code != 3 || !strings.Contains(res.stderr, "merge") {
		t.Fatalf("exit %d, want 3 mentioning the merge\nstderr: %s", res.code, res.stderr)
	}
	assertNothingCreated(t, f, "x")
}

func TestWtNewRefusesUnmanagedDirectory(t *testing.T) {
	f := seedWtWorkspace(t)
	writeFileAll(t, filepath.Join(f.wtPath("u"), "notes.txt"), "keep me")

	res := runWt(t, f.copy, "new", "u", "--no-setup")
	if res.code != 3 {
		t.Fatalf("exit %d, want 3\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, filepath.Join(f.wtPath("u"), "notes.txt")); got != "keep me" {
		t.Fatalf("refused create touched the directory: %q", got)
	}
}

func TestWtNewUsageErrors(t *testing.T) {
	f := seedWtWorkspace(t)
	for name, args := range map[string][]string{
		"bad name":     {"new", ".hidden"},
		"no name":      {"new"},
		"bad branch":   {"new", "x", "--branch", "a..b"},
		"unknown flag": {"new", "x", "--nope"},
	} {
		if res := runWt(t, f.copy, args...); res.code != 2 {
			t.Fatalf("%s: exit %d, want 2\nstderr: %s", name, res.code, res.stderr)
		}
	}
	if out := decodeNew(t, runWt(t, f.copy, "new", "--json")); out.Error == nil || out.Error.Code != "usage" {
		t.Fatalf("missing name with --json: error = %+v, want code usage", out.Error)
	}
	res := runWt(t, f.meta, "new", "x", "--json")
	if res.code != 2 {
		t.Fatalf("from the meta root: exit %d, want 2\nstderr: %s", res.code, res.stderr)
	}
	if out := decodeNew(t, res); out.Error == nil || out.Error.Code != "usage" {
		t.Fatalf("from the meta root: error = %+v, want code usage", out.Error)
	}
}

func TestWtNewRunsSetup(t *testing.T) {
	f := seedWtWorkspace(t)
	f.cfg.Repos[0].Setup = []config.SetupCommand{{Cmd: "touch repo-setup-ran"}}
	f.cfg.Worktrees = &config.Worktrees{Setup: []config.SetupCommand{{Cmd: "touch root-setup-ran"}}}
	f.saveConfig(t)

	mustNew(t, f.copy, "x")
	for _, p := range []string{"backend/repo-setup-ran", "root-setup-ran"} {
		if _, err := os.Stat(filepath.Join(f.wtPath("x"), p)); err != nil {
			t.Fatalf("setup did not create %s: %v", p, err)
		}
	}
	// The copy itself is untouched by setup.
	if _, err := os.Stat(filepath.Join(f.repo("backend"), "repo-setup-ran")); err == nil {
		t.Fatal("repo setup ran in the working copy instead of the worktree")
	}
}

func TestWtNewRepoSetupFalseRunsOnlyRootSetup(t *testing.T) {
	f := seedWtWorkspace(t)
	off := false
	f.cfg.Repos[0].Setup = []config.SetupCommand{{Cmd: "touch repo-setup-ran"}}
	f.cfg.Worktrees = &config.Worktrees{RepoSetup: &off, Setup: []config.SetupCommand{{Cmd: "touch root-setup-ran"}}}
	f.saveConfig(t)

	mustNew(t, f.copy, "x")
	if _, err := os.Stat(filepath.Join(f.wtPath("x"), "backend", "repo-setup-ran")); err == nil {
		t.Fatal("repo_setup = false still ran [[repos.setup]]")
	}
	if _, err := os.Stat(filepath.Join(f.wtPath("x"), "root-setup-ran")); err != nil {
		t.Fatalf("[[worktrees.setup]] did not run: %v", err)
	}
}

func TestWtNewSetupFailureKeepsWorktree(t *testing.T) {
	f := seedWtWorkspace(t)
	f.cfg.Repos[1].Setup = []config.SetupCommand{{Cmd: "exit 7"}}
	f.saveConfig(t)

	res := runWt(t, f.copy, "new", "x", "--json")
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	out := decodeNew(t, res)
	if out.Path != f.wtPath("x") || out.Error == nil || out.Error.Code != "setup_failed" || !strings.Contains(out.Error.Message, "exit 7") {
		t.Fatalf("result = %+v, want the path plus a setup_failed error naming the command", out)
	}
	if _, err := os.Stat(filepath.Join(f.wtPath("x"), project.WorktreeMarkerFileName)); err != nil {
		t.Fatalf("worktree should be kept after a setup failure: %v", err)
	}

	plain := runWt(t, f.copy, "new", "y")
	if plain.code != 1 || plain.lastLine() != f.wtPath("y") {
		t.Fatalf("plain: exit %d, last line %q; want 1 and the path", plain.code, plain.lastLine())
	}
}

// A shell that reached the meta through a symlink reports the symlinked path
// in $PWD, while git lists worktrees by their resolved path.
func TestWtWorksThroughSymlinkedMeta(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	link := filepath.Join(f.root, "link")
	if err := os.Symlink(f.meta, link); err != nil {
		t.Fatal(err)
	}
	via := filepath.Join(link, "c")
	t.Setenv("PWD", via)

	if res := runWt(t, via, "path", "x"); res.code != 0 {
		t.Fatalf("path: exit %d\nstderr: %s", res.code, res.stderr)
	}
	if res := runWt(t, via, "rm", "x"); res.code != 0 {
		t.Fatalf("rm: exit %d\nstderr: %s", res.code, res.stderr)
	}
	assertRemoved(t, f, "x")
}

func TestWtPathUnknownWorktree(t *testing.T) {
	f := seedWtWorkspace(t)
	res := runWt(t, f.copy, "path", "nope")
	if res.code != 4 || res.stdout != "" {
		t.Fatalf("exit %d stdout %q, want 4 and nothing on stdout", res.code, res.stdout)
	}
}

func TestWtRmOfAMissingWorktreeIsNotFound(t *testing.T) {
	f := seedWtWorkspace(t)
	res := runWt(t, f.copy, "rm", "nope", "--json")
	if res.code != 4 || !strings.Contains(res.stdout, `"code": "not_found"`) {
		t.Fatalf("exit %d stdout %q, want 4 and a not_found JSON error", res.code, res.stdout)
	}
}

func TestWtRmRetrySkipsNamesAlreadyRemoved(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "a", "--no-setup")
	mustNew(t, f.copy, "b", "--no-setup")
	if res := runWt(t, f.copy, "rm", "a"); res.code != 0 {
		t.Fatalf("rm a: exit %d\nstderr: %s", res.code, res.stderr)
	}
	res := runWt(t, f.copy, "rm", "a", "b", "--json")
	if res.code != 0 || !strings.Contains(res.stdout, `"not_found": [`) {
		t.Fatalf("retry rm a b: exit %d stdout %q\nstderr: %s", res.code, res.stdout, res.stderr)
	}
	assertRemoved(t, f, "b")
}

func TestWtRmDryRunRemovesNothing(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	res := runWt(t, f.copy, "rm", "x", "--dry-run")
	if res.code != 0 || res.stdout != "would remove "+f.wtPath("x")+"\n" {
		t.Fatalf("dry run: exit %d stdout %q\nstderr: %s", res.code, res.stdout, res.stderr)
	}
	if _, err := os.Stat(f.wtPath("x")); err != nil {
		t.Fatalf("dry run removed the worktree: %v", err)
	}
}

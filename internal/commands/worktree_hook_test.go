package commands

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

// hookJSON encodes a Claude Code hook payload.
func hookJSON(t *testing.T, fields map[string]string) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// runHook runs `mws wt hook <event>` with the payload on stdin from a
// directory outside the workspace, so only CLAUDE_PROJECT_DIR and the
// payload can locate the copy. projectDir "" leaves CLAUDE_PROJECT_DIR unset.
func runHook(t *testing.T, f wtFixture, projectDir, event string, payload map[string]string) mwsResult {
	t.Helper()
	t.Setenv(claudeProjectDirEnv, projectDir)
	return runWtIn(t, f.root, hookJSON(t, payload), "hook", event)
}

func TestWtHookCreateResolvesCopyFromProjectDir(t *testing.T) {
	f := seedWtWorkspace(t)

	res := runHook(t, f, f.copy, "create", map[string]string{"name": "agent-a1", "cwd": f.root, "session_id": "s"})
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got := strings.TrimSpace(res.stdout); got != f.wtPath("agent-a1") {
		t.Fatalf("stdout = %q, want only the path %q", res.stdout, f.wtPath("agent-a1"))
	}
	if got := gitT(t, f.wtPath("agent-a1")+"/frontend", "branch", "--show-current"); got != "wt/agent-a1" {
		t.Fatalf("frontend branch = %q, want wt/agent-a1", got)
	}
}

// Without CLAUDE_PROJECT_DIR the payload's cwd locates the copy, and a cwd
// inside a worktree bases the new one on that worktree's checkouts.
func TestWtHookCreateFallsBackToPayloadCwd(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	xFront := filepath.Join(f.wtPath("x"), "frontend")
	commitIn(t, xFront, "x.txt")

	res := runHook(t, f, "", "create", map[string]string{"name": "y", "cwd": xFront})
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got := strings.TrimSpace(res.stdout); got != f.wtPath("y") {
		t.Fatalf("stdout = %q, want %q", res.stdout, f.wtPath("y"))
	}
	if got, want := gitT(t, filepath.Join(f.wtPath("y"), "frontend"), "rev-parse", "HEAD"), gitT(t, xFront, "rev-parse", "HEAD"); got != want {
		t.Fatalf("y/frontend based on %s, want x's HEAD %s", got, want)
	}
}

// EnterWorktree passes the meta root as cwd; CLAUDE_PROJECT_DIR wins.
func TestWtHookCreateMetaRootCwdWithCopyProjectDir(t *testing.T) {
	f := seedWtWorkspace(t)

	res := runHook(t, f, f.copy, "create", map[string]string{"name": "x", "cwd": f.meta})
	if res.code != 0 || strings.TrimSpace(res.stdout) != f.wtPath("x") {
		t.Fatalf("exit %d stdout %q\nstderr: %s", res.code, res.stdout, res.stderr)
	}
}

// A refusal (exit 3 for `wt new`) is still exit 1 for Claude, and leaves
// nothing behind.
func TestWtHookCreateFailureExits1AndLeavesNothing(t *testing.T) {
	f := seedWtWorkspace(t)
	gitT(t, f.repo("frontend"), "branch", "wt/x")
	gitT(t, f.repo("frontend"), "switch", "-q", "wt/x")

	res := runHook(t, f, f.copy, "create", map[string]string{"name": "x", "cwd": f.copy})
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	if res.stdout != "" {
		t.Fatalf("a failed create must print no path, got %q", res.stdout)
	}
	if _, err := os.Lstat(filepath.Join(f.copy, ".wt")); err == nil {
		t.Fatal("a failed create left the worktrees dir behind")
	}
	if branchExists(t, f, "backend", "wt/x") {
		t.Fatal("a failed create left backend:wt/x behind")
	}
}

func TestWtHookCreateSetupFailureStillExits0WithPath(t *testing.T) {
	f := seedWtWorkspace(t)
	f.cfg.Worktrees = &config.Worktrees{Setup: []config.SetupCommand{{Cmd: "echo setup-noise; false"}}}
	f.saveConfig(t)

	res := runHook(t, f, f.copy, "create", map[string]string{"name": "x", "cwd": f.copy})
	if res.code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", res.code, res.stderr)
	}
	if got := strings.TrimSpace(res.stdout); got != f.wtPath("x") {
		t.Fatalf("stdout = %q, want only the path", res.stdout)
	}
	if !strings.Contains(res.stderr, "[WARN]") || !strings.Contains(res.stderr, "setup") {
		t.Fatalf("stderr should warn about the failed setup:\n%s", res.stderr)
	}
	if _, err := os.Stat(filepath.Join(f.wtPath("x"), project.WorktreeMarkerFileName)); err != nil {
		t.Fatalf("worktree should be kept after a setup failure: %v", err)
	}
}

func TestWtHookRemoveRefusesDirtyWithExit1(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	wip := filepath.Join(f.wtPath("x"), "frontend", "wip.txt")
	writeFileAll(t, wip, "unsaved")

	res := runHook(t, f, f.copy, "remove", map[string]string{"worktree_path": f.wtPath("x")})
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "frontend") {
		t.Fatalf("stderr should name the dirty repo:\n%s", res.stderr)
	}
	if got := readFile(t, wip); got != "unsaved" {
		t.Fatalf("refused remove touched the work: %q", got)
	}
}

func TestWtHookRemoveRemovesCleanWorktree(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")

	res := runHook(t, f, f.copy, "remove", map[string]string{"worktree_path": f.wtPath("x")})
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	assertRemoved(t, f, "x")
}

// A path that is not an mws worktree is refused, never deleted.
func TestWtHookRemoveRefusesForeignPath(t *testing.T) {
	f := seedWtWorkspace(t)
	plain := filepath.Join(f.copy, "notes")
	writeFileAll(t, filepath.Join(plain, "a.txt"), "keep me")

	res := runHook(t, f, f.copy, "remove", map[string]string{"worktree_path": plain})
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, filepath.Join(plain, "a.txt")); got != "keep me" {
		t.Fatalf("refused remove touched %s: %q", plain, got)
	}
}

func TestWtHookSubagentStopRemovesIdleWorktree(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "agent-a1", "--no-setup")

	res := runHook(t, f, f.copy, "subagent-stop", map[string]string{"cwd": f.wtPath("agent-a1"), "agent_id": "a1"})
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	assertRemoved(t, f, "agent-a1")
	for _, folder := range []string{"backend", "frontend"} {
		if branchExists(t, f, folder, "wt/agent-a1") {
			t.Fatalf("%s kept wt/agent-a1 although it holds no unique commit", folder)
		}
	}
}

// wt rm would delete the directory and keep the branch; a finished subagent's
// committed work must stay where the user can find it.
func TestWtHookSubagentStopKeepsCommittedWork(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "agent-a1", "--no-setup")
	commitIn(t, filepath.Join(f.wtPath("agent-a1"), "frontend"), "work.txt")

	res := runHook(t, f, f.copy, "subagent-stop", map[string]string{"cwd": f.wtPath("agent-a1"), "agent_id": "a1"})
	if res.code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "frontend") || !strings.Contains(res.stderr, "1 commit not merged anywhere") {
		t.Fatalf("stderr should say why the worktree was kept:\n%s", res.stderr)
	}
	if got := readFile(t, filepath.Join(f.wtPath("agent-a1"), "frontend", "work.txt")); got != "work.txt" {
		t.Fatalf("work.txt = %q", got)
	}
	for _, folder := range []string{"backend", "frontend"} {
		if !branchExists(t, f, folder, "wt/agent-a1") {
			t.Fatalf("%s lost wt/agent-a1", folder)
		}
	}
}

func TestWtHookSubagentStopKeepsDirtyWorktree(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "agent-a1", "--no-setup")
	wip := filepath.Join(f.wtPath("agent-a1"), "backend", "wip.txt")
	writeFileAll(t, wip, "unsaved")

	res := runHook(t, f, f.copy, "subagent-stop", map[string]string{"cwd": filepath.Join(f.wtPath("agent-a1"), "backend"), "agent_id": "a1"})
	if res.code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "kept worktree agent-a1") || !strings.Contains(res.stderr, "backend") {
		t.Fatalf("stderr should say why the worktree was kept:\n%s", res.stderr)
	}
	if got := readFile(t, wip); got != "unsaved" {
		t.Fatalf("wip.txt = %q", got)
	}
}

// A subagent without isolation stops with the cwd of the session that spawned
// it, which may be a worktree of its own (`claude -w x`, EnterWorktree); only
// the stopping agent's own agent-<agent_id> worktree is removed.
func TestWtHookSubagentStopIgnoresOtherAgentsWorktrees(t *testing.T) {
	f := seedWtWorkspace(t)
	for _, name := range []string{"x", "agent-a1"} {
		mustNew(t, f.copy, name, "--no-setup")
		for _, payload := range []map[string]string{
			{"cwd": f.wtPath(name)},
			{"cwd": f.wtPath(name), "agent_id": "a2"},
		} {
			res := runHook(t, f, f.copy, "subagent-stop", payload)
			if res.code != 0 || res.stderr != "" {
				t.Fatalf("%v: exit %d stderr %q, want a silent no-op", payload, res.code, res.stderr)
			}
			if _, err := os.Stat(filepath.Join(f.wtPath(name), project.WorktreeMarkerFileName)); err != nil {
				t.Fatalf("%v removed worktree %s: %v", payload, name, err)
			}
		}
	}
}

// Anything that is not a managed worktree is left alone, silently, and a
// payload mws cannot read still exits 0.
func TestWtHookSubagentStopIgnoresNonWorktrees(t *testing.T) {
	f := seedWtWorkspace(t)
	writeFileAll(t, filepath.Join(f.wtPath("plain"), project.WorktreeMarkerFileName), "name = \"plain\"\n")

	for _, cwd := range []string{f.copy, f.repo("frontend"), f.meta, f.root, f.wtPath("plain")} {
		res := runHook(t, f, f.copy, "subagent-stop", map[string]string{"cwd": cwd})
		if res.code != 0 || res.stdout != "" || res.stderr != "" {
			t.Fatalf("cwd %s: exit %d stdout %q stderr %q, want a silent no-op", cwd, res.code, res.stdout, res.stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(f.wtPath("plain"), project.WorktreeMarkerFileName)); err != nil {
		t.Fatalf("unregistered marked directory was touched: %v", err)
	}

	t.Setenv(claudeProjectDirEnv, f.copy)
	if res := runWtIn(t, f.root, "not json", "hook", "subagent-stop"); res.code != 0 {
		t.Fatalf("unreadable payload: exit %d, want 0\nstderr: %s", res.code, res.stderr)
	}
}

// Claude runs the hook from a shell whose cwd is the subagent's worktree;
// that shell must not count as a live user of it.
func TestWtHookSubagentStopIgnoresItsOwnProcessTree(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process cwds are read from /proc")
	}
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "agent-a1", "--no-setup")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	// The trailing command keeps sh alive (no exec) while mws runs.
	sh := exec.Command("sh", "-c", `"$0" wt hook subagent-stop; exit $?`, self)
	sh.Dir = f.wtPath("agent-a1")
	sh.Env = append(os.Environ(), cliEnv+"=1", claudeProjectDirEnv+"="+f.copy)
	sh.Stdin = strings.NewReader(hookJSON(t, map[string]string{"cwd": f.wtPath("agent-a1"), "agent_id": "a1"}))
	out, err := sh.CombinedOutput()
	if err != nil {
		t.Fatalf("subagent-stop from inside the worktree: %v\n%s", err, out)
	}
	assertRemoved(t, f, "agent-a1")
}

func TestWtHookHelpIsHiddenButDocumented(t *testing.T) {
	dir := t.TempDir()
	if res := runWt(t, dir, "--help"); strings.Contains(res.stdout, "hook") {
		t.Fatalf("wt --help lists the hidden hook command:\n%s", res.stdout)
	}
	res := runWt(t, dir, "hook", "--help")
	for _, want := range []string{"create", "remove", "subagent-stop", claudeProjectDirEnv} {
		if !strings.Contains(res.stdout, want) {
			t.Fatalf("wt hook --help does not mention %s:\n%s", want, res.stdout)
		}
	}
}

func TestWtHookCreateReusesName(t *testing.T) {
	f := seedWtWorkspace(t)
	payload := map[string]string{"name": "x", "cwd": f.copy}
	first := runHook(t, f, f.copy, "create", payload)
	second := runHook(t, f, f.copy, "create", payload)
	if first.code != 0 || second.code != 0 {
		t.Fatalf("exits %d, %d\nstderr: %s%s", first.code, second.code, first.stderr, second.stderr)
	}
	if first.stdout != second.stdout || strings.TrimSpace(second.stdout) != f.wtPath("x") {
		t.Fatalf("stdout %q then %q, want the same path twice", first.stdout, second.stdout)
	}
}

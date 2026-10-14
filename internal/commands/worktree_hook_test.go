package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
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
func runHook(t *testing.T, f wtFixture, projectDir, event string, payload map[string]string) wtResult {
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

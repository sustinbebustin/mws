package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var hookBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "git-repo-reminder-test")
	if err != nil {
		panic(err)
	}
	hookBin = filepath.Join(dir, "hook")
	if out, err := exec.Command("go", "build", "-o", hookBin, ".").CombinedOutput(); err != nil {
		panic("build hook: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// gitMarker is how a directory's `.git` entry is laid out.
type gitMarker int

const (
	gitNone gitMarker = iota // no .git entry: a working-copy root
	gitFile                  // .git is a regular file: a linked worktree root
	gitDir                   // .git is a directory: a native checkout
)

// workspace lays out a directory holding two native child repos, with its own
// `.git` entry shaped by marker.
func workspace(t *testing.T, marker gitMarker) string {
	t.Helper()
	root := t.TempDir()
	for _, repo := range []string{"api", "web"} {
		mkdir(t, filepath.Join(root, repo, ".git"))
	}
	switch marker {
	case gitFile:
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /elsewhere/.git/worktrees/x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	case gitDir:
		mkdir(t, filepath.Join(root, ".git"))
	}
	return root
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// runHook feeds a Bash tool call to the hook and returns its denial reason, or
// "" when it allowed the call.
func runHook(t *testing.T, projectDir, cwd, command string) string {
	t.Helper()
	in, err := json.Marshal(map[string]any{
		"cwd":        cwd,
		"tool_input": map[string]string{"command": command},
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(hookBin)
	cmd.Stdin = bytes.NewReader(in)
	cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+projectDir)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hook exited with error: %v", err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return ""
	}
	var decision struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &decision); err != nil {
		t.Fatalf("hook output is not JSON: %q", out)
	}
	if decision.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("permissionDecision = %q, want deny", decision.HookSpecificOutput.PermissionDecision)
	}
	return decision.HookSpecificOutput.PermissionDecisionReason
}

func denied(t *testing.T, projectDir, cwd, command string) bool {
	t.Helper()
	return runHook(t, projectDir, cwd, command) != ""
}

func TestCopyRoot(t *testing.T) {
	root := workspace(t, gitNone)
	cases := []struct {
		name    string
		cwd     string
		command string
		denied  bool
	}{
		{"bare git at the copy root is denied", root, "git status", true},
		{"git by absolute path at the copy root is denied", root, "/usr/bin/git status", true},
		{"git chained after another command is denied", root, "ls && git log", true},
		{"git inside a native repo is allowed", filepath.Join(root, "web"), "git status", false},
		{"git -C a child repo is allowed", root, "git -C web status", false},
		{"global git config is allowed", root, "git config --global user.name x", false},
		{"non-git commands are allowed", root, "ls -la", false},
		{"git after cd into a child repo is allowed", root, "cd web && git diff", false},
		{"git after cd ; into a child repo is allowed", root, "cd web; git status", false},
		{"git after cd in a subshell is allowed", root, "(cd web && git log | head)", false},
		{"git after cd to an absolute child repo is allowed", root, "cd " + filepath.Join(root, "api") + " && git status", false},
		{"git after cd to a dir outside any copy is allowed", root, "cd " + t.TempDir() + " && git status", false},
		{"git after cd to a non-literal dir is allowed", root, `cd "$REPO" && git status`, false},
		{"git after cd back to the copy root is denied", root, "cd web && cd .. && git status", true},
		{"git after cd to the copy root from a repo is denied", filepath.Join(root, "web"), "cd " + root + " && git status", true},
		{"cd inside a subshell does not leak out", root, "(cd web) && git status", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := denied(t, root, tc.cwd, tc.command); got != tc.denied {
				t.Errorf("denied = %v, want %v", got, tc.denied)
			}
		})
	}
}

func TestReminderNamesChildRepos(t *testing.T) {
	root := workspace(t, gitNone)
	reason := runHook(t, root, root, "git status")
	for _, want := range []string{"`" + filepath.Base(root) + "`", "`api`", "`web`", "git -C"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason %q does not mention %s", reason, want)
		}
	}
}

func TestWorktreeRoot(t *testing.T) {
	copyRoot := workspace(t, gitNone)
	worktree := workspace(t, gitFile)

	t.Run("entered worktree, project dir stays at the copy, is denied", func(t *testing.T) {
		if !denied(t, copyRoot, worktree, "git status") {
			t.Error("bare git at a worktree root was allowed")
		}
	})
	t.Run("worktree session, project dir is the worktree, is denied", func(t *testing.T) {
		if !denied(t, worktree, worktree, "git status") {
			t.Error("bare git at a worktree root was allowed")
		}
	})
	t.Run("git -C a child repo is allowed", func(t *testing.T) {
		if denied(t, copyRoot, worktree, "git -C web status") {
			t.Error("git -C at a worktree root was denied")
		}
	})
}

func TestWorktreeAdd(t *testing.T) {
	plain := t.TempDir()
	cases := []struct {
		name    string
		command string
		denied  bool
	}{
		{"bare worktree add is denied", "git worktree add ../x", true},
		{"git -C <dir> worktree add is denied", "git -C web worktree add ../x", true},
		{"-c option before worktree is denied", "git -c core.x=y worktree add -b b ../x", true},
		{"--git-dir= option before worktree is denied", "git --git-dir=.git --no-pager worktree add ../x", true},
		{"absolute git path is denied", "/usr/bin/git worktree add ../x", true},
		{"after && is denied", "ls && git worktree add ../x", true},
		{"after ; is denied", "true; git worktree add ../x", true},
		{"inside a subshell is denied", "(cd web && git worktree add ../x)", true},
		{"inside a block is denied", "{ git worktree add ../x; }", true},
		{"inside an if is denied", "if true; then git worktree add ../x; fi", true},
		{"in a pipeline is denied", "git worktree add ../x | cat", true},
		{"worktree list is allowed", "git worktree list --porcelain", false},
		{"worktree remove is allowed", "git worktree remove ../x", false},
		{"worktree prune is allowed", "git -C web worktree prune", false},
		{"worktree repair is allowed", "git worktree repair", false},
		{"-C value named worktree is allowed", "git -C worktree add file.txt", false},
		{"mws wt new is allowed", "mws wt new feature-x", false},
		{"echoing the words is allowed", "echo git worktree add", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := denied(t, plain, plain, tc.command); got != tc.denied {
				t.Errorf("denied = %v, want %v", got, tc.denied)
			}
		})
	}

	t.Run("git -C a child repo at a copy root is denied", func(t *testing.T) {
		root := workspace(t, gitNone)
		if !denied(t, root, root, "git -C web worktree add ../x") {
			t.Error("worktree add via -C at a copy root was allowed")
		}
	})
	t.Run("inside a native checkout is denied", func(t *testing.T) {
		checkout := workspace(t, gitDir)
		if !denied(t, checkout, checkout, "git worktree add ../x") {
			t.Error("worktree add in a native checkout was allowed")
		}
	})
}

func TestNativeCheckoutIsNeverDenied(t *testing.T) {
	checkout := workspace(t, gitDir)
	for _, projectDir := range []string{checkout, t.TempDir()} {
		if denied(t, projectDir, checkout, "git status") {
			t.Errorf("bare git in a native checkout with child repos was denied (project dir %s)", projectDir)
		}
	}
}

package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type hookInput struct {
	Cwd       string `json:"cwd"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

type hookOutput struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(0)
	}
	var in hookInput
	if err := json.Unmarshal(raw, &in); err != nil || in.ToolInput.Command == "" {
		os.Exit(0)
	}

	file, err := syntax.NewParser().Parse(strings.NewReader(in.ToolInput.Command), "")
	if err != nil {
		os.Exit(0)
	}

	if hasWorktreeAdd(file) {
		deny(worktreeAddReason)
	}

	if dir, found := gitFromRootViolation(file, in.Cwd); found {
		deny(reminder(dir))
	}
	os.Exit(0)
}

// reminder builds the denial message from the directory git would have run in,
// naming the native repos actually present as its children so the message
// stays correct as repos are added.
func reminder(dir string) string {
	repos := childRepos(dir)
	example := "<repo>"
	if len(repos) > 0 {
		example = repos[0]
	}

	var b strings.Builder
	b.WriteString("git ran in `")
	b.WriteString(filepath.Base(dir))
	b.WriteString("`, which is an mws working copy, not a git repository -- its native repos live in subdirectories")
	if len(repos) > 0 {
		b.WriteString(" (`")
		b.WriteString(strings.Join(repos, "`, `"))
		b.WriteString("`)")
	}
	b.WriteString(". Run git against one with `git -C <dir> <cmd>` (e.g. `git -C ")
	b.WriteString(example)
	b.WriteString(" status`). Keep `<dir>` relative; don't expand it to an absolute path.")
	return b.String()
}

// holdsReposWithoutCheckout reports whether dir holds native child repos while
// not being a native checkout itself: a working-copy root (no .git) or a linked
// worktree root (.git is a file). Keying on the directory rather than
// CLAUDE_PROJECT_DIR covers entered and subagent worktrees, where the project
// dir stays at the copy.
func holdsReposWithoutCheckout(dir string) bool {
	if info, err := os.Lstat(filepath.Join(dir, ".git")); err == nil && !info.Mode().IsRegular() {
		return false
	}
	return len(childRepos(dir)) > 0
}

// childRepos returns the names of dir's immediate subdirectories that are git
// repositories (hold a .git entry), in directory read order.
func childRepos(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var repos []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), ".git")); err == nil {
			repos = append(repos, e.Name())
		}
	}
	return repos
}

const worktreeAddReason = "`git worktree add` is not used in this workspace: worktrees are made by mws so every repo, " +
	"the harness, ports and env come along. Create one with `mws wt new <name>` " +
	"or spawn an Agent with `isolation: \"worktree\"`. Other `git worktree` subcommands (list, remove, prune, repair) stay allowed."

// hasWorktreeAdd reports whether any git invocation anywhere in the command,
// including inside subshells, blocks, pipelines and conditionals, runs
// `git worktree add`. It applies in every directory: mws creates worktrees by
// running git as its own subprocess, which this hook never sees.
func hasWorktreeAdd(file *syntax.File) bool {
	found := false
	syntax.Walk(file, func(node syntax.Node) bool {
		if call, ok := node.(*syntax.CallExpr); ok && isWorktreeAdd(call) {
			found = true
		}
		return !found
	})
	return found
}

// gitOptionsWithValue are git's global options that take their value as the
// next argument, so that argument is not mistaken for the subcommand.
var gitOptionsWithValue = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
	"--namespace": true, "--config-env": true, "--super-prefix": true,
}

func isWorktreeAdd(c *syntax.CallExpr) bool {
	if !isGit(c) {
		return false
	}
	var rest []string
	for i := 1; i < len(c.Args); i++ {
		arg := wordLit(c.Args[i])
		if len(rest) == 0 && strings.HasPrefix(arg, "-") {
			if gitOptionsWithValue[arg] {
				i++
			}
			continue
		}
		rest = append(rest, arg)
		if len(rest) == 2 {
			break
		}
	}
	return len(rest) == 2 && rest[0] == "worktree" && rest[1] == "add"
}

// gitFromRootViolation reports the directory of the first git call that would
// run in a directory holding repos without being a checkout. It follows `cd`
// through the command so `cd web && git diff` is judged in `web`, not in the
// Bash tool's cwd. A dir of "" means a `cd` target the hook can't resolve
// statically; git there is allowed and fails on its own if wrong.
func gitFromRootViolation(file *syntax.File, cwd string) (string, bool) {
	dir, found, _ := stmtsGitViolation(file.Stmts, cwd)
	return dir, found
}

// stmtsGitViolation walks statements in order, carrying the working directory
// each `cd` leaves behind. It returns the violating dir and whether one was
// found, then the directory the statements end in.
func stmtsGitViolation(stmts []*syntax.Stmt, dir string) (string, bool, string) {
	for _, s := range stmts {
		var found bool
		var at string
		at, found, dir = cmdGitViolation(s.Cmd, dir)
		if found {
			return at, true, dir
		}
	}
	return "", false, dir
}

func cmdGitViolation(cmd syntax.Command, dir string) (string, bool, string) {
	switch c := cmd.(type) {
	case *syntax.CallExpr:
		if target, ok := cdTarget(c, dir); ok {
			return "", false, target
		}
		if dir != "" && checkGitCall(c) && holdsReposWithoutCheckout(dir) {
			return dir, true, dir
		}
	case *syntax.BinaryCmd:
		if c.Op == syntax.Pipe || c.Op == syntax.PipeAll {
			// Each side of a pipeline runs in its own subshell.
			if at, found, _ := cmdGitViolation(c.X.Cmd, dir); found {
				return at, true, dir
			}
			at, found, _ := cmdGitViolation(c.Y.Cmd, dir)
			return at, found, dir
		}
		at, found, next := cmdGitViolation(c.X.Cmd, dir)
		if found {
			return at, true, next
		}
		return cmdGitViolation(c.Y.Cmd, next)
	case *syntax.Subshell:
		at, found, _ := stmtsGitViolation(c.Stmts, dir)
		return at, found, dir
	case *syntax.Block:
		return stmtsGitViolation(c.Stmts, dir)
	}
	return "", false, dir
}

// cdTarget reports whether c is a `cd` and, if so, the absolute directory it
// moves to, or "" when the target isn't a plain literal path (variables, `~`,
// `-`, no argument).
func cdTarget(c *syntax.CallExpr, dir string) (string, bool) {
	if len(c.Args) == 0 || wordLit(c.Args[0]) != "cd" {
		return "", false
	}
	for _, w := range c.Args[1:] {
		arg, literal := fullLit(w)
		if literal && strings.HasPrefix(arg, "-") && arg != "-" {
			continue
		}
		switch {
		case !literal || arg == "-" || strings.HasPrefix(arg, "~"):
			return "", true
		case filepath.IsAbs(arg):
			return filepath.Clean(arg), true
		case dir == "":
			return "", true
		default:
			return filepath.Join(dir, arg), true
		}
	}
	return "", true
}

// fullLit returns w's text and whether w is made only of literal parts, so an
// expansion such as `"$REPO"` is never mistaken for a path.
func fullLit(w *syntax.Word) (string, bool) {
	var sb strings.Builder
	for _, p := range w.Parts {
		switch part := p.(type) {
		case *syntax.Lit:
			sb.WriteString(part.Value)
		case *syntax.SglQuoted:
			sb.WriteString(part.Value)
		default:
			return "", false
		}
	}
	return sb.String(), true
}

// isGit reports whether c invokes git, by bare name or by path.
func isGit(c *syntax.CallExpr) bool {
	return len(c.Args) > 0 && filepath.Base(wordLit(c.Args[0])) == "git"
}

func checkGitCall(c *syntax.CallExpr) bool {
	if !isGit(c) {
		return false
	}
	for i := 1; i < len(c.Args); i++ {
		arg := wordLit(c.Args[i])
		switch {
		case arg == "-C":
			return false
		case strings.HasPrefix(arg, "--git-dir") || strings.HasPrefix(arg, "--work-tree"):
			return false
		case arg == "--version" || arg == "--help" || arg == "-h" || arg == "--exec-path":
			return false
		case arg == "config":
			for j := i + 1; j < len(c.Args); j++ {
				flag := wordLit(c.Args[j])
				if flag == "--global" || flag == "--system" {
					return false
				}
			}
			return true
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			return true
		}
	}
	return false
}

func wordLit(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range w.Parts {
		if lit, ok := p.(*syntax.Lit); ok {
			sb.WriteString(lit.Value)
		}
	}
	return sb.String()
}

func deny(reason string) {
	out := hookOutput{}
	out.HookSpecificOutput.HookEventName = "PreToolUse"
	out.HookSpecificOutput.PermissionDecision = "deny"
	out.HookSpecificOutput.PermissionDecisionReason = reason
	_ = json.NewEncoder(os.Stdout).Encode(out)
	os.Exit(0)
}

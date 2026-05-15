package main

import (
	"encoding/json"
	"io"
	"os"
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

const reminder = "Git command run from the working-copy root, but the root is NOT a git repository -- it is an mws working copy. Native git repos live in subdirectories. Use `git -C <repo> <cmd>` to operate on a specific repo without changing directory."

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(0)
	}
	var in hookInput
	if err := json.Unmarshal(raw, &in); err != nil || in.ToolInput.Command == "" {
		os.Exit(0)
	}

	projectRoot := os.Getenv("CLAUDE_PROJECT_DIR")
	if projectRoot == "" || in.Cwd != projectRoot {
		os.Exit(0)
	}

	file, err := syntax.NewParser().Parse(strings.NewReader(in.ToolInput.Command), "")
	if err != nil {
		os.Exit(0)
	}

	if hasGitFromRootViolation(file) {
		deny(reminder)
	}
	os.Exit(0)
}

func hasGitFromRootViolation(file *syntax.File) bool {
	for _, stmt := range file.Stmts {
		if walkForGitViolation(stmt.Cmd) {
			return true
		}
	}
	return false
}

func walkForGitViolation(cmd syntax.Command) bool {
	switch c := cmd.(type) {
	case *syntax.CallExpr:
		return checkGitCall(c)
	case *syntax.BinaryCmd:
		return walkForGitViolation(c.X.Cmd) || walkForGitViolation(c.Y.Cmd)
	case *syntax.Subshell:
		for _, s := range c.Stmts {
			if walkForGitViolation(s.Cmd) {
				return true
			}
		}
	case *syntax.Block:
		for _, s := range c.Stmts {
			if walkForGitViolation(s.Cmd) {
				return true
			}
		}
	}
	return false
}

func checkGitCall(c *syntax.CallExpr) bool {
	if len(c.Args) == 0 || wordLit(c.Args[0]) != "git" {
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
	json.NewEncoder(os.Stdout).Encode(out)
	os.Exit(0)
}

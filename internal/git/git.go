// Package git wraps the few git plumbing operations mws needs.
//
// Each operation is implemented as a thin shell-out to /usr/bin/git so the user's
// SSH config, credential helpers, and aliases are honoured naturally.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// InitQuiet runs `git init -q` in dir, suppressing the normal banner.
func InitQuiet(ctx context.Context, dir string) error {
	return run(ctx, dir, "init", "-q")
}

// Clone runs `git clone <src> <dst>`, streaming stdout/stderr through to the caller's.
func Clone(ctx context.Context, src, dst string) error {
	return runStreaming(ctx, "", "clone", src, dst)
}

// CurrentBranch returns the abbreviated HEAD ref for repoDir.
func CurrentBranch(ctx context.Context, repoDir string) (string, error) {
	out, err := capture(ctx, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// DefaultBranch resolves the default branch of the given remote (or "origin" if empty)
// inside repoDir. Returns the short ref name (e.g., "main"), or an error.
func DefaultBranch(ctx context.Context, repoDir, remote string) (string, error) {
	if remote == "" {
		remote = "origin"
	}
	out, err := capture(ctx, repoDir, "symbolic-ref", "--short", "refs/remotes/"+remote+"/HEAD")
	if err == nil {
		// e.g. "origin/main" -> "main"
		s := strings.TrimSpace(out)
		if i := strings.Index(s, "/"); i >= 0 {
			return s[i+1:], nil
		}
		return s, nil
	}
	// Fall back to `git remote show` which queries the remote directly.
	out, err = capture(ctx, repoDir, "remote", "show", remote)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "HEAD branch:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "HEAD branch:")), nil
		}
	}
	return "", fmt.Errorf("could not determine default branch for remote %q", remote)
}

// Checkout runs `git checkout <ref>` in repoDir.
func Checkout(ctx context.Context, repoDir, ref string) error {
	return run(ctx, repoDir, "checkout", ref)
}

// run executes `git <args...>` in cwd, capturing combined output into the error on failure.
func run(ctx context.Context, cwd string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(buf.String()))
	}
	return nil
}

// runStreaming executes `git <args...>` with stdout/stderr inherited so the user sees progress live.
func runStreaming(ctx context.Context, cwd string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// capture executes `git <args...>` in cwd and returns stdout.
func capture(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

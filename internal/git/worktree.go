package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Worktree is one entry of `git worktree list --porcelain`.
type Worktree struct {
	Path string
	// Branch is the short name of the checked-out branch, or "" when detached.
	Branch string
}

// Worktrees lists every worktree registered with the repository at repoDir,
// the main checkout included.
func Worktrees(ctx context.Context, repoDir string) ([]Worktree, error) {
	out, err := capture(ctx, repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []Worktree
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			list = append(list, Worktree{Path: strings.TrimPrefix(line, "worktree ")})
		case strings.HasPrefix(line, "branch ") && len(list) > 0:
			list[len(list)-1].Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	return list, nil
}

// AddDetachedNoCheckout registers path as a detached linked worktree of
// repoDir's HEAD without checking out any files.
func AddDetachedNoCheckout(ctx context.Context, repoDir, path string) error {
	return run(ctx, repoDir, "worktree", "add", "--quiet", "--no-checkout", "--detach", path)
}

// SparseCheckoutOnly restricts the checkout at dir to the given non-cone
// patterns and populates the working tree from HEAD.
func SparseCheckoutOnly(ctx context.Context, dir string, patterns ...string) error {
	if err := run(ctx, dir, append([]string{"sparse-checkout", "set", "--no-cone"}, patterns...)...); err != nil {
		return err
	}
	return run(ctx, dir, "reset", "--quiet", "--hard")
}

// AddWorktree checks out an existing branch at path as a linked worktree of repoDir.
func AddWorktree(ctx context.Context, repoDir, path, branch string) error {
	return run(ctx, repoDir, "worktree", "add", "--quiet", path, branch)
}

// AddWorktreeNewBranch creates branch at base and checks it out at path as a
// linked worktree of repoDir.
func AddWorktreeNewBranch(ctx context.Context, repoDir, path, branch, base string) error {
	return run(ctx, repoDir, "worktree", "add", "--quiet", "-b", branch, path, base)
}

// RemoveWorktreeForce unregisters the linked worktree at path and deletes its
// files, discarding any local changes, then prunes stale registrations.
func RemoveWorktreeForce(ctx context.Context, repoDir, path string) error {
	removeErr := run(ctx, repoDir, "worktree", "remove", "--force", "--force", path)
	return errors.Join(removeErr, run(ctx, repoDir, "worktree", "prune"))
}

// RemoveWorktree unregisters the linked worktree at path and deletes its
// files. git refuses when the worktree has local changes or is locked.
func RemoveWorktree(ctx context.Context, repoDir, path string) error {
	return run(ctx, repoDir, "worktree", "remove", path)
}

// PruneWorktrees drops registrations of linked worktrees whose directories
// are gone.
func PruneWorktrees(ctx context.Context, repoDir string) error {
	return run(ctx, repoDir, "worktree", "prune")
}

// RepairWorktree reconnects repoDir's registration of the linked worktree at
// path after either side moved.
func RepairWorktree(ctx context.Context, repoDir, path string) error {
	return run(ctx, repoDir, "worktree", "repair", path)
}

// IsDirty reports whether the checkout at dir has modified, staged or
// untracked (not ignored) files.
func IsDirty(ctx context.Context, dir string) (bool, error) {
	out, err := capture(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// UniqueCommits counts the commits on branch that no other local branch,
// tag or remote-tracking branch reaches: the work lost if branch is deleted.
// Worktree HEADs are deliberately not counted as other refs.
func UniqueCommits(ctx context.Context, repoDir, branch string) (int, error) {
	out, err := capture(ctx, repoDir, "rev-list", "--count", "refs/heads/"+branch,
		"--not", "--exclude="+branch, "--branches", "--tags", "--remotes")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("git rev-list --count printed %q: %w", strings.TrimSpace(out), err)
	}
	return n, nil
}

// DeleteBranch force-deletes a local branch.
// DetachedUniqueCommits counts the commits on the detached HEAD of the
// worktree at dir that no local branch, tag or remote-tracking branch
// reaches: the work lost if that worktree is removed.
func DetachedUniqueCommits(ctx context.Context, dir string) (int, error) {
	out, err := capture(ctx, dir, "rev-list", "--count", "HEAD", "--not", "--branches", "--tags", "--remotes")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("git rev-list --count printed %q: %w", strings.TrimSpace(out), err)
	}
	return n, nil
}

func DeleteBranch(ctx context.Context, repoDir, branch string) error {
	return run(ctx, repoDir, "branch", "--quiet", "-D", branch)
}

// BranchExists reports whether refs/heads/<branch> exists in repoDir.
func BranchExists(ctx context.Context, repoDir, branch string) bool {
	return run(ctx, repoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch) == nil
}

// ResolveCommit resolves ref to a full commit id in repoDir.
func ResolveCommit(ctx context.Context, repoDir, ref string) (string, error) {
	out, err := capture(ctx, repoDir, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%q is not a commit in %s", ref, repoDir)
	}
	return strings.TrimSpace(out), nil
}

// CheckBranchName reports whether name is a valid branch name.
func CheckBranchName(ctx context.Context, name string) error {
	if err := run(ctx, "", "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%q is not a valid git branch name", name)
	}
	return nil
}

// OperationInProgress names the multi-step operation (merge, rebase,
// cherry-pick, revert) the checkout at dir is in the middle of, or "" if none.
func OperationInProgress(ctx context.Context, dir string) (string, error) {
	for _, op := range []struct{ name, path string }{
		{"merge", "MERGE_HEAD"},
		{"rebase", "rebase-merge"},
		{"rebase", "rebase-apply"},
		{"cherry-pick", "CHERRY_PICK_HEAD"},
		{"revert", "REVERT_HEAD"},
	} {
		out, err := capture(ctx, dir, "rev-parse", "--git-path", op.path)
		if err != nil {
			return "", err
		}
		p := strings.TrimSpace(out)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if _, err := os.Stat(p); err == nil {
			return op.name, nil
		}
	}
	return "", nil
}

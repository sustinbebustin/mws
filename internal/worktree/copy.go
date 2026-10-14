package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sustinbebustin/mws/internal/config"
)

// Roots returns the marked worktree roots of the working copy at copyPath,
// sorted by path. Unmarked directories in the worktrees dir are not
// worktrees and are left out.
func Roots(copyPath string, policy config.WorktreePolicy) ([]string, error) {
	wtDir := filepath.Join(copyPath, policy.Dir)
	dirs, err := os.ReadDir(wtDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, d := range dirs {
		root := filepath.Join(wtDir, d.Name())
		if d.IsDir() && hasMarker(root) {
			roots = append(roots, root)
		}
	}
	return roots, nil
}

// RepairCopy reconnects every worktree of the working copy at copyPath with
// the repos that own it, after the copy moved (trashed, restored, renamed):
// the anchor with the meta repo and each inner worktree with the copy's
// native repo. It returns the repaired roots; a copy without worktrees runs
// no git at all.
func RepairCopy(ctx context.Context, metaRoot, copyPath string, policy config.WorktreePolicy) ([]string, error) {
	roots, err := Roots(copyPath, policy)
	if err != nil {
		return nil, err
	}
	for i, root := range roots {
		if err := repair(ctx, metaRoot, copyPath, root); err != nil {
			return roots[:i], fmt.Errorf("repair worktree %s: %w", root, err)
		}
	}
	return roots, nil
}

package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
)

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

// repair reconnects the anchor and every inner worktree of root with the
// repos that own them.
func repair(ctx context.Context, metaRoot, copyPath, root string) error {
	if err := git.RepairWorktree(ctx, metaRoot, root); err != nil {
		return err
	}
	folders, err := nativeRepos(root)
	if err != nil {
		return err
	}
	for _, folder := range folders {
		if err := git.RepairWorktree(ctx, filepath.Join(copyPath, folder), filepath.Join(root, folder)); err != nil {
			return fmt.Errorf("%s: %w", folder, err)
		}
	}
	return nil
}

// ForgetVanished drops the meta repo's registrations of worktrees whose
// directories are gone, such as those of deleted or purged copies. Copies in
// the trash keep theirs, since RepairCopy points them into the trash. A meta
// workspace that is not a git repository has no worktrees, so nothing runs.
func ForgetVanished(ctx context.Context, metaRoot string) error {
	if _, err := os.Lstat(filepath.Join(metaRoot, ".git")); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return git.PruneWorktrees(ctx, metaRoot)
}

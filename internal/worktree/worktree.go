// Package worktree creates and resolves worktrees: children of a working copy
// at <copy>/<worktrees dir>/<name>/ holding one git worktree per native repo,
// rooted in a detached, sparse linked worktree of the meta workspace repo.
package worktree

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
)

// Reporter receives progress lines while a worktree is built.
type Reporter interface {
	OK(string)
	Warn(string)
}

// Path returns where the worktree called name of copyPath lives.
func Path(copyPath string, policy config.WorktreePolicy, name string) string {
	return filepath.Join(copyPath, policy.Dir, name)
}

// IsManaged reports whether root is both marked and registered as a linked
// worktree of the meta repo -- the only shape mws treats as its own.
func IsManaged(ctx context.Context, metaRoot, root string) (bool, error) {
	if !project.IsWorktreeRoot(root) {
		return false, nil
	}
	list, err := git.Worktrees(ctx, metaRoot)
	if err != nil {
		return false, err
	}
	for _, w := range list {
		if w.Path == root {
			return true, nil
		}
	}
	return false, nil
}

// nativeRepos lists the direct child directories of dir holding a .git entry.
func nativeRepos(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Lstat(filepath.Join(dir, e.Name(), ".git")); err == nil {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

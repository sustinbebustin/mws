package worktree

import (
	"errors"
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

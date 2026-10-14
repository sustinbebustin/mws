package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
)

// scannedDir is one directory in a copy's worktrees dir. Only one both
// marked and registered is a worktree mws manages.
type scannedDir struct {
	name       string
	root       string
	marked     bool
	registered bool
}

// copyScan is what a copy's worktrees dir holds, matched against the meta
// repo's worktree registrations.
type copyScan struct {
	dirs []scannedDir // sorted by name
	// orphans are registrations in the worktrees dir whose directory is
	// gone, sorted.
	orphans []string
}

// scanCopy reads the worktrees dir of the copy at copyPath. With metaWts nil
// it runs no git and reports nothing as registered.
func scanCopy(copyPath string, policy config.WorktreePolicy, metaWts []git.Worktree) (copyScan, error) {
	wtDir := filepath.Join(copyPath, policy.Dir)
	registered := map[string]bool{}
	var s copyScan
	for _, w := range metaWts {
		if filepath.Dir(w.Path) != wtDir {
			continue
		}
		registered[w.Path] = true
		if _, err := os.Lstat(w.Path); errors.Is(err, os.ErrNotExist) {
			s.orphans = append(s.orphans, w.Path)
		}
	}
	sort.Strings(s.orphans)
	entries, err := os.ReadDir(wtDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return copyScan{}, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		root := filepath.Join(wtDir, e.Name())
		s.dirs = append(s.dirs, scannedDir{name: e.Name(), root: root, marked: project.IsWorktreeRoot(root), registered: registered[root]})
	}
	return s, nil
}

// Roots returns the marked worktree roots of the working copy at copyPath,
// sorted by path. Unmarked directories in the worktrees dir are not
// worktrees and are left out. It runs no git.
func Roots(copyPath string, policy config.WorktreePolicy) ([]string, error) {
	s, err := scanCopy(copyPath, policy, nil)
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, d := range s.dirs {
		if d.marked {
			roots = append(roots, d.root)
		}
	}
	return roots, nil
}

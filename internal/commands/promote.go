package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/project"
)

func newPromoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "promote <path>",
		Short: "Move a file or directory from a working copy into the meta workspace",
		Long: `promote moves a file or directory that lives only in the current working
copy into the sibling meta workspace, replaces the original with a symlink, and
backfills the symlink into every peer working copy so the item is now shared.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPromote(newConsoleReporter(), args[0])
		},
	}
}

func runPromote(r Reporter, arg string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ws, err := project.Locate(cwd)
	if err != nil {
		return err
	}
	if ws.WorkingCopy == "" {
		return errors.New("promote must be run from a working copy, not from the meta")
	}

	abs, err := filepath.Abs(arg)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(ws.WorkingCopy, abs)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || hasParentSegment(rel) {
		return fmt.Errorf("%s is outside the working copy %s", abs, ws.WorkingCopy)
	}

	st, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("lstat %s: %w", abs, err)
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is already a symlink; nothing to promote", abs)
	}

	// Only top-level entries can be promoted -- the symlink discovery model only links top-level items.
	if filepath.Dir(rel) != "." {
		return fmt.Errorf("promote only handles top-level entries; got nested path %q", rel)
	}

	dst := filepath.Join(ws.MetaRoot, rel)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("meta already has %s; resolve manually", rel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Rename(abs, dst); err != nil {
		return fmt.Errorf("move %s -> %s: %w", abs, dst, err)
	}
	relTarget, err := filepath.Rel(ws.WorkingCopy, dst)
	if err != nil {
		return err
	}
	if err := os.Symlink(relTarget, abs); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", abs, relTarget, err)
	}
	r.OK(fmt.Sprintf("Promoted %s into %s", rel, filepath.Base(ws.MetaRoot)))

	// Backfill into peers.
	peers, err := project.EnumeratePeers(ws.MetaRoot)
	if err != nil {
		return err
	}
	for _, peer := range peers {
		if peer == ws.WorkingCopy {
			continue
		}
		linkPath := filepath.Join(peer, rel)
		if st, err := os.Lstat(linkPath); err == nil {
			if st.Mode()&os.ModeSymlink == 0 {
				r.Warn(fmt.Sprintf("%s: %s exists and is not a symlink; skipping", filepath.Base(peer), rel))
				continue
			}
			if err := os.Remove(linkPath); err != nil {
				r.Fail(fmt.Sprintf("%s: %v", filepath.Base(peer), err))
				continue
			}
		}
		target, err := filepath.Rel(peer, dst)
		if err != nil {
			r.Fail(fmt.Sprintf("%s: %v", filepath.Base(peer), err))
			continue
		}
		if err := os.Symlink(target, linkPath); err != nil {
			r.Fail(fmt.Sprintf("%s: %v", filepath.Base(peer), err))
			continue
		}
		r.OK(fmt.Sprintf("%s: linked %s", filepath.Base(peer), rel))
	}
	return nil
}

func hasParentSegment(rel string) bool {
	for _, seg := range splitPath(rel) {
		if seg == ".." {
			return true
		}
	}
	return false
}

func splitPath(p string) []string {
	var out []string
	for {
		dir, file := filepath.Split(p)
		if file != "" {
			out = append([]string{file}, out...)
		}
		if dir == "" || dir == string(filepath.Separator) {
			break
		}
		p = filepath.Clean(dir)
		if p == "." || p == string(filepath.Separator) {
			break
		}
	}
	return out
}

package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/project"
)

func newPromoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "promote <path>",
		Short: "Move a top-level file or directory from a working copy into the harness",
		Long: `promote moves a file or directory that lives only in the current working
copy into the meta's .mws/ harness dir, replaces the original with a symlink,
and backfills the symlink into every other working copy so the item is now
shared across all copies.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var arg string
			if len(args) == 1 {
				arg = args[0]
			}
			return runPromote(newConsoleReporter(), arg)
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
		return errors.New("promote must be run from a working copy, not from the meta root")
	}

	harnessRoot := filepath.Join(ws.MetaRoot, project.HarnessDirName)

	if arg == "" {
		candidates, err := promoteCandidates(ws.WorkingCopy, harnessRoot)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return errors.New("no promotable top-level entries (everything is already a symlink or already exists in the harness)")
		}
		opts := make([]huh.Option[string], 0, len(candidates))
		for _, name := range candidates {
			opts = append(opts, huh.NewOption(name, name))
		}
		var picked string
		if err := huh.NewSelect[string]().
			Title("Select entry to promote into the harness").
			Options(opts...).
			Value(&picked).
			Run(); err != nil {
			return err
		}
		arg = filepath.Join(ws.WorkingCopy, picked)
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
	if filepath.Dir(rel) != "." {
		return fmt.Errorf("promote only handles top-level entries; got nested path %q", rel)
	}

	dst := filepath.Join(harnessRoot, rel)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("harness already has %s; resolve manually", rel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(harnessRoot, 0o755); err != nil {
		return fmt.Errorf("ensure harness dir %s: %w", harnessRoot, err)
	}

	if err := os.Rename(abs, dst); err != nil {
		return fmt.Errorf("move %s -> %s: %w", abs, dst, err)
	}
	relTarget, err := filepath.Rel(ws.WorkingCopy, dst)
	if err != nil {
		return err
	}
	if err := project.AtomicSymlink(relTarget, abs); err != nil {
		// Try to roll back the rename so the working copy is restored.
		if rbErr := os.Rename(dst, abs); rbErr != nil {
			return fmt.Errorf("symlink %s failed and rollback failed; content is at %s: %w", abs, dst, errors.Join(err, rbErr))
		}
		return fmt.Errorf("symlink %s -> %s: %w", abs, relTarget, err)
	}
	r.OK(fmt.Sprintf("Promoted %s into harness", rel))

	peers, err := project.EnumerateWorkingCopies(ws.MetaRoot)
	if err != nil {
		return err
	}
	for _, peer := range peers {
		if peer == ws.WorkingCopy {
			continue
		}
		linkPath := filepath.Join(peer, rel)
		if st, err := os.Lstat(linkPath); err == nil && st.Mode()&os.ModeSymlink == 0 {
			r.Warn(fmt.Sprintf("%s: %s exists and is not a symlink; skipping", filepath.Base(peer), rel))
			continue
		}
		target, err := filepath.Rel(peer, dst)
		if err != nil {
			r.Fail(fmt.Sprintf("%s: %v", filepath.Base(peer), err))
			continue
		}
		if err := project.AtomicSymlink(target, linkPath); err != nil {
			r.Fail(fmt.Sprintf("%s: %v", filepath.Base(peer), err))
			continue
		}
		r.OK(fmt.Sprintf("%s: linked %s", filepath.Base(peer), rel))
	}
	return nil
}

// promoteCandidates returns sorted top-level entries in workingCopy that can be promoted:
// not symlinks (already shared) and not present in harnessRoot (would conflict).
func promoteCandidates(workingCopy, harnessRoot string) ([]string, error) {
	entries, err := os.ReadDir(workingCopy)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		st, err := os.Lstat(filepath.Join(workingCopy, e.Name()))
		if err != nil || st.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if _, err := os.Lstat(filepath.Join(harnessRoot, e.Name())); err == nil {
			continue
		}
		out = append(out, e.Name())
	}
	return out, nil
}

func hasParentSegment(rel string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(rel), "/"), "..")
}

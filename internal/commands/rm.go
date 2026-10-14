package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/trash"
	"github.com/sustinbebustin/mws/internal/worktree"
)

func newRmCmd() *cobra.Command {
	yes := false
	purge := false
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a working copy",
		Long: `rm removes a working copy directory from the meta workspace. By default
the copy is moved into <meta>/.trash/ rather than deleted, so it can be brought
back with 'mws restore'. A copy past its retention window (7 days by default,
see trash.retention_days in .mws.toml) is purged the next time an mws command
touches the trash -- nothing purges in the background. Force one with
'mws trash prune'.

Pass --purge to delete the working copy outright with no way back. Either way
the meta workspace and harness are untouched.

The copy's worktrees go with it. rm refuses while another process works inside
one of them.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runRm(cmd.Context(), newConsoleReporter(), name, yes, purge)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	cmd.Flags().BoolVar(&purge, "purge", false, "delete immediately instead of moving to the trash")
	return cmd
}

func runRm(ctx context.Context, r Reporter, name string, yes, purge bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ws, err := project.Locate(cwd)
	if err != nil {
		return err
	}
	// Locate tolerates a malformed .mws.toml; surface the parse error here so
	// the rm guard doesn't compare against a wrong copies root.
	cfg, err := config.Load(ws.MetaRoot)
	if err != nil {
		return err
	}
	policy := cfg.TrashPolicy()
	sweepTrash(r, ws.MetaRoot, policy)
	toTrash := policy.Enabled && !purge

	peers, err := ws.EnumerateCopies()
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		return fmt.Errorf("no working copies to remove")
	}

	if name == "" {
		opts := make([]huh.Option[string], 0, len(peers))
		for _, p := range peers {
			opts = append(opts, huh.NewOption(filepath.Base(p), filepath.Base(p)))
		}
		if err := huh.NewSelect[string]().
			Title("Select working copy to remove").
			Options(opts...).
			Value(&name).
			Run(); err != nil {
			return err
		}
	}

	target := filepath.Join(ws.CopiesRoot(), name)
	if !slices.Contains(peers, target) {
		return fmt.Errorf("%s is not a working copy of %s", target, ws.MetaRoot)
	}
	if !looksLikeWorkingCopy(ws.MetaRoot, target) {
		return fmt.Errorf("%s does not contain any harness symlinks pointing into %s/.mws/; refusing to remove. If this is genuinely a working copy whose symlinks are broken, run `mws relink` first, or remove it manually", target, ws.MetaRoot)
	}
	wts, err := copyWorktrees(ctx, r, ws, cfg, target)
	if err != nil {
		return err
	}
	if err := refuseLiveWorktrees(target, wts); err != nil {
		return err
	}

	if !yes {
		var ok bool
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Remove working copy %s?", target)).
			Description(rmConfirmDescription(ws.MetaRoot, name, toTrash, wts)).
			Affirmative("Remove").
			Negative("Cancel").
			Value(&ok).
			Run(); err != nil {
			return err
		}
		if !ok {
			r.Warn("Cancelled.")
			return nil
		}
	}

	if !toTrash {
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		r.OK(fmt.Sprintf("Deleted %s", target))
		if len(wts) > 0 {
			if err := git.PruneWorktrees(ctx, ws.MetaRoot); err != nil {
				r.Warn(fmt.Sprintf("Could not drop the deleted worktrees' registrations from the meta repo: %v. Run `git worktree prune` in %s", err, ws.MetaRoot))
			}
		}
		return nil
	}

	entry, err := trash.Stash(ws.MetaRoot, target)
	if err != nil {
		return err
	}
	r.OK(fmt.Sprintf("Trashed %s -> %s", target, filepath.Join(trash.DirName, entry.ID)))
	// Point the worktree registrations into the trash, so a `git worktree
	// prune` on the meta repo before the restore does not drop them.
	if _, err := worktree.RepairCopy(ctx, ws.MetaRoot, entry.CopyPath, cfg.WorktreePolicy()); err != nil {
		r.Warn(fmt.Sprintf("Could not re-point the worktrees of %s at the trash: %v. They restore intact unless `mws trash prune|empty` or `mws wt prune` runs before `mws restore %s`", name, err, entry.Name))
	}
	if policy.Retention > 0 {
		r.Info(fmt.Sprintf("Restore within %s with `mws restore %s`.", humanDuration(policy.Retention), entry.Name))
	} else {
		r.Info(fmt.Sprintf("Kept until purged. Restore with `mws restore %s`.", entry.Name))
	}
	return nil
}

// rmConfirmDescription explains what removing the copy called name does,
// naming its worktrees, which go with it.
func rmConfirmDescription(metaRoot, name string, toTrash bool, wts []worktree.Entry) string {
	contents := "the directory and any native repo clones inside it"
	if len(wts) > 0 {
		names := make([]string, len(wts))
		for i := range wts {
			names[i] = wts[i].Name
		}
		contents = fmt.Sprintf("the directory, any native repo clones inside it, and its %s %s", plural(len(wts), "worktree", "worktrees"), strings.Join(names, ", "))
	}
	if toTrash {
		return fmt.Sprintf("This moves %s into %s. Bring it back with `mws restore %s`.", contents, trash.Root(metaRoot), name)
	}
	return fmt.Sprintf("This deletes %s. It cannot be undone.", contents)
}

// copyWorktrees lists the managed worktrees of the working copy at copyPath.
// A copy without worktrees needs no git, so a meta workspace that is not a
// git repository still works.
func copyWorktrees(ctx context.Context, r Reporter, ws *project.Workspace, cfg *config.Config, copyPath string) ([]worktree.Entry, error) {
	roots, err := worktree.Roots(copyPath, cfg.WorktreePolicy())
	if err != nil {
		return nil, fmt.Errorf("find worktrees of %s: %w", copyPath, err)
	}
	if len(roots) == 0 {
		return nil, nil
	}
	scoped := project.Workspace{MetaRoot: ws.MetaRoot, WorkingCopiesDir: ws.WorkingCopiesDir, WorkingCopy: copyPath}
	entries, err := worktree.List(ctx, r, worktree.ListOptions{Workspace: &scoped, Config: cfg})
	if err != nil {
		return nil, err
	}
	var managed []worktree.Entry
	for i := range entries {
		if entries[i].State == worktree.Managed {
			managed = append(managed, entries[i])
		}
	}
	return managed, nil
}

// refuseLiveWorktrees fails when another process works inside one of the
// copy's worktrees: moving the copy would pull the directory from under it.
func refuseLiveWorktrees(copyPath string, wts []worktree.Entry) error {
	var live []string
	for i := range wts {
		e := &wts[i]
		if len(e.Live) == 0 {
			continue
		}
		pids := make([]string, len(e.Live))
		for i, pid := range e.Live {
			pids[i] = strconv.Itoa(pid)
		}
		live = append(live, fmt.Sprintf("%s (process %s)", e.Name, strings.Join(pids, ", ")))
	}
	if len(live) == 0 {
		return nil
	}
	return fmt.Errorf("refusing to remove working copy %s: its worktrees are in use: %s. Stop those processes, then retry; nothing was removed", copyPath, strings.Join(live, "; "))
}

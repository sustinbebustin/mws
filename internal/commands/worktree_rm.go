package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/worktree"
)

type wtRmFlags struct {
	force, keepBranch, dryRun bool
}

func newWorktreeRmCmd(jsonOut *bool) *cobra.Command {
	var f wtRmFlags
	cmd := &cobra.Command{
		Use:   "rm <name>...",
		Short: "Remove worktrees, deleting their fully merged branches",
		Long: `rm removes each named worktree: its native repo worktrees, then its root.
A branch named <prefix><name> is deleted only when no other branch, tag or
remote-tracking branch reaches all of its commits; otherwise it is kept and
reported. --keep-branch keeps every branch.

Every worktree is checked before anything is removed. rm refuses (exit 3) a
directory that is not both marked and registered, a native repo with modified
or untracked files or with commits on a detached HEAD that no branch reaches,
root files mws did not create or that changed since creation (--force
discards all three), and a worktree some process
outside rm's own process tree is running in. The live check needs /proc; on
other systems it is skipped with a warning.

A name with no worktree exits 4 (not_found), so a retried rm can tell "already
gone" from a failure. --dry-run runs every check and lists what would be
removed, changing nothing.`,
		Example: `  mws wt rm 04
  mws wt rm 04 --dry-run --json
  mws wt rm 04 05 --keep-branch --json`,
		Args: usageCheckedArgs(cobra.MinimumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorktreeRm(cmd.Context(), newOutput(cmd, *jsonOut), args, f)
		},
	}
	cmd.Flags().BoolVar(&f.force, "force", false, "discard modified and untracked files and unreachable detached commits in the native repos, and changed files at the root")
	cmd.Flags().BoolVar(&f.keepBranch, "keep-branch", false, "keep every branch, merged or not")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "run every check and list what would be removed; change nothing")
	return cmd
}

func runWorktreeRm(ctx context.Context, o output, names []string, f wtRmFlags) error {
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	// A retried rm finds some names already gone: skip those, so the rest are
	// still removed. Only when every name is gone is it "not found".
	present, missing := splitMissing(ctx, ws, cfg, names)
	if len(present) == 0 {
		return o.fail(clierr.NotFoundf("no worktree named %s in working copy %s; a previous rm may have removed them; `mws wt ls` lists the worktrees",
			strings.Join(missing, ", "), ws.WorkingCopy))
	}
	r := o.reporter()
	for _, name := range missing {
		r.Info(fmt.Sprintf("No worktree named %s; skipped", name))
	}
	opts := worktree.RemoveOptions{Workspace: ws, Config: cfg, Names: present, Force: f.force, KeepBranch: f.keepBranch}

	if f.dryRun {
		checked, err := worktree.Check(ctx, r, opts)
		if err != nil {
			return o.fail(err)
		}
		return o.result(struct {
			DryRun   bool               `json:"dry_run"`
			Removed  []worktree.Checked `json:"removed"`
			NotFound []string           `json:"not_found"`
		}{true, checked, missing}, func(w io.Writer) error {
			for _, c := range checked {
				if _, err := fmt.Fprintf(w, "would remove %s\n", c.Path); err != nil {
					return err
				}
			}
			return nil
		})
	}

	removed, err := worktree.Remove(ctx, r, opts)
	switch {
	case err == nil:
		return o.result(struct {
			Removed  []worktree.Removed `json:"removed"`
			NotFound []string           `json:"not_found"`
		}{removed, missing}, func(w io.Writer) error { return printRemoved(w, removed) })
	case len(removed) == 0:
		return o.fail(err)
	case o.json:
		// A later name failed after earlier ones were removed: report both.
		return errors.Join(err, writeJSON(o.stdout, struct {
			Removed  []worktree.Removed `json:"removed"`
			NotFound []string           `json:"not_found"`
			Error    *jsonError         `json:"error"`
		}{removed, missing, toJSONError(err)}))
	default:
		return errors.Join(err, printRemoved(o.stdout, removed))
	}
}

// splitMissing separates the names with no worktree from the rest; any other
// problem with a name is left for Remove to report.
func splitMissing(ctx context.Context, ws *project.Workspace, cfg *config.Config, names []string) (present, missing []string) {
	missing = []string{}
	for _, name := range names {
		if _, err := worktree.Resolve(ctx, ws, cfg.WorktreePolicy(), name); clierr.KindOf(err) == clierr.NotFound {
			missing = append(missing, name)
			continue
		}
		present = append(present, name)
	}
	return present, missing
}

// printRemoved writes one line per removed worktree and one per kept branch.
func printRemoved(w io.Writer, removed []worktree.Removed) error {
	for _, r := range removed {
		if _, err := fmt.Fprintf(w, "removed %s\n", r.Path); err != nil {
			return err
		}
		for _, b := range r.Branches {
			if b.Deleted {
				continue
			}
			if _, err := fmt.Fprintf(w, "kept %s:%s, %s\n", b.Folder, b.Branch, b.Reason); err != nil {
				return err
			}
		}
	}
	return nil
}

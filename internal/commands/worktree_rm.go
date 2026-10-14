package commands

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/worktree"
)

type wtRmFlags struct {
	force, keepBranch bool
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
or untracked files or with commits on a detached HEAD that no branch reaches
(--force discards both), and a worktree some process
outside rm's own process tree is running in. The live check needs /proc; on
other systems it is skipped with a warning.`,
		Example: `  mws wt rm 04
  mws wt rm 04 05 --keep-branch --json`,
		Args: usageCheckedArgs(cobra.MinimumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorktreeRm(cmd.Context(), newWtOutput(cmd, *jsonOut), args, f)
		},
	}
	cmd.Flags().BoolVar(&f.force, "force", false, "discard modified and untracked files, and unreachable detached commits, in the native repos")
	cmd.Flags().BoolVar(&f.keepBranch, "keep-branch", false, "keep every branch, merged or not")
	return cmd
}

func runWorktreeRm(ctx context.Context, o wtOutput, names []string, f wtRmFlags) error {
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	removed, err := worktree.Remove(ctx, o.reporter(), worktree.RemoveOptions{
		Workspace:  ws,
		Config:     cfg,
		Names:      names,
		Force:      f.force,
		KeepBranch: f.keepBranch,
	})
	switch {
	case err == nil:
		return o.result(struct {
			Removed []worktree.Removed `json:"removed"`
		}{removed}, func(w io.Writer) error { return printRemoved(w, removed) })
	case len(removed) == 0:
		return o.fail(err)
	case o.json:
		// A later name failed after earlier ones were removed: report both.
		return errors.Join(err, writeJSON(o.stdout, struct {
			Removed []worktree.Removed `json:"removed"`
			Error   *jsonError         `json:"error"`
		}{removed, toJSONError(err)}))
	default:
		return errors.Join(err, printRemoved(o.stdout, removed))
	}
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

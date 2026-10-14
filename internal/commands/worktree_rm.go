package commands

import (
	"context"
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
or untracked files (--force discards them), and a worktree some process
outside rm's own process tree is running in. The live check needs /proc; on
other systems it is skipped with a warning.`,
		Example: `  mws wt rm 04
  mws wt rm 04 05 --keep-branch --json`,
		Args: wtArgs(cobra.MinimumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorktreeRm(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), *jsonOut, args, f)
		},
	}
	cmd.Flags().BoolVar(&f.force, "force", false, "discard modified and untracked files in the native repos")
	cmd.Flags().BoolVar(&f.keepBranch, "keep-branch", false, "keep every branch, merged or not")
	return cmd
}

func runWorktreeRm(ctx context.Context, out, errW io.Writer, jsonOut bool, names []string, f wtRmFlags) error {
	ws, cfg, err := locateForWorktree()
	if err != nil {
		return emitWtError(out, jsonOut, err)
	}
	removed, err := worktree.Remove(ctx, consoleReporter{w: errW}, worktree.RemoveOptions{
		Workspace:  ws,
		Config:     cfg,
		Names:      names,
		Force:      f.force,
		KeepBranch: f.keepBranch,
	})
	if err != nil {
		return emitWtError(out, jsonOut, err)
	}
	if jsonOut {
		return writeJSON(out, struct {
			Removed []worktree.Removed `json:"removed"`
		}{removed})
	}
	for _, r := range removed {
		if _, err := fmt.Fprintf(out, "removed %s\n", r.Path); err != nil {
			return err
		}
		for _, b := range r.Branches {
			if b.Deleted {
				continue
			}
			if _, err := fmt.Fprintf(out, "kept %s:%s, %s\n", b.Folder, b.Branch, b.Reason); err != nil {
				return err
			}
		}
	}
	return nil
}

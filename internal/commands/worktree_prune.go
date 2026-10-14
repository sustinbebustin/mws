package commands

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/worktree"
)

func newWorktreePruneCmd(jsonOut *bool) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove every clean, idle, fully merged worktree",
		Long: `prune tidies this working copy's worktrees in three steps:

  1. repairs marked roots whose registration went stale (e.g. after a restore)
  2. runs git worktree prune in the meta repo and every native repo, dropping
     registrations whose directories are gone
  3. removes, like rm without --force, every managed worktree whose native
     repos are clean, that no process runs in, and whose branches hold no
     commit missing from other refs; the worktree prune runs in is kept

It prints what it kept and why. --dry-run reports the same and changes nothing.`,
		Example: `  mws wt prune --dry-run
  mws wt prune --json`,
		Args: wtArgs(cobra.NoArgs, jsonOut),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorktreePrune(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), *jsonOut, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be repaired, dropped and removed; change nothing")
	return cmd
}

func runWorktreePrune(ctx context.Context, out, errW io.Writer, jsonOut, dryRun bool) error {
	ws, cfg, err := locateForWorktree()
	if err != nil {
		return emitWtError(out, jsonOut, err)
	}
	res, err := worktree.Prune(ctx, consoleReporter{w: errW}, worktree.PruneOptions{Workspace: ws, Config: cfg, DryRun: dryRun})
	if err != nil {
		return emitWtError(out, jsonOut, err)
	}
	if jsonOut {
		return writeJSON(out, res)
	}
	would := ""
	if dryRun {
		would = "would "
	}
	var lines []string
	for _, p := range res.Repaired {
		lines = append(lines, fmt.Sprintf("%srepair %s", would, p))
	}
	for _, p := range res.Dropped {
		lines = append(lines, fmt.Sprintf("%sdrop registration of %s (directory gone)", would, p))
	}
	for _, r := range res.Removed {
		lines = append(lines, fmt.Sprintf("%sremove %s", would, r.Path))
		for _, b := range r.Branches {
			if !b.Deleted {
				lines = append(lines, fmt.Sprintf("kept %s:%s, %s", b.Folder, b.Branch, b.Reason))
			}
		}
	}
	for _, k := range res.Kept {
		lines = append(lines, fmt.Sprintf("kept %s: %s", k.Name, strings.Join(k.Reasons, "; ")))
	}
	if len(lines) == 0 {
		lines = append(lines, "nothing to prune")
	}
	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

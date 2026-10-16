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
     repos are clean and on a branch, that no process runs in, and whose
     branches hold no commit missing from other refs; the worktree prune
     runs in is kept

It prints what it kept and why. --dry-run reports the same and changes nothing.`,
		Example: `  mws wt prune --dry-run
  mws wt prune --json`,
		Args: usageCheckedArgs(cobra.NoArgs, jsonOut),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorktreePrune(cmd.Context(), newOutput(cmd, *jsonOut), dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be repaired, dropped and removed; change nothing")
	return cmd
}

func runWorktreePrune(ctx context.Context, o output, dryRun bool) error {
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	res, err := worktree.Prune(ctx, o.reporter(), worktree.PruneOptions{Workspace: ws, Config: cfg, DryRun: dryRun})
	if err != nil {
		return o.fail(err)
	}
	return o.result(res, func(w io.Writer) error {
		if res.DryRun {
			return printPruneDryRun(w, o.reporter(), res)
		}
		return printPrune(w, res)
	})
}

// printPruneDryRun writes one "would <verb> <object>" line per planned change
// to w; what prune would keep, and why, goes to r.
func printPruneDryRun(w io.Writer, r Reporter, res worktree.PruneResult) error {
	var lines []string
	for _, p := range res.Repaired {
		lines = append(lines, "would repair "+p)
	}
	for _, p := range res.Dropped {
		lines = append(lines, "would drop registration of "+p)
	}
	for _, rm := range res.Removed {
		lines = append(lines, "would remove "+rm.Path)
	}
	for _, k := range res.Kept {
		r.Info(fmt.Sprintf("kept %s: %s", k.Name, strings.Join(k.Reasons, "; ")))
	}
	if len(lines) == 0 {
		r.Info("nothing to prune")
		return nil
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

// printPrune writes one line per repair, dropped registration, removal and
// kept worktree of a real prune, or "nothing to prune".
func printPrune(w io.Writer, res worktree.PruneResult) error {
	var lines []string
	for _, p := range res.Repaired {
		lines = append(lines, "repair "+p)
	}
	for _, p := range res.Dropped {
		lines = append(lines, fmt.Sprintf("drop registration of %s (directory gone)", p))
	}
	for _, r := range res.Removed {
		lines = append(lines, "remove "+r.Path)
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
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

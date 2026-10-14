package commands

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/worktree"
)

func newListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List working copies in this meta workspace",
		Long: `list prints one line per working copy on stdout, marking the one containing
the current directory with "*" and counting its worktrees. --json prints
{"meta", "copies": [{"name", "path", "current", "worktrees"}]}.`,
		Example: `  mws list
  mws list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(newOutput(cmd, jsonOut))
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print the working copies as JSON")
	return cmd
}

// copyJSON is one working copy in `mws list --json`.
type copyJSON struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Current   bool   `json:"current"`
	Worktrees int    `json:"worktrees"`
}

func runList(o output) error {
	// locate loads the config explicitly: Locate tolerates a malformed
	// .mws.toml, and listing must not scan a (possibly wrong) copies root.
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	policy := cfg.WorktreePolicy()

	peers, err := ws.EnumerateCopies()
	if err != nil {
		return o.fail(clierr.Failuref("list the working copies of %s: %w", ws.MetaRoot, err))
	}
	copies := make([]copyJSON, 0, len(peers))
	for _, peer := range peers {
		roots, err := worktree.Roots(peer, policy)
		if err != nil {
			return o.fail(clierr.Failuref("count worktrees of %s: %w", peer, err))
		}
		copies = append(copies, copyJSON{Name: filepath.Base(peer), Path: peer, Current: peer == ws.WorkingCopy, Worktrees: len(roots)})
	}
	return o.result(struct {
		Meta   string     `json:"meta"`
		Copies []copyJSON `json:"copies"`
	}{ws.MetaRoot, copies}, func(w io.Writer) error {
		r := o.reporter()
		r.Heading(fmt.Sprintf("Meta workspace: %s", ws.MetaRoot))
		if len(copies) == 0 {
			r.Info("No working copies.")
			return nil
		}
		for _, c := range copies {
			marker := "  "
			if c.Current {
				marker = "* "
			}
			line := marker + c.Name
			if c.Worktrees > 0 {
				line += fmt.Sprintf("  (+%d %s)", c.Worktrees, plural(c.Worktrees, "worktree", "worktrees"))
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
		return nil
	})
}

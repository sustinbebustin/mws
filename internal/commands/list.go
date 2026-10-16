package commands

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/ports"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/ui"
	"github.com/sustinbebustin/mws/internal/worktree"
)

func newListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List working copies in this meta workspace",
		Long: `ls prints one line per working copy on stdout, marking the one containing
the current directory with "*", naming its ports ("default" on the default
ports, "slot N" when it holds a Port slot, "invalid lease" when mws cannot
read its lease) and counting its worktrees. --json prints
{"meta", "copies": [{"copy", "path", "current", "ports", "worktrees"}]}.`,
		Example: `  mws ls
  mws ls --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(newOutput(cmd, jsonOut))
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print the working copies as JSON")
	return cmd
}

// copyJSON is one working copy in `mws ls --json`.
type copyJSON struct {
	Copy      string `json:"copy"`
	Path      string `json:"path"`
	Current   bool   `json:"current"`
	Ports     string `json:"ports"`
	Worktrees int    `json:"worktrees"`
}

// invalidLease is the ports label of a copy whose Port lease mws cannot read.
const invalidLease = "invalid lease"

func runList(o output) error {
	// locate loads the config explicitly: Locate tolerates a malformed
	// .mws.toml, and listing must not scan a (possibly wrong) copies root.
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	copies, err := collectCopies(o.reporter(), ws, cfg)
	if err != nil {
		return o.fail(err)
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
		s := ui.Detect(w)
		width := 0
		for _, c := range copies {
			width = max(width, len(c.Copy))
		}
		for _, c := range copies {
			marker := "  "
			if c.Current {
				marker = "* "
			}
			line := fmt.Sprintf("%s%-*s  %s", marker, width, c.Copy, c.Ports)
			if c.Worktrees > 0 {
				line += fmt.Sprintf("  (+%d %s)", c.Worktrees, plural(c.Worktrees, "worktree", "worktrees"))
			}
			if c.Current {
				line = s.Brand(line)
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
		return nil
	})
}

// collectCopies lists the working copies of ws with their ports and worktree
// counts, marking the one containing the current directory. An unreadable
// lease is warned about on r and labelled invalidLease.
func collectCopies(r Reporter, ws *project.Workspace, cfg *config.Config) ([]copyJSON, error) {
	peers, err := ws.EnumerateCopies()
	if err != nil {
		return nil, clierr.Failuref("list the working copies of %s: %w", ws.MetaRoot, err)
	}
	policy := cfg.WorktreePolicy()
	copies := make([]copyJSON, 0, len(peers))
	for _, peer := range peers {
		roots, err := worktree.Roots(peer, policy)
		if err != nil {
			return nil, clierr.Failuref("count worktrees of %s: %w", peer, err)
		}
		copies = append(copies, copyJSON{Copy: filepath.Base(peer), Path: peer, Current: peer == ws.WorkingCopy, Ports: portsLabel(r, peer), Worktrees: len(roots)})
	}
	return copies, nil
}

// portsLabel names the ports of the working copy at copyPath: "default",
// "slot N", or invalidLease, warned about on r, when its lease is unreadable.
func portsLabel(r Reporter, copyPath string) string {
	slot, err := ports.ReadSlot(copyPath)
	if err != nil {
		r.Warn(err.Error())
		return invalidLease
	}
	return slotLabel(slot)
}

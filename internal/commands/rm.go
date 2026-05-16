package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/project"
)

func newRmCmd() *cobra.Command {
	yes := false
	cmd := &cobra.Command{
		Use:   "rm <peer>",
		Short: "Remove a peer working copy",
		Long: `rm deletes a peer working copy directory. The peer's native repos and any
non-symlink files inside it are removed; the meta workspace is untouched.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var peerName string
			if len(args) == 1 {
				peerName = args[0]
			}
			return runRm(newConsoleReporter(), peerName, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	return cmd
}

func runRm(r Reporter, peerName string, yes bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ws, err := project.Locate(cwd)
	if err != nil {
		return err
	}

	peers, err := project.EnumeratePeers(ws.MetaRoot)
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		return fmt.Errorf("no peer working copies to remove")
	}

	parent := filepath.Dir(ws.MetaRoot)

	if peerName == "" {
		opts := make([]huh.Option[string], 0, len(peers))
		for _, p := range peers {
			opts = append(opts, huh.NewOption(filepath.Base(p), filepath.Base(p)))
		}
		if err := huh.NewSelect[string]().
			Title("Select peer working copy to remove").
			Options(opts...).
			Value(&peerName).
			Run(); err != nil {
			return err
		}
	}

	target := filepath.Join(parent, peerName)

	if target == ws.MetaRoot {
		return fmt.Errorf("refusing to remove the meta workspace")
	}

	known := false
	for _, p := range peers {
		if p == target {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("%s is not a peer working copy of %s", target, ws.MetaRoot)
	}

	if !yes {
		var ok bool
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Remove peer working copy %s?", target)).
			Description("This deletes the directory and any native repo clones inside it.").
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

	if err := os.RemoveAll(target); err != nil {
		return err
	}
	r.OK(fmt.Sprintf("Removed %s", target))
	return nil
}

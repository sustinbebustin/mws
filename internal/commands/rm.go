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
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRm(newConsoleReporter(), args[0], yes)
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

	parent := filepath.Dir(ws.MetaRoot)
	target := filepath.Join(parent, peerName)

	if target == ws.MetaRoot {
		return fmt.Errorf("refusing to remove the meta workspace")
	}

	peers, err := project.EnumeratePeers(ws.MetaRoot)
	if err != nil {
		return err
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

package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

func newRmCmd() *cobra.Command {
	yes := false
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a working copy",
		Long: `rm deletes a working copy directory inside the meta workspace. The
working copy's native repos and any non-symlink files inside it are removed;
the meta workspace and harness are untouched.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runRm(newConsoleReporter(), name, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	return cmd
}

func runRm(r Reporter, name string, yes bool) error {
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
	if _, err := config.Load(ws.MetaRoot); err != nil {
		return err
	}

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

	if !yes {
		var ok bool
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Remove working copy %s?", target)).
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

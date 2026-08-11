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
	"github.com/sustinbebustin/mws/internal/trash"
)

func newRmCmd() *cobra.Command {
	yes := false
	purge := false
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a working copy",
		Long: `rm removes a working copy directory from the meta workspace. By default
the copy is moved into <meta>/.trash/ rather than deleted, so it can be brought
back with 'mws restore'. A copy past its retention window (7 days by default,
see trash.retention_days in .mws.toml) is purged the next time an mws command
touches the trash -- nothing purges in the background. Force one with
'mws trash prune'.

Pass --purge to delete the working copy outright with no way back. Either way
the meta workspace and harness are untouched.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runRm(newConsoleReporter(), name, yes, purge)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	cmd.Flags().BoolVar(&purge, "purge", false, "delete immediately instead of moving to the trash")
	return cmd
}

func runRm(r Reporter, name string, yes, purge bool) error {
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
	cfg, err := config.Load(ws.MetaRoot)
	if err != nil {
		return err
	}
	policy := cfg.TrashPolicy()
	sweepTrash(r, ws.MetaRoot, policy)
	toTrash := policy.Enabled && !purge

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
		description := "This deletes the directory and any native repo clones inside it. It cannot be undone."
		if toTrash {
			description = fmt.Sprintf("This moves the directory and any native repo clones inside it into %s. Bring it back with `mws restore %s`.", trash.Root(ws.MetaRoot), name)
		}
		var ok bool
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Remove working copy %s?", target)).
			Description(description).
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

	if !toTrash {
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		r.OK(fmt.Sprintf("Deleted %s", target))
		return nil
	}

	entry, err := trash.Stash(ws.MetaRoot, target)
	if err != nil {
		return err
	}
	r.OK(fmt.Sprintf("Trashed %s -> %s", target, filepath.Join(trash.DirName, entry.ID)))
	if policy.Retention > 0 {
		r.Info(fmt.Sprintf("Restore within %s with `mws restore %s`.", humanDuration(policy.Retention), entry.Name))
	} else {
		r.Info(fmt.Sprintf("Kept until purged. Restore with `mws restore %s`.", entry.Name))
	}
	return nil
}

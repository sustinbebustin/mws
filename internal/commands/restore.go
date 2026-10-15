package commands

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/trash"
	"github.com/sustinbebustin/mws/internal/worktree"
)

func newRestoreCmd() *cobra.Command {
	var as string
	yes := false
	cmd := &cobra.Command{
		Use:   "restore [copy]",
		Short: "Bring a trashed working copy back",
		Long: `restore moves a working copy out of <meta>/.trash/ and back under the
meta workspace, then repairs its harness symlinks and the git links of its
worktrees, which keep their branches and uncommitted files. With no name, it
prompts with the contents of the trash.

If a name was trashed more than once, restore prompts for which entry to bring
back. --yes skips every prompt and takes the most recent match -- or, with no
name given, the most recently trashed copy of any name. restore never
overwrites: if a working copy already holds the name, use --as to restore under
a different one. Without a terminal, give a name (or --yes) wherever restore
would prompt. A name with no trash entry exits 4 (not found); the message says
when a working copy of that name exists, as after a retried restore.`,
		Example: `  mws restore feature-x
  mws restore feature-x --yes
  mws restore feature-x --as feature-x-old`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runRestore(cmd.Context(), newConsoleReporter(), name, as, yes)
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "restore under a different working copy name")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip prompts; pick the most recent matching entry")
	return cmd
}

func runRestore(ctx context.Context, r Reporter, name, as string, yes bool) error {
	ws, cfg, err := locateForTrash()
	if err != nil {
		return err
	}
	policy := cfg.TrashPolicy()
	// Sweep first so an entry that is already past its retention can't be
	// resurrected by a well-timed restore.
	sweepTrash(r, ws.MetaRoot, policy)

	entry, err := selectEntry(ws.MetaRoot, name, yes, policy)
	if clierr.KindOf(err) == clierr.NotFound && looksLikeWorkingCopy(ws.MetaRoot, filepath.Join(ws.CopiesRoot(), name)) {
		// A retried restore finds the copy back in place and the trash without it.
		return clierr.NotFoundf("no trashed working copy named %q, but %s is a working copy, so a previous restore already brought it back", name, filepath.Join(ws.CopiesRoot(), name))
	}
	if err != nil {
		return err
	}

	destName := entry.Name
	if as != "" {
		destName = as
	}
	if err := project.ValidateName(destName); err != nil {
		return fmt.Errorf("restore name %q: %w", destName, err)
	}
	dest := filepath.Join(ws.CopiesRoot(), destName)
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite it. Restore under another name with `mws restore %s --as <newname>`, or remove the existing copy first", dest, entry.Name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", dest, err)
	}

	if err := trash.Restore(entry, dest); err != nil {
		return err
	}
	r.OK(fmt.Sprintf("Restored %s -> %s", entry.ID, dest))

	// The harness symlinks were written relative to the copy's old location and
	// dangled while it sat in the trash; re-link them against the real one.
	linked, err := project.LinkHarnessIntoWorkingCopy(ws.MetaRoot, dest)
	if err != nil {
		return fmt.Errorf("restored %s but failed to relink the harness: %w. Run `mws relink` to retry", dest, err)
	}
	if len(linked) > 0 {
		r.OK(fmt.Sprintf("Relinked %d harness %s", len(linked), plural(len(linked), "entry", "entries")))
	}

	// Worktree links are absolute and still name the trash (or the old name).
	repaired, err := worktree.RepairCopy(ctx, ws.MetaRoot, dest, cfg.WorktreePolicy())
	if err != nil {
		return fmt.Errorf("restored %s but %w. Its files are intact; run `mws wt prune` in %s to retry the repair", dest, err, dest)
	}
	if n := len(repaired); n > 0 {
		r.OK(fmt.Sprintf("Repaired %d %s", n, plural(n, "worktree", "worktrees")))
	}
	return nil
}

// selectEntry resolves the trash entry to restore. An empty name prompts over
// the whole trash; a name with several entries prompts over just those, unless
// yes was passed, in which case the most recent wins.
func selectEntry(metaRoot, name string, yes bool, policy config.TrashPolicy) (trash.Entry, error) {
	if name != "" {
		matches, err := trash.Find(metaRoot, name)
		if err != nil {
			return trash.Entry{}, err
		}
		switch {
		case len(matches) == 0:
			return trash.Entry{}, clierr.NotFoundf("no trashed working copy named %q in %s. Run `mws trash ls` to see what is there", name, trash.Root(metaRoot))
		case len(matches) == 1 || yes:
			// Find returns newest first, so matches[0] is the most recent.
			return matches[0], nil
		default:
			return promptEntry(matches, policy, fmt.Sprintf("%q was trashed %d times. Which one?", name, len(matches)),
				fmt.Sprintf("%q was trashed %d times", name, len(matches)))
		}
	}

	entries, _, err := trash.List(metaRoot)
	if err != nil {
		return trash.Entry{}, err
	}
	if len(entries) == 0 {
		return trash.Entry{}, clierr.NotFoundf("the trash at %s is empty; there is nothing to restore", trash.Root(metaRoot))
	}
	if yes {
		return entries[0], nil
	}
	return promptEntry(entries, policy, "Select a working copy to restore", "no name given")
}

// promptEntry asks which of entries to restore. Without a terminal it fails
// with missing, which says why there is a choice to make.
func promptEntry(entries []trash.Entry, policy config.TrashPolicy, title, missing string) (trash.Entry, error) {
	if !canPrompt() {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name
		}
		return trash.Entry{}, noPrompt(fmt.Sprintf("%s (trashed: %s)", missing, strings.Join(names, ", ")),
			"mws restore <copy>", "mws restore <copy> --yes   (the most recent entry of that name)")
	}
	now := time.Now().UTC()
	opts := make([]huh.Option[string], 0, len(entries))
	byID := make(map[string]trash.Entry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s  (%s)", e.Name, entryAgeLine(e, policy, now)), e.ID))
	}
	var id string
	if err := runField(huh.NewSelect[string]().Title(title).Options(opts...).Value(&id)); err != nil {
		return trash.Entry{}, err
	}
	return byID[id], nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

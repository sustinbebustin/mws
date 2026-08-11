package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/trash"
)

func newRestoreCmd() *cobra.Command {
	var as string
	yes := false
	cmd := &cobra.Command{
		Use:   "restore [name]",
		Short: "Bring a trashed working copy back",
		Long: `restore moves a working copy out of <meta>/.trash/ and back under the
meta workspace, then repairs its harness symlinks. With no name, it prompts
with the contents of the trash.

If a name was trashed more than once, restore prompts for which entry to bring
back. --yes skips every prompt and takes the most recent match -- or, with no
name given, the most recently trashed copy of any name. restore never
overwrites: if a working copy already holds the name, use --as to restore under
a different one.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runRestore(newConsoleReporter(), name, as, yes)
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "restore under a different working copy name")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip prompts; pick the most recent matching entry")
	return cmd
}

func runRestore(r Reporter, name, as string, yes bool) error {
	ws, policy, err := locateForTrash()
	if err != nil {
		return err
	}
	// Sweep first so an entry that is already past its retention can't be
	// resurrected by a well-timed restore.
	sweepTrash(r, ws.MetaRoot, policy)

	entry, err := selectEntry(ws.MetaRoot, name, yes, policy)
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
	r.OK(fmt.Sprintf("Relinked %d harness %s", len(linked), plural(len(linked), "entry", "entries")))
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
			return trash.Entry{}, fmt.Errorf("no trashed working copy named %q in %s. Run `mws trash list` to see what is there", name, trash.Root(metaRoot))
		case len(matches) == 1 || yes:
			// Find returns newest first, so matches[0] is the most recent.
			return matches[0], nil
		default:
			return promptEntry(matches, policy, fmt.Sprintf("%q was trashed %d times. Which one?", name, len(matches)))
		}
	}

	entries, _, err := trash.List(metaRoot)
	if err != nil {
		return trash.Entry{}, err
	}
	if len(entries) == 0 {
		return trash.Entry{}, fmt.Errorf("the trash at %s is empty; there is nothing to restore", trash.Root(metaRoot))
	}
	if yes {
		return entries[0], nil
	}
	return promptEntry(entries, policy, "Select a working copy to restore")
}

func promptEntry(entries []trash.Entry, policy config.TrashPolicy, title string) (trash.Entry, error) {
	now := time.Now().UTC()
	opts := make([]huh.Option[string], 0, len(entries))
	byID := make(map[string]trash.Entry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s  (%s)", e.Name, entryAgeLine(e, policy, now)), e.ID))
	}
	var id string
	if err := huh.NewSelect[string]().Title(title).Options(opts...).Value(&id).Run(); err != nil {
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

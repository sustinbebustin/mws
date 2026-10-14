package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/trash"
	"github.com/sustinbebustin/mws/internal/worktree"
)

func newTrashCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "trash",
		Short: "Inspect and manage trashed working copies",
		Long: `trash manages the soft-delete area at <meta>/.trash/, where 'mws rm' puts
working copies instead of deleting them. With no subcommand it lists the
trash, the same as 'mws trash list'.

Harness symlinks inside a trashed copy point at relative paths and therefore
dangle while the copy sits in the trash. That is expected; 'mws restore'
repairs them on the way out.`,
		Example: `  mws trash
  mws trash list --json
  mws trash prune --dry-run
  mws trash empty --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrashList(newOutput(cmd, jsonOut))
		},
	}
	cmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "print the entries as JSON (list only)")
	cmd.AddCommand(newTrashListCmd(&jsonOut), newTrashPruneCmd(), newTrashEmptyCmd())
	return cmd
}

func newTrashListCmd(jsonOut *bool) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List trashed working copies",
		Long: `list prints one line per trashed working copy on stdout: its name, how long
ago it was trashed and when it purges. It first sweeps entries past their
retention window, as rm and restore do. --json prints
{"trash", "entries": [{"id", "name", "path", "deleted_at", "purges_at"}], "unreadable"}.`,
		Example: `  mws trash list
  mws trash list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrashList(newOutput(cmd, *jsonOut))
		},
	}
}

func newTrashPruneCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Purge trashed working copies past their retention window",
		Long: `prune purges every trash entry older than trash.retention_days. This is the
same sweep that 'mws rm', 'mws restore', and 'mws trash' run on their own; use
it to force one without removing or restoring anything. It then drops the
meta repo's registrations of worktrees whose directories are gone.
--dry-run names the entries it would purge and changes nothing.`,
		Example: `  mws trash prune --dry-run
  mws trash prune`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrashPrune(cmd.Context(), newConsoleReporter(), dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "name the entries that would be purged; change nothing")
	return cmd
}

func newTrashEmptyCmd() *cobra.Command {
	var f confirmFlags
	cmd := &cobra.Command{
		Use:   "empty",
		Short: "Purge every trashed working copy, regardless of age",
		Long: `empty purges every trash entry, including those still inside their
retention window, then drops the meta repo's registrations of worktrees whose
directories are gone. It asks for confirmation; --yes (required without a
terminal) skips it, and --dry-run names what would be purged and stops.
Emptying an empty trash succeeds.`,
		Example: `  mws trash empty --dry-run
  mws trash empty --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrashEmpty(cmd.Context(), newConsoleReporter(), f)
		},
	}
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "skip confirmation")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "name what would be purged; change nothing")
	return cmd
}

// trashEntryJSON is one entry of `mws trash list --json`. PurgesAt is absent
// when the retention policy keeps entries forever.
type trashEntryJSON struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Path      string     `json:"path"`
	DeletedAt time.Time  `json:"deleted_at"`
	PurgesAt  *time.Time `json:"purges_at,omitempty"`
}

func runTrashList(o output) error {
	r := o.reporter()
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	policy := cfg.TrashPolicy()
	sweepTrash(r, ws.MetaRoot, policy)

	entries, skipped, err := trash.List(ws.MetaRoot)
	if err != nil {
		return o.fail(clierr.Failuref("list the trash: %w", err))
	}
	reportSkipped(r, ws.MetaRoot, skipped)
	list := make([]trashEntryJSON, len(entries))
	for i, e := range entries {
		list[i] = trashEntryJSON{ID: e.ID, Name: e.Name, Path: e.CopyPath, DeletedAt: e.DeletedAt}
		if policy.Retention > 0 {
			at := e.DeletedAt.Add(policy.Retention)
			list[i].PurgesAt = &at
		}
	}
	return o.result(struct {
		Trash      string           `json:"trash"`
		Entries    []trashEntryJSON `json:"entries"`
		Unreadable []string         `json:"unreadable"`
	}{trash.Root(ws.MetaRoot), list, append([]string{}, skipped...)}, func(w io.Writer) error {
		if len(entries) == 0 {
			r.Info(fmt.Sprintf("Trash is empty (%s).", trash.Root(ws.MetaRoot)))
			return nil
		}
		r.Heading(fmt.Sprintf("Trashed working copies in %s", trash.Root(ws.MetaRoot)))
		now := time.Now().UTC()
		for _, e := range entries {
			if _, err := fmt.Fprintf(w, "%-24s %s\n", e.Name, entryAgeLine(e, policy, now)); err != nil {
				return err
			}
		}
		return nil
	})
}

func runTrashPrune(ctx context.Context, r Reporter, dryRun bool) error {
	ws, cfg, err := locateForTrash()
	if err != nil {
		return err
	}
	policy := cfg.TrashPolicy()
	switch {
	case policy.Retention <= 0:
		r.Info("trash.retention_days is 0, so nothing expires. Use `mws trash empty` to purge everything.")
	case dryRun:
		expired, err := trash.Expired(ws.MetaRoot, policy.Retention)
		if err != nil {
			return err
		}
		reportPurged(r, "Would purge", expired, policy)
	default:
		purged, err := trash.Sweep(ws.MetaRoot, policy.Retention)
		reportPurged(r, "Purged", purged, policy)
		if err != nil {
			return err
		}
	}
	if !dryRun {
		pruneMetaWorktrees(ctx, r, ws.MetaRoot)
	}
	return nil
}

// reportPurged names each entry a prune purged (or would purge).
func reportPurged(r Reporter, verb string, entries []trash.Entry, policy config.TrashPolicy) {
	for _, e := range entries {
		r.OK(fmt.Sprintf("%s %s (trashed %s ago)", verb, e.Name, humanDuration(time.Since(e.DeletedAt))))
	}
	if len(entries) == 0 {
		r.Info(fmt.Sprintf("Nothing past the %s retention window.", humanDuration(policy.Retention)))
	}
}

// pruneMetaWorktrees drops the meta repo's registrations of the purged copies'
// worktrees. A failure only warns, since the purge itself succeeded.
func pruneMetaWorktrees(ctx context.Context, r Reporter, metaRoot string) {
	if err := worktree.ForgetVanished(ctx, metaRoot); err != nil {
		r.Warn(fmt.Sprintf("Could not drop stale worktree registrations from the meta repo: %v. Run `git worktree prune` in %s", err, metaRoot))
	}
}

func runTrashEmpty(ctx context.Context, r Reporter, f confirmFlags) error {
	// empty ignores the retention policy on purpose -- it purges everything.
	ws, _, err := locateForTrash()
	if err != nil {
		return err
	}
	entries, skipped, err := trash.List(ws.MetaRoot)
	if err != nil {
		return err
	}
	reportSkipped(r, ws.MetaRoot, skipped)
	if len(entries) == 0 && len(skipped) == 0 {
		r.Info("Trash is already empty.")
		return nil
	}

	if f.dryRun {
		for _, e := range entries {
			r.Info(fmt.Sprintf("Would purge %s", e.Name))
		}
		if len(skipped) > 0 {
			r.Info(fmt.Sprintf("Would purge %d unreadable %s", len(skipped), plural(len(skipped), "entry", "entries")))
		}
		return nil
	}
	ok, err := confirm(r, f.yes, confirmation{
		title:       emptyPromptTitle(len(entries), len(skipped)),
		description: fmt.Sprintf("This deletes everything under %s, including entries still inside their retention window. It cannot be undone.", trash.Root(ws.MetaRoot)),
		affirmative: "Purge",
		missing:     "emptying the trash needs confirmation",
		try:         []string{"mws trash empty --yes", "mws trash empty --dry-run"},
	})
	if err != nil || !ok {
		return err
	}

	// Name what is going before clearing the directory, so the log says which
	// working copies were lost rather than just that the trash is now gone.
	for _, e := range entries {
		r.OK(fmt.Sprintf("Purged %s", e.Name))
	}
	// Remove the directory wholesale rather than looping Purge: unreadable
	// entries never reach List, and leaving them would make `empty` a lie and
	// leave them unreclaimable by any command.
	if err := trash.Empty(ws.MetaRoot); err != nil {
		return err
	}
	if len(skipped) > 0 {
		r.OK(fmt.Sprintf("Purged %d unreadable %s", len(skipped), plural(len(skipped), "entry", "entries")))
	}
	pruneMetaWorktrees(ctx, r, ws.MetaRoot)
	return nil
}

// emptyPromptTitle counts unreadable entries separately, since they are real
// directories the purge will remove but have no working-copy name to show.
func emptyPromptTitle(entries, skipped int) string {
	if skipped == 0 {
		return fmt.Sprintf("Purge all %d trashed working copies?", entries)
	}
	return fmt.Sprintf("Purge all %d trashed working copies and %d unreadable %s?",
		entries, skipped, plural(skipped, "entry", "entries"))
}

// locateForTrash resolves the workspace from cwd and its config. Like the
// other commands it loads the config explicitly, because project.Locate
// tolerates a malformed .mws.toml and would otherwise leave the caller acting
// on default retention it never configured.
func locateForTrash() (*project.Workspace, *config.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	ws, err := project.Locate(cwd)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := config.Load(ws.MetaRoot)
	if err != nil {
		return nil, nil, err
	}
	return ws, cfg, nil
}

// sweepTrash purges expired entries and reports what went. A sweep failure is
// reported but never fails the caller: not being able to tidy the trash must
// not block removing, restoring, or listing.
func sweepTrash(r Reporter, metaRoot string, policy config.TrashPolicy) {
	purged, err := trash.Sweep(metaRoot, policy.Retention)
	for _, e := range purged {
		r.Info(fmt.Sprintf("Purged %s from the trash (older than %s).", e.Name, humanDuration(policy.Retention)))
	}
	if err != nil {
		r.Warn(fmt.Sprintf("Could not sweep the trash: %v", err))
	}
}

// reportSkipped warns about entry directories that could not be read, naming
// them so the user can inspect or delete them by hand.
func reportSkipped(r Reporter, metaRoot string, skipped []string) {
	for _, id := range skipped {
		r.Warn(fmt.Sprintf("Ignoring unreadable trash entry %s.", filepath.Join(trash.Root(metaRoot), id)))
	}
}

// entryAgeLine renders how long ago an entry was trashed and how long it has
// left, e.g. "trashed 2h ago, purges in 6d".
func entryAgeLine(e trash.Entry, policy config.TrashPolicy, now time.Time) string {
	age := fmt.Sprintf("trashed %s ago", humanDuration(now.Sub(e.DeletedAt)))
	if policy.Retention <= 0 {
		return age + ", kept until purged"
	}
	left := policy.Retention - now.Sub(e.DeletedAt)
	if left <= 0 {
		return age + ", due to be purged"
	}
	return fmt.Sprintf("%s, purges in %s", age, humanDuration(left))
}

// humanDuration renders a duration at one unit of precision for status lines.
// Each unit rounds to nearest rather than truncating, so a copy trashed a
// moment ago under a 7-day retention reads "purges in 7d" and not "6d".
// Rounding is applied before choosing the unit, so a value that rounds up to a
// full unit is promoted rather than printed as "60m" or "24h". Anything under a
// minute is "less than a minute" rather than a bare "0m".
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return "less than a minute"
	}
	if m := int(d.Round(time.Minute).Minutes()); m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	if h := int(d.Round(time.Hour).Hours()); h < 24 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dd", int(d.Round(24*time.Hour).Hours()/24))
}

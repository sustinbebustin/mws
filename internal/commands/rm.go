package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/ports"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/trash"
	"github.com/sustinbebustin/mws/internal/worktree"
)

// rmFlags is the command-line input of `mws rm`.
type rmFlags struct {
	yes, purge, dryRun bool
}

func newRmCmd() *cobra.Command {
	var f rmFlags
	cmd := &cobra.Command{
		Use:   "rm <copy>",
		Short: "Remove a working copy",
		Long: `rm removes a working copy directory from the meta workspace. By default
the copy is moved into <meta>/.trash/ rather than deleted, so it can be brought
back with 'mws restore'. A copy past its retention window (7 days by default,
see trash.retention_days in .mws.toml) is purged the next time an mws command
touches the trash -- nothing purges in the background. Force one with
'mws trash prune'.

Pass --purge to delete the working copy outright with no way back. Either way
the meta workspace and harness are untouched.

The copy's worktrees go with it. rm refuses (exit 3) while another process
works inside one of them.

Without a terminal, the name and --yes are required. --dry-run runs every
check and reports what would happen without changing anything (it does not
sweep the trash either). A name that is no working copy exits 4 (not found);
when it was trashed earlier, the message names the trash entry and its age,
so a retried rm can tell "already removed" from a typo.`,
		Example: `  mws rm feature-x
  mws rm feature-x --yes
  mws rm feature-x --purge --dry-run`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runRm(cmd.Context(), newOutput(cmd, false), name, f)
		},
	}
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "skip confirmation")
	cmd.Flags().BoolVar(&f.purge, "purge", false, "delete immediately instead of moving to the trash")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "report what would be removed; change nothing")
	return cmd
}

func runRm(ctx context.Context, o output, name string, f rmFlags) error {
	r := o.reporter()
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
	if !f.dryRun {
		sweepTrash(r, ws.MetaRoot, policy)
	}
	toTrash := policy.Enabled && !f.purge

	peers, err := ws.EnumerateCopies()
	if err != nil {
		return err
	}
	if name == "" && len(peers) == 0 {
		return fmt.Errorf("no working copies to remove in %s", ws.MetaRoot)
	}

	if name == "" {
		if !canPrompt() {
			names := make([]string, len(peers))
			for i, p := range peers {
				names[i] = filepath.Base(p)
			}
			return noPrompt(fmt.Sprintf("no working copy named (working copies: %s)", strings.Join(names, ", ")), "mws rm <copy> --yes")
		}
		opts := make([]huh.Option[string], 0, len(peers))
		for _, p := range peers {
			opts = append(opts, huh.NewOption(filepath.Base(p), filepath.Base(p)))
		}
		if err := runField(huh.NewSelect[string]().
			Title("Select working copy to remove").
			Options(opts...).
			Value(&name)); err != nil {
			return err
		}
	}

	target := filepath.Join(ws.CopiesRoot(), name)
	if !slices.Contains(peers, target) {
		return notACopy(ws.MetaRoot, name, target)
	}
	if !looksLikeWorkingCopy(ws.MetaRoot, target) {
		return clierr.Refusedf("%s does not contain any harness symlinks pointing into %s/.mws/; refusing to remove. If this is genuinely a working copy whose symlinks are broken, run `mws relink` first, or remove it manually", target, ws.MetaRoot)
	}
	wts, err := copyWorktrees(ctx, r, ws.MetaRoot, cfg, target)
	if err != nil {
		return err
	}
	if err := worktree.RefuseLive(wts); err != nil {
		return fmt.Errorf("refusing to remove working copy %s: %w", target, err)
	}

	// An unreadable lease reads as slot 0 (ReadSlot returns 0 with the
	// error); a real run reports it from the release step before trashing.
	slot, leaseErr := ports.ReadSlot(target)
	description := rmConfirmDescription(ws.MetaRoot, name, toTrash, slot, wts)
	if f.dryRun {
		if leaseErr != nil && toTrash {
			r.Warn(fmt.Sprintf("%v; rm would trash %s with it, without rendering its env files at the default ports", leaseErr, name))
		}
		r.Info("Dry run: " + description)
		verb := "delete"
		if toTrash {
			verb = "trash"
			if slot > 0 {
				if _, err := fmt.Fprintf(o.stdout, "would release slot %d from %s\n", slot, name); err != nil {
					return err
				}
			}
		}
		_, err := fmt.Fprintf(o.stdout, "would %s %s\n", verb, target)
		return err
	}
	ok, err := confirm(r, f.yes, confirmation{
		title:       fmt.Sprintf("Remove working copy %s?", target),
		description: description,
		affirmative: "Remove",
		missing:     "removing a working copy needs confirmation",
		try:         []string{fmt.Sprintf("mws rm %s --yes", name), fmt.Sprintf("mws rm %s --dry-run", name)},
	})
	if err != nil || !ok {
		return err
	}

	if !toTrash {
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		r.OK(fmt.Sprintf("Deleted %s", target))
		if len(wts) > 0 {
			if err := worktree.ForgetVanished(ctx, ws.MetaRoot); err != nil {
				r.Warn(fmt.Sprintf("Could not drop the deleted worktrees' registrations from the meta repo: %v. Run `git worktree prune` in %s", err, ws.MetaRoot))
			}
		}
		return nil
	}

	releaseBeforeTrash(r, ws.MetaRoot, cfg, target)
	entry, err := trash.Stash(ws.MetaRoot, target)
	if err != nil {
		return err
	}
	r.OK(fmt.Sprintf("Trashed %s -> %s", target, filepath.Join(trash.DirName, entry.ID)))
	// Point the worktree registrations into the trash, so a `git worktree
	// prune` on the meta repo before the restore does not drop them.
	if _, err := worktree.RepairCopy(ctx, ws.MetaRoot, entry.CopyPath, cfg.WorktreePolicy()); err != nil {
		r.Warn(fmt.Sprintf("Could not re-point the worktrees of %s at the trash: %v. They restore intact unless `mws trash prune|empty` or `mws wt prune` runs before `mws restore %s`", name, err, entry.Name))
	}
	if policy.Retention > 0 {
		r.Info(fmt.Sprintf("Restore within %s with `mws restore %s`.", humanDuration(policy.Retention), entry.Name))
	} else {
		r.Info(fmt.Sprintf("Kept until purged. Restore with `mws restore %s`.", entry.Name))
	}
	return nil
}

// notACopy is the not-found error for `mws rm name` when no working copy has
// that name. A retried rm finds the copy in the trash; naming the entry and
// its age lets the caller tell that apart from a typo.
func notACopy(metaRoot, name, target string) error {
	trashed, err := trash.Find(metaRoot, name)
	if err != nil {
		return clierr.Failuref("%s is not a working copy of %s, and the trash could not be read to say whether it was removed: %w", target, metaRoot, err)
	}
	if len(trashed) > 0 {
		return clierr.NotFoundf("%s is not a working copy of %s; a copy of that name was trashed %s ago (%s), so a previous rm already removed it. Restore it with `mws restore %s`",
			target, metaRoot, humanDuration(time.Since(trashed[0].DeletedAt)), trashed[0].ID, name)
	}
	return clierr.NotFoundf("%s is not a working copy of %s; `mws ls` shows the working copies", target, metaRoot)
}

// releaseBeforeTrash puts the working copy at copyPath back on the default
// ports before rm trashes it: it drops the copy's Port lease and renders its
// env files at slot 0, so the trash only holds copies on the default ports
// and restore needs no port logic (ADR-0015). It runs no setup. Every failure
// is a warning; rm trashes the copy anyway.
func releaseBeforeTrash(r Reporter, metaRoot string, cfg *config.Config, copyPath string) {
	name := filepath.Base(copyPath)
	slot, err := releaseLease(metaRoot, copyPath)
	if err != nil {
		r.Warn(fmt.Sprintf("%s: %v; trashing it anyway with its lease. After `mws restore %s`, fix or delete %s and run `mws env sync %s`",
			name, err, name, ports.LeaseFile, name))
		return
	}
	if slot == 0 {
		return
	}
	r.OK(fmt.Sprintf("%s: %s -> %s", name, slotLabel(slot), slotLabel(0)))
	res := renderEnv(r, metaRoot, cfg, copyPath, false)
	var stale []string
	for _, f := range res.Files {
		if f.Status != envCopied {
			stale = append(stale, relToMeta(metaRoot, f.To))
		}
	}
	if res.Error != "" {
		stale = append(stale, "its worktrees' carried files")
	}
	if len(stale) > 0 {
		r.Warn(fmt.Sprintf("%s: not every env file was rendered at the default ports, so %s may still hold %s's ports; trashing it anyway. After `mws restore %s`, run `mws env sync %s`",
			name, strings.Join(stale, ", "), slotLabel(slot), name, name))
	}
}

// rmConfirmDescription explains what removing the copy called name does,
// naming its worktrees, which go with it, and the Port slot it gives back.
func rmConfirmDescription(metaRoot, name string, toTrash bool, slot int, wts []worktree.Entry) string {
	contents := "the directory and any native repo clones inside it"
	if len(wts) > 0 {
		names := make([]string, len(wts))
		for i := range wts {
			names[i] = wts[i].Name
		}
		contents = fmt.Sprintf("the directory, any native repo clones inside it, and its %s %s", plural(len(wts), "worktree", "worktrees"), strings.Join(names, ", "))
	}
	if toTrash {
		release := ""
		if slot > 0 {
			release = fmt.Sprintf("It first releases %s and renders the env files at the default ports. ", slotLabel(slot))
		}
		return fmt.Sprintf("%sThis moves %s into %s. Bring it back on the default ports with `mws restore %s`.", release, contents, trash.Root(metaRoot), name)
	}
	if slot > 0 {
		return fmt.Sprintf("This deletes %s, giving back %s. It cannot be undone.", contents, slotLabel(slot))
	}
	return fmt.Sprintf("This deletes %s. It cannot be undone.", contents)
}

// copyWorktrees lists the managed worktrees of the working copy at copyPath.
// A copy without worktrees needs no git, so a meta workspace that is not a
// git repository still works.
func copyWorktrees(ctx context.Context, r Reporter, metaRoot string, cfg *config.Config, copyPath string) ([]worktree.Entry, error) {
	roots, err := worktree.Roots(copyPath, cfg.WorktreePolicy())
	if err != nil {
		return nil, fmt.Errorf("find worktrees of %s: %w", copyPath, err)
	}
	if len(roots) == 0 {
		return nil, nil
	}
	entries, err := worktree.ListCopies(ctx, r, metaRoot, cfg, []string{copyPath})
	if err != nil {
		return nil, err
	}
	var managed []worktree.Entry
	for i := range entries {
		if entries[i].State == worktree.Managed {
			managed = append(managed, entries[i])
		}
	}
	return managed, nil
}

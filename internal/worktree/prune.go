package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
)

// PruneOptions describes one `mws wt prune`.
type PruneOptions struct {
	Workspace *project.Workspace
	Config    *config.Config
	// DryRun reports what prune would do and changes nothing.
	DryRun bool
}

// Kept is a managed worktree prune left in place, with every reason why.
type Kept struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Reasons []string `json:"reasons"`
}

// PruneResult reports one prune. Under DryRun every list says what would
// happen.
type PruneResult struct {
	DryRun bool `json:"dry_run"`
	// Repaired holds marked roots whose stale registrations were reconnected.
	Repaired []string `json:"repaired"`
	// Dropped holds meta registrations whose directories were gone.
	Dropped []string  `json:"dropped"`
	Removed []Removed `json:"removed"`
	Kept    []Kept    `json:"kept"`
}

// Prune repairs stale anchors, drops registrations of vanished worktrees,
// then removes every managed worktree of the copy that is clean, idle and
// fully merged (no detached inner repo, as RemoveOptions.OnlyIfMerged),
// keeping the one the caller runs in.
func Prune(ctx context.Context, r Reporter, opts PruneOptions) (PruneResult, error) {
	ws := opts.Workspace
	res := PruneResult{DryRun: opts.DryRun, Repaired: []string{}, Dropped: []string{}, Removed: []Removed{}, Kept: []Kept{}}
	if ws.WorkingCopy == "" {
		return res, usagef("not inside a working copy: run mws worktree prune from a working copy (or a worktree of one) under %s", ws.MetaRoot)
	}
	stale, orphans, err := staleAndOrphans(ctx, ws.MetaRoot, ws.WorkingCopy, opts.Config.WorktreePolicy())
	if err != nil {
		return res, failuref("inspect worktree registrations of %s: %w", ws.WorkingCopy, err)
	}
	// Repair first: prune would drop the registration of a moved root.
	for _, root := range stale {
		if !opts.DryRun {
			if err := repair(ctx, ws.MetaRoot, ws.WorkingCopy, root); err != nil {
				return res, failuref("repair worktree %s: %w; nothing was removed", root, err)
			}
			r.OK(fmt.Sprintf("Repaired %s", root))
		}
		res.Repaired = append(res.Repaired, root)
	}
	res.Dropped = append(res.Dropped, orphans...)
	if !opts.DryRun {
		if err := pruneRegistrations(ctx, ws.MetaRoot, ws.WorkingCopy); err != nil {
			return res, failuref("git worktree prune: %w", err)
		}
	}

	entries, err := List(ctx, r, ListOptions{Workspace: ws, Config: opts.Config})
	if err != nil {
		return res, err
	}
	for i := range entries {
		e := &entries[i]
		if e.State != Managed {
			continue
		}
		if reasons := keepReasons(e, ws.Worktree); len(reasons) > 0 {
			res.Kept = append(res.Kept, Kept{Name: e.Name, Path: e.Path, Reasons: reasons})
			continue
		}
		if opts.DryRun {
			res.Removed = append(res.Removed, Removed{Name: e.Name, Path: e.Path, Branches: []BranchOutcome{}})
			continue
		}
		removed, err := Remove(ctx, r, RemoveOptions{Workspace: ws, Config: opts.Config, Names: []string{e.Name}, OnlyIfMerged: true})
		switch {
		case err != nil && clierr.KindOf(err) == clierr.Refused:
			// Changed since it was listed, e.g. an agent started or committed inside it.
			res.Kept = append(res.Kept, Kept{Name: e.Name, Path: e.Path, Reasons: []string{err.Error()}})
		case err != nil:
			return res, err
		default:
			res.Removed = append(res.Removed, removed...)
		}
	}
	return res, nil
}

// keepReasons lists why prune must not remove e; empty means it may.
func keepReasons(e *Entry, callerWorktree string) []string {
	var reasons []string
	if e.Path == callerWorktree {
		reasons = append(reasons, "prune is running inside it")
	}
	var dirty []string
	for _, r := range e.Repos {
		if r.Dirty {
			dirty = append(dirty, r.Folder)
		}
	}
	if len(dirty) > 0 {
		reasons = append(reasons, "modified or untracked files in "+strings.Join(dirty, ", "))
	}
	if len(e.RootFiles) > 0 {
		reasons = append(reasons, "untracked or changed files at the root: "+strings.Join(e.RootFiles, ", "))
	}
	if len(e.Live) > 0 {
		reasons = append(reasons, "in use by process(es) "+e.Live.Join(", "))
	}
	for _, r := range e.Repos {
		if reason, ok := unmergedRepo(r.Folder, r.Branch, r.UniqueCommits); ok {
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

// staleAndOrphans finds marked roots of the copy the meta repo does not
// register (stale anchors) and registrations whose directory is gone.
func staleAndOrphans(ctx context.Context, metaRoot, copyPath string, policy config.WorktreePolicy) (stale, orphans []string, err error) {
	wts, err := git.Worktrees(ctx, metaRoot)
	if err != nil {
		return nil, nil, err
	}
	s, err := scanCopy(copyPath, policy, wts)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range s.dirs {
		if !d.marked || d.registered {
			continue
		}
		if _, err := os.Lstat(filepath.Join(d.root, ".git")); err == nil {
			stale = append(stale, d.root)
		}
	}
	return stale, s.orphans, nil
}

// pruneRegistrations runs `git worktree prune` in the meta repo and in every
// native repo of the copy.
func pruneRegistrations(ctx context.Context, metaRoot, copyPath string) error {
	if err := git.PruneWorktrees(ctx, metaRoot); err != nil {
		return fmt.Errorf("meta repo: %w", err)
	}
	folders, err := nativeRepos(copyPath)
	if err != nil {
		return err
	}
	for _, folder := range folders {
		if err := git.PruneWorktrees(ctx, filepath.Join(copyPath, folder)); err != nil {
			return fmt.Errorf("%s: %w", folder, err)
		}
	}
	return nil
}

package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
// fully merged, keeping the one the caller runs in.
func Prune(ctx context.Context, r Reporter, opts PruneOptions) (PruneResult, error) {
	ws := opts.Workspace
	res := PruneResult{DryRun: opts.DryRun, Repaired: []string{}, Dropped: []string{}, Removed: []Removed{}, Kept: []Kept{}}
	if ws.WorkingCopy == "" {
		return res, usagef("not inside a working copy: run mws worktree prune from a working copy (or a worktree of one) under %s", ws.MetaRoot)
	}
	policy := opts.Config.WorktreePolicy()
	wtDir := filepath.Join(ws.WorkingCopy, policy.Dir)

	stale, orphans, err := staleAndOrphans(ctx, ws.MetaRoot, wtDir)
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
		removed, err := Remove(ctx, r, RemoveOptions{Workspace: ws, Config: opts.Config, Names: []string{e.Name}})
		var we *Error
		switch {
		case errors.As(err, &we) && we.Kind == Refused:
			// Changed since it was listed, e.g. an agent started inside it.
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
	if len(e.Live) > 0 {
		reasons = append(reasons, "in use by process(es) "+joinInts(e.Live))
	}
	for _, r := range e.Repos {
		if r.UniqueCommits > 0 {
			reasons = append(reasons, fmt.Sprintf("%s:%s has %s", r.Folder, r.Branch, unmergedReason(r.UniqueCommits)))
		}
	}
	return reasons
}

// staleAndOrphans finds marked roots in wtDir the meta repo does not
// register (stale anchors) and registrations in wtDir whose directory is gone.
func staleAndOrphans(ctx context.Context, metaRoot, wtDir string) (stale, orphans []string, err error) {
	wts, err := git.Worktrees(ctx, metaRoot)
	if err != nil {
		return nil, nil, err
	}
	registered := map[string]bool{}
	for _, w := range wts {
		if filepath.Dir(w.Path) != wtDir {
			continue
		}
		registered[w.Path] = true
		if _, err := os.Lstat(w.Path); errors.Is(err, os.ErrNotExist) {
			orphans = append(orphans, w.Path)
		}
	}
	dirs, err := os.ReadDir(wtDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	for _, d := range dirs {
		root := filepath.Join(wtDir, d.Name())
		if !d.IsDir() || registered[root] || !hasMarker(root) {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, ".git")); err == nil {
			stale = append(stale, root)
		}
	}
	sort.Strings(orphans)
	return stale, orphans, nil
}

// repair reconnects the anchor and every inner worktree of root with the
// repos that own them.
func repair(ctx context.Context, metaRoot, copyPath, root string) error {
	if err := git.RepairWorktree(ctx, metaRoot, root); err != nil {
		return err
	}
	folders, err := nativeRepos(root)
	if err != nil {
		return err
	}
	for _, folder := range folders {
		if err := git.RepairWorktree(ctx, filepath.Join(copyPath, folder), filepath.Join(root, folder)); err != nil {
			return fmt.Errorf("%s: %w", folder, err)
		}
	}
	return nil
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

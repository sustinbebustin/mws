package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
)

// innerRepo is the live state of one native repo's worktree inside a root.
type innerRepo struct {
	folder string
	src    string // the copy's checkout, which owns the registration
	dir    string
	branch string // "" when detached
	dirty  bool
}

// readInnerRepos inspects every native repo worktree inside root.
func readInnerRepos(ctx context.Context, copyPath, root string) ([]innerRepo, error) {
	folders, err := nativeRepos(root)
	if err != nil {
		return nil, fmt.Errorf("list native repos in %s: %w", root, err)
	}
	repos := make([]innerRepo, 0, len(folders))
	for _, folder := range folders {
		ir := innerRepo{folder: folder, src: filepath.Join(copyPath, folder), dir: filepath.Join(root, folder)}
		if ir.branch, err = git.CurrentBranch(ctx, ir.dir); err != nil {
			return nil, fmt.Errorf("%s: read branch: %w", folder, err)
		}
		if ir.branch == "HEAD" {
			ir.branch = ""
		}
		if ir.dirty, err = git.IsDirty(ctx, ir.dir); err != nil {
			return nil, fmt.Errorf("%s: read status: %w", folder, err)
		}
		repos = append(repos, ir)
	}
	return repos, nil
}

// Resolve returns the root of the managed worktree called name in the
// workspace's copy: a failure when nothing is there, refused when what is
// there is not marked and registered.
func Resolve(ctx context.Context, ws *project.Workspace, policy config.WorktreePolicy, name string) (string, error) {
	if ws.WorkingCopy == "" {
		return "", usagef("not inside a working copy: run mws worktree from a working copy (or a worktree of one) under %s", ws.MetaRoot)
	}
	if err := project.ValidateName(name); err != nil {
		return "", usagef("invalid worktree name %q: %w", name, err)
	}
	root := Path(ws.WorkingCopy, policy, name)
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return "", failuref("no worktree named %s in working copy %s (nothing at %s); `mws wt ls` lists the worktrees", name, ws.WorkingCopy, root)
	}
	managed, err := IsManaged(ctx, ws.MetaRoot, root)
	if err != nil {
		return "", failuref("check worktree %s: %w", root, err)
	}
	if !managed {
		return "", refusedf("%s is not a worktree mws manages (it needs both a %s marker and a registration in the meta repo's `git worktree list`), so mws will not touch it; nothing was changed; inspect it and remove it by hand if it is yours, or run `mws wt prune` to repair a moved worktree", root, project.WorktreeMarkerFileName)
	}
	return root, nil
}

// RemoveOptions describes one `mws wt rm`.
type RemoveOptions struct {
	Workspace *project.Workspace
	Config    *config.Config
	Names     []string
	// Force discards modified and untracked files in the inner repos.
	Force bool
	// KeepBranch keeps every branch, merged or not.
	KeepBranch bool
	// OnlyIfMerged refuses a worktree with a detached inner repo or with an
	// inner branch holding a commit no other ref reaches, so its directory
	// survives along with the branch.
	OnlyIfMerged bool
}

// BranchOutcome says what happened to one inner repo's branch.
type BranchOutcome struct {
	Folder  string `json:"folder"`
	Branch  string `json:"branch"`
	Deleted bool   `json:"deleted"`
	// UniqueCommits counts commits no other ref reaches; set when counted.
	UniqueCommits int `json:"unique_commits"`
	// Reason says why a branch was kept; empty when deleted.
	Reason string `json:"reason,omitempty"`
}

// Removed describes one removed worktree.
type Removed struct {
	Name     string          `json:"name"`
	Path     string          `json:"path"`
	Branches []BranchOutcome `json:"branches"`
}

// target is one worktree that passed every safety check.
type target struct {
	name  string
	root  string
	repos []innerRepo
}

// Remove removes the named worktrees of the workspace's copy. Every worktree
// is checked first -- managed, clean unless forced, no foreign process inside
// -- and nothing is removed if any check refuses. If the process's working
// directory is inside a target, Remove first changes it to the meta root.
func Remove(ctx context.Context, r Reporter, opts RemoveOptions) ([]Removed, error) {
	ws := opts.Workspace
	policy := opts.Config.WorktreePolicy()
	targets := make([]target, 0, len(opts.Names))
	for _, name := range opts.Names {
		root, err := Resolve(ctx, ws, policy, name)
		if err != nil {
			return nil, err
		}
		repos, err := readInnerRepos(ctx, ws.WorkingCopy, root)
		if err != nil {
			return nil, failuref("inspect worktree %s: %w", name, err)
		}
		if !opts.Force {
			if dirty := dirtyFolders(repos); len(dirty) > 0 {
				return nil, refusedf("worktree %s has modified or untracked files in %s; nothing was removed; commit or stash them there, or rerun with --force to discard them", name, strings.Join(dirty, ", "))
			}
		}
		if opts.OnlyIfMerged {
			unmerged, err := unmergedWork(ctx, repos)
			if err != nil {
				return nil, failuref("inspect worktree %s: %w", name, err)
			}
			if len(unmerged) > 0 {
				return nil, refusedf("worktree %s holds work no other ref reaches (%s); nothing was removed; merge or push it, then run `mws wt rm %s`", name, strings.Join(unmerged, "; "), name)
			}
		}
		targets = append(targets, target{name: name, root: root, repos: repos})
	}

	if err := leaveTargets(ws.MetaRoot, targets); err != nil {
		return nil, failuref("leave the worktree before removing it: %w", err)
	}
	if err := refuseLive(r, targets); err != nil {
		return nil, err
	}

	out := make([]Removed, 0, len(targets))
	for _, t := range targets {
		removed, err := removeOne(ctx, ws.MetaRoot, policy, t, opts)
		if err != nil {
			return out, err
		}
		r.OK(fmt.Sprintf("Removed worktree %s", t.name))
		out = append(out, removed)
	}
	return out, nil
}

func dirtyFolders(repos []innerRepo) []string {
	var dirty []string
	for _, ir := range repos {
		if ir.dirty {
			dirty = append(dirty, ir.folder)
		}
	}
	return dirty
}

// unmergedWork describes each inner repo whose commits would survive only on
// its own branch, or that is detached (its reachability is not checked).
func unmergedWork(ctx context.Context, repos []innerRepo) ([]string, error) {
	var out []string
	for _, ir := range repos {
		n := 0
		if ir.branch != "" {
			var err error
			if n, err = git.UniqueCommits(ctx, ir.src, ir.branch); err != nil {
				return nil, fmt.Errorf("%s: count unmerged commits on %s: %w", ir.folder, ir.branch, err)
			}
		}
		if reason, ok := unmergedRepo(ir.folder, ir.branch, n); ok {
			out = append(out, reason)
		}
	}
	return out, nil
}

// unmergedRepo describes one inner repo whose work would be lost with its
// worktree: detached (branch "", reachability not checked) or holding
// uniqueCommits no other ref reaches. ok is false when nothing would be lost.
func unmergedRepo(folder, branch string, uniqueCommits int) (reason string, ok bool) {
	switch {
	case branch == "":
		return folder + ": detached HEAD", true
	case uniqueCommits > 0:
		return fmt.Sprintf("%s:%s has %s", folder, branch, unmergedReason(uniqueCommits)), true
	}
	return "", false
}

// leaveTargets moves the process out of any target so its own cwd neither
// blocks removal nor dangles afterwards.
func leaveTargets(metaRoot string, targets []target) error {
	cwd, err := os.Getwd()
	if err != nil {
		return nil // already in a deleted directory; nothing to leave
	}
	for _, t := range targets {
		if cwd == t.root || strings.HasPrefix(cwd, t.root+string(filepath.Separator)) {
			return os.Chdir(metaRoot)
		}
	}
	return nil
}

// refuseLive refuses when a process outside the caller's own tree has its
// cwd inside a target, and warns when that cannot be checked.
func refuseLive(r Reporter, targets []target) error {
	live := scanProcesses()
	if !live.supported {
		r.Warn("cannot list process working directories on this system, so worktrees were not checked for running processes")
		return nil
	}
	for _, t := range targets {
		if pids := live.in(t.root); len(pids) > 0 {
			return refusedf("worktree %s is in use: process(es) %s have their working directory inside %s; nothing was removed; stop them (or wait for the agent to finish), then retry", t.name, joinInts(pids), t.root)
		}
	}
	return nil
}

func unmergedReason(n int) string {
	if n == 1 {
		return "1 commit not merged anywhere"
	}
	return fmt.Sprintf("%d commits not merged anywhere", n)
}

func joinInts(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = fmt.Sprint(n)
	}
	return strings.Join(s, ", ")
}

// removeOne removes the inner worktrees, settles their branches, then
// removes the anchor and whatever mws-made files remain in the root.
func removeOne(ctx context.Context, metaRoot string, policy config.WorktreePolicy, t target, opts RemoveOptions) (Removed, error) {
	res := Removed{Name: t.name, Path: t.root, Branches: []BranchOutcome{}}
	for _, ir := range t.repos {
		var err error
		if opts.Force {
			err = git.RemoveWorktreeForce(ctx, ir.src, ir.dir)
		} else {
			err = git.RemoveWorktree(ctx, ir.src, ir.dir)
		}
		if err != nil {
			return res, failuref("remove worktree %s: %s: %w\nthe repos listed before %s were already removed; the rest of %s is intact; fix the cause, then rerun `mws wt rm %s`", t.name, ir.folder, err, ir.folder, t.root, t.name)
		}
		if ir.branch == "" {
			continue
		}
		outcome, err := settleBranch(ctx, policy, t.name, ir, opts.KeepBranch)
		if err != nil {
			return res, failuref("remove worktree %s: %s: %w", t.name, ir.folder, err)
		}
		res.Branches = append(res.Branches, outcome)
	}
	if err := git.RemoveWorktreeForce(ctx, metaRoot, t.root); err != nil {
		return res, failuref("remove worktree %s: unregister its root from the meta repo: %w\nits native repo worktrees are already removed", t.name, err)
	}
	if err := os.RemoveAll(t.root); err != nil {
		return res, failuref("remove worktree %s: delete %s: %w", t.name, t.root, err)
	}
	return res, nil
}

// settleBranch deletes the inner repo's branch when mws owns it (it is named
// <prefix><name>) and no commit on it would be lost.
func settleBranch(ctx context.Context, policy config.WorktreePolicy, name string, ir innerRepo, keep bool) (BranchOutcome, error) {
	o := BranchOutcome{Folder: ir.folder, Branch: ir.branch}
	n, err := git.UniqueCommits(ctx, ir.src, ir.branch)
	if err != nil {
		return o, fmt.Errorf("count unmerged commits on %s: %w", ir.branch, err)
	}
	o.UniqueCommits = n
	switch {
	case keep:
		o.Reason = "--keep-branch"
	case ir.branch != policy.BranchPrefix+name:
		o.Reason = fmt.Sprintf("not named %s%s, so mws does not own it", policy.BranchPrefix, name)
	case n > 0:
		o.Reason = unmergedReason(n)
	default:
		if err := git.DeleteBranch(ctx, ir.src, ir.branch); err != nil {
			return o, err
		}
		o.Deleted = true
	}
	return o, nil
}

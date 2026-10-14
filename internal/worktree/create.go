package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
)

// NewOptions describes one `mws wt new`.
type NewOptions struct {
	Workspace *project.Workspace
	Config    *config.Config
	Name      string
	// Branch overrides <prefix><name> as the branch in every native repo.
	Branch string
	// Base overrides the launching checkout's HEAD as the start point.
	Base    string
	Version string
}

// RepoResult describes one native repo's worktree.
type RepoResult struct {
	Folder string `json:"folder"`
	Path   string `json:"path"`
	// Branch is "" when the checkout is detached.
	Branch string `json:"branch"`
	// Base is the commit the worktree started from: the resolved base for a
	// new branch, the branch tip for a reused one, empty for a reused worktree.
	Base string `json:"base,omitempty"`
}

// Result describes the worktree New created or found.
type Result struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Copy string `json:"copy"`
	// Branch is the branch every native repo is on: the requested one for a
	// new worktree; for a reused one, what its repos are on, or "" when they
	// differ or are detached (Repos has each).
	Branch string       `json:"branch"`
	Reused bool         `json:"reused"`
	Repos  []RepoResult `json:"repos"`
}

// plannedRepo is a native repo of the copy, checked and ready to branch.
type plannedRepo struct {
	folder string
	src    string // the copy's checkout; owns the git object store
	base   string // commit id
	reuse  bool   // the branch exists and is attached nowhere
}

// New creates the worktree described by opts, or returns the existing one
// when a managed worktree of that name is already present. Everything it
// creates is removed again if it fails before the marker is written.
func New(ctx context.Context, r Reporter, opts NewOptions) (Result, error) {
	ws := opts.Workspace
	if ws.WorkingCopy == "" {
		return Result{}, usagef("not inside a working copy: run mws worktree from a working copy (or a worktree of one) under %s", ws.MetaRoot)
	}
	if err := project.ValidateName(opts.Name); err != nil {
		return Result{}, usagef("invalid worktree name %q: %w", opts.Name, err)
	}
	policy := opts.Config.WorktreePolicy()
	branch := opts.Branch
	if branch == "" {
		branch = policy.BranchPrefix + opts.Name
	}
	if err := git.CheckBranchName(ctx, branch); errors.Is(err, git.ErrInvalidBranchName) {
		return Result{}, &clierr.Error{Kind: clierr.Usage, Err: err}
	} else if err != nil {
		return Result{}, failuref("check branch name %q: %w", branch, err)
	}

	copyPath := ws.WorkingCopy
	target := Path(copyPath, policy, opts.Name)
	res := Result{Name: opts.Name, Path: target, Copy: copyPath, Branch: branch}

	if _, err := os.Lstat(target); err == nil {
		return reuse(ctx, ws.MetaRoot, res)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, failuref("inspect %s: %w", target, err)
	}

	launch := ws.Worktree
	if launch == "" {
		launch = copyPath
	}
	plan, err := planRepos(ctx, copyPath, launch, branch, opts.Base)
	if err != nil {
		return Result{}, err
	}

	b := &builder{meta: ws.MetaRoot, target: target}
	repos, err := b.build(ctx, r, opts, plan, branch)
	if err != nil {
		if rbErr := b.rollback(ctx); rbErr != nil {
			return Result{}, failuref("create worktree %s failed: %w\nrollback was incomplete, so some of it may remain: %v\ninspect with `git worktree list` in the meta root and each native repo", opts.Name, err, rbErr)
		}
		return Result{}, wrapKind(err, "create worktree %s failed: %w\neverything it created was removed; the working copy is unchanged", opts.Name, err)
	}
	res.Repos = repos
	return res, nil
}

// reuse returns the existing worktree at res.Path when mws manages it, and
// refuses to touch anything else found there.
func reuse(ctx context.Context, metaRoot string, res Result) (Result, error) {
	managed, err := IsManaged(ctx, metaRoot, res.Path)
	if err != nil {
		return Result{}, failuref("check worktree %s: %w", res.Path, err)
	}
	if !managed {
		return Result{}, refusedf("%s already exists but is not a worktree mws manages (it needs both a %s marker and a registration in the meta repo's `git worktree list`); nothing was changed; pick another name, or move that directory away", res.Path, project.WorktreeMarkerFileName)
	}
	repos, err := nativeRepos(res.Path)
	if err != nil {
		return Result{}, failuref("read worktree %s: %w", res.Path, err)
	}
	res.Reused = true
	res.Repos = make([]RepoResult, 0, len(repos))
	for i, folder := range repos {
		dir := filepath.Join(res.Path, folder)
		br, err := git.CurrentBranch(ctx, dir)
		if err != nil {
			return Result{}, failuref("read branch of %s: %w", dir, err)
		}
		if br == "HEAD" {
			br = ""
		}
		if i == 0 {
			res.Branch = br
		} else if br != res.Branch {
			res.Branch = ""
		}
		res.Repos = append(res.Repos, RepoResult{Folder: folder, Path: dir, Branch: br})
	}
	return res, nil
}

// planRepos checks every native repo of the copy before anything is created:
// the base must resolve, the checkout must not be mid-operation, and the
// branch must not be checked out anywhere.
func planRepos(ctx context.Context, copyPath, launch, branch, base string) ([]plannedRepo, error) {
	folders, err := nativeRepos(copyPath)
	if err != nil {
		return nil, failuref("list native repos in %s: %w", copyPath, err)
	}
	if len(folders) == 0 {
		return nil, usagef("%s holds no native repos (no child directory has a .git entry), so there is nothing to make a worktree of", copyPath)
	}
	plan := make([]plannedRepo, 0, len(folders))
	for _, folder := range folders {
		p := plannedRepo{folder: folder, src: filepath.Join(copyPath, folder)}
		// A nested launch bases on the launching worktree's own checkout; a
		// repo it lacks falls back to the copy's.
		from := filepath.Join(launch, folder)
		if _, err := os.Stat(filepath.Join(from, ".git")); err != nil {
			from = p.src
		}
		op, err := git.OperationInProgress(ctx, from)
		if err != nil {
			return nil, failuref("%s: inspect checkout state: %w", folder, err)
		}
		if op != "" {
			return nil, refusedf("%s: %s is in the middle of a %s, so its HEAD is not a stable base; nothing was created; finish or abort the %s there, then retry", folder, from, op, op)
		}
		if holder, err := branchHolder(ctx, p.src, branch, ""); err != nil {
			return nil, failuref("%s: list worktrees: %w", folder, err)
		} else if holder != "" {
			return nil, refusedf("%s: branch %s is already checked out at %s, and git allows a branch in one worktree only; nothing was created; use another name or --branch, or switch that checkout off %s", folder, branch, holder, branch)
		}
		if p.reuse, err = git.BranchExists(ctx, p.src, branch); err != nil {
			return nil, failuref("%s: look up branch %s: %w", folder, branch, err)
		}
		if p.reuse {
			if p.base, err = git.ResolveCommit(ctx, p.src, branch); err != nil {
				return nil, failuref("%s: %w", folder, err)
			}
		} else {
			ref := base
			if ref == "" {
				ref = "HEAD"
			}
			p.base, err = git.ResolveCommit(ctx, from, ref)
			switch {
			case err == nil:
			case base != "" && errors.Is(err, git.ErrNotFound):
				return nil, usagef("%s: --base %w; nothing was created", folder, err)
			default:
				return nil, failuref("%s: resolve %s: %w", folder, ref, err)
			}
		}
		plan = append(plan, p)
	}
	return plan, nil
}

// branchHolder returns the path of a worktree of repoDir, other than except,
// that has branch checked out, or "" when there is none.
func branchHolder(ctx context.Context, repoDir, branch, except string) (string, error) {
	holders, err := git.Worktrees(ctx, repoDir)
	if err != nil {
		return "", err
	}
	for _, h := range holders {
		if h.Branch == branch && h.Path != except {
			return h.Path, nil
		}
	}
	return "", nil
}

// builder creates a worktree step by step and remembers every step so
// rollback can undo exactly what was done.
type builder struct {
	meta   string
	target string
	undo   []func(context.Context) error
}

func (b *builder) onRollback(fn func(context.Context) error) { b.undo = append(b.undo, fn) }

// rollback runs the recorded undo steps newest first and joins their errors.
// It ignores cancellation: an interrupted create must still clean up.
func (b *builder) rollback(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx)
	var errs []error
	for i := len(b.undo) - 1; i >= 0; i-- {
		errs = append(errs, b.undo[i](ctx))
	}
	return errors.Join(errs...)
}

func (b *builder) build(ctx context.Context, r Reporter, opts NewOptions, plan []plannedRepo, branch string) ([]RepoResult, error) {
	parent := filepath.Dir(b.target)
	if _, err := os.Stat(parent); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(parent, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, failuref("create %s: %w", parent, err)
		}
		// Remove only if still empty: a concurrent `new` may share it.
		b.onRollback(func(context.Context) error {
			if err := os.Remove(parent); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
				return fmt.Errorf("remove %s: %w", parent, err)
			}
			return nil
		})
	}

	// Claim the name: a concurrent `new` of the same name fails here, before
	// this run registers a rollback step that would delete its directory.
	if err := os.Mkdir(b.target, 0o755); errors.Is(err, os.ErrExist) {
		return nil, refusedf("%s appeared while this worktree was being created, probably from a concurrent `mws wt new %s`; nothing was changed; retry to reuse it", b.target, opts.Name)
	} else if err != nil {
		return nil, failuref("create %s: %w", b.target, err)
	}
	b.onRollback(func(context.Context) error { return os.RemoveAll(b.target) })

	if err := git.AddDetachedNoCheckout(ctx, b.meta, b.target); err != nil {
		return nil, failuref("anchor %s to the meta repo at %s (it must be a git repository with at least one commit): %w", b.target, b.meta, err)
	}
	b.onRollback(func(ctx context.Context) error {
		return git.RemoveWorktreeForce(ctx, b.meta, b.target)
	})
	if err := git.SparseCheckoutOnly(ctx, b.target, "/.gitignore"); err != nil {
		return nil, failuref("sparse-checkout the anchor at %s: %w", b.target, err)
	}
	r.OK("Anchored root to the meta repo (detached, sparse)")

	repos := make([]RepoResult, 0, len(plan))
	for _, p := range plan {
		dst := filepath.Join(b.target, p.folder)
		var err error
		if p.reuse {
			err = git.AddWorktree(ctx, p.src, dst, branch)
		} else {
			// Registered before the attempt: a failing checkout hook leaves the
			// branch and registration behind even though git reports failure.
			// A branch checked out elsewhere is another run's, never ours.
			b.onRollback(func(ctx context.Context) error {
				exists, err := git.BranchExists(ctx, p.src, branch)
				if err != nil || !exists {
					return err
				}
				if holder, err := branchHolder(ctx, p.src, branch, dst); err != nil || holder != "" {
					return err
				}
				return git.DeleteBranch(ctx, p.src, branch)
			})
			err = git.AddWorktreeNewBranch(ctx, p.src, dst, branch, p.base)
		}
		b.onRollback(func(ctx context.Context) error {
			if _, statErr := os.Lstat(dst); errors.Is(statErr, os.ErrNotExist) {
				return nil
			}
			return git.RemoveWorktreeForce(ctx, p.src, dst)
		})
		if err != nil {
			return nil, failuref("%s: add worktree on %s: %w", p.folder, branch, err)
		}
		verb := "branched"
		if p.reuse {
			verb = "reused branch"
		}
		r.OK(fmt.Sprintf("%s: %s %s at %s", p.folder, verb, branch, shortID(p.base)))
		repos = append(repos, RepoResult{Folder: p.folder, Path: dst, Branch: branch, Base: p.base})
	}

	copyPath := opts.Workspace.WorkingCopy
	policy := opts.Config.WorktreePolicy()
	copied, err := carryRoot(r, opts.Workspace.MetaRoot, copyPath, b.target, policy.Dir)
	if err != nil {
		return nil, failuref("carry working-copy state into %s: %w", b.target, err)
	}
	if err := carryEnvs(r, opts.Config, copyPath, b.target, plan); err != nil {
		return nil, failuref("carry env files into %s: %w", b.target, err)
	}

	if err := writeMarker(b.target, Marker{Copy: copyPath, Name: opts.Name, Created: time.Now().UTC().Truncate(time.Second), MWSVersion: opts.Version, Copied: copied}); err != nil {
		return nil, failuref("write marker: %w", err)
	}
	return repos, nil
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

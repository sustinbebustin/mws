// Package worktree creates and resolves worktrees: children of a working copy
// at <copy>/<worktrees dir>/<name>/ holding one git worktree per native repo,
// rooted in a detached, sparse linked worktree of the meta workspace repo.
package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/sustinbebustin/mws/internal/atomicfile"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
)

// Kind classifies a worktree error by how a caller should react to it.
type Kind int

const (
	// Failure is an unexpected git or filesystem error.
	Failure Kind = iota
	// Usage is a bad name, flag, ref or invocation context.
	Usage
	// Refused means a safety invariant blocked the operation.
	Refused
)

// ExitCode is the process exit code for k: 1 failure, 2 usage, 3 refused.
func (k Kind) ExitCode() int {
	switch k {
	case Usage:
		return 2
	case Refused:
		return 3
	default:
		return 1
	}
}

// Code names k in machine-readable output: "failure", "usage" or "refused".
func (k Kind) Code() string {
	switch k {
	case Usage:
		return "usage"
	case Refused:
		return "refused"
	default:
		return "failure"
	}
}

// Error is a worktree operation failure tagged with its Kind.
type Error struct {
	Kind Kind
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// ExitCode is the process exit code for the error's Kind.
func (e *Error) ExitCode() int { return e.Kind.ExitCode() }

func usagef(format string, args ...any) error {
	return &Error{Kind: Usage, Err: fmt.Errorf(format, args...)}
}

func refusedf(format string, args ...any) error {
	return &Error{Kind: Refused, Err: fmt.Errorf(format, args...)}
}

func failuref(format string, args ...any) error {
	return &Error{Kind: Failure, Err: fmt.Errorf(format, args...)}
}

// Reporter receives progress lines while a worktree is built.
type Reporter interface {
	OK(string)
	Warn(string)
}

// Marker is the content of a worktree's marker file. Branches are not
// recorded; they are read live from git.
type Marker struct {
	Copy       string    `toml:"copy"`
	Name       string    `toml:"name"`
	Created    time.Time `toml:"created"`
	MWSVersion string    `toml:"mws_version"`
}

// Path returns where the worktree called name of copyPath lives.
func Path(copyPath string, policy config.WorktreePolicy, name string) string {
	return filepath.Join(copyPath, policy.Dir, name)
}

// IsManaged reports whether root is both marked and registered as a linked
// worktree of the meta repo -- the only shape mws treats as its own.
func IsManaged(ctx context.Context, metaRoot, root string) (bool, error) {
	if !hasMarker(root) {
		return false, nil
	}
	list, err := git.Worktrees(ctx, metaRoot)
	if err != nil {
		return false, err
	}
	for _, w := range list {
		if w.Path == root {
			return true, nil
		}
	}
	return false, nil
}

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
	Branch string `json:"branch"`
	// Base is the commit the worktree started from: the resolved base for a
	// new branch, the branch tip for a reused one, empty for a reused worktree.
	Base string `json:"base,omitempty"`
}

// Result describes the worktree New created or found.
type Result struct {
	Name   string       `json:"name"`
	Path   string       `json:"path"`
	Copy   string       `json:"copy"`
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
	if err := git.CheckBranchName(ctx, branch); err != nil {
		return Result{}, &Error{Kind: Usage, Err: err}
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

	b := &builder{ctx: ctx, meta: ws.MetaRoot, target: target}
	repos, err := b.build(r, opts, plan, branch)
	if err != nil {
		if rbErr := b.rollback(); rbErr != nil {
			return Result{}, failuref("create worktree %s failed: %w\nrollback was incomplete, so some of it may remain: %v\ninspect with `git worktree list` in the meta root and each native repo", opts.Name, err, rbErr)
		}
		return Result{}, wrapKind(err, "create worktree %s failed: %w\neverything it created was removed; the working copy is unchanged", opts.Name, err)
	}
	res.Repos = repos
	return res, nil
}

// wrapKind rewords err while keeping its Kind (Failure when untagged).
func wrapKind(err error, format string, args ...any) error {
	kind := Failure
	var we *Error
	if errors.As(err, &we) {
		kind = we.Kind
	}
	return &Error{Kind: kind, Err: fmt.Errorf(format, args...)}
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
	for _, folder := range repos {
		dir := filepath.Join(res.Path, folder)
		br, err := git.CurrentBranch(ctx, dir)
		if err != nil {
			return Result{}, failuref("read branch of %s: %w", dir, err)
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
		holders, err := git.Worktrees(ctx, p.src)
		if err != nil {
			return nil, failuref("%s: list worktrees: %w", folder, err)
		}
		for _, h := range holders {
			if h.Branch == branch {
				return nil, refusedf("%s: branch %s is already checked out at %s, and git allows a branch in one worktree only; nothing was created; use another name or --branch, or switch that checkout off %s", folder, branch, h.Path, branch)
			}
		}
		if git.BranchExists(ctx, p.src, branch) {
			p.reuse = true
			if p.base, err = git.ResolveCommit(ctx, p.src, branch); err != nil {
				return nil, failuref("%s: %w", folder, err)
			}
		} else {
			ref := base
			if ref == "" {
				ref = "HEAD"
			}
			if p.base, err = git.ResolveCommit(ctx, from, ref); err != nil {
				if base != "" {
					return nil, usagef("%s: --base %w; nothing was created", folder, err)
				}
				return nil, failuref("%s: resolve HEAD: %w", folder, err)
			}
		}
		plan = append(plan, p)
	}
	return plan, nil
}

// nativeRepos lists the direct child directories of dir holding a .git entry.
func nativeRepos(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Lstat(filepath.Join(dir, e.Name(), ".git")); err == nil {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// builder creates a worktree step by step and remembers every step so
// rollback can undo exactly what was done.
type builder struct {
	ctx    context.Context
	meta   string
	target string
	undo   []func(context.Context) error
}

func (b *builder) onRollback(fn func(context.Context) error) { b.undo = append(b.undo, fn) }

// rollback runs the recorded undo steps newest first and joins their errors.
// It ignores cancellation: an interrupted create must still clean up.
func (b *builder) rollback() error {
	ctx := context.WithoutCancel(b.ctx)
	var errs []error
	for i := len(b.undo) - 1; i >= 0; i-- {
		errs = append(errs, b.undo[i](ctx))
	}
	return errors.Join(errs...)
}

func (b *builder) build(r Reporter, opts NewOptions, plan []plannedRepo, branch string) ([]RepoResult, error) {
	parent := filepath.Dir(b.target)
	if _, err := os.Stat(parent); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(parent, 0o755); err != nil {
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

	if err := git.AddDetachedNoCheckout(b.ctx, b.meta, b.target); err != nil {
		// git may have created the directory before failing.
		b.onRollback(func(context.Context) error { return os.RemoveAll(b.target) })
		return nil, failuref("anchor %s to the meta repo at %s (it must be a git repository with at least one commit): %w", b.target, b.meta, err)
	}
	b.onRollback(func(ctx context.Context) error {
		err := git.RemoveWorktreeForce(ctx, b.meta, b.target)
		return errors.Join(err, os.RemoveAll(b.target))
	})
	if err := git.SparseCheckoutOnly(b.ctx, b.target, "/.gitignore"); err != nil {
		return nil, failuref("sparse-checkout the anchor at %s: %w", b.target, err)
	}
	r.OK("Anchored root to the meta repo (detached, sparse)")

	repos := make([]RepoResult, 0, len(plan))
	for _, p := range plan {
		dst := filepath.Join(b.target, p.folder)
		var err error
		if p.reuse {
			err = git.AddWorktree(b.ctx, p.src, dst, branch)
		} else {
			// Registered before the attempt: a failing checkout hook leaves the
			// branch and registration behind even though git reports failure.
			b.onRollback(func(ctx context.Context) error {
				if !git.BranchExists(ctx, p.src, branch) {
					return nil
				}
				return git.DeleteBranch(ctx, p.src, branch)
			})
			err = git.AddWorktreeNewBranch(b.ctx, p.src, dst, branch, p.base)
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
	if err := carryRoot(r, opts.Workspace.MetaRoot, copyPath, b.target, policy.Dir); err != nil {
		return nil, failuref("carry working-copy state into %s: %w", b.target, err)
	}
	if err := carryEnvs(r, opts.Config, copyPath, b.target, plan); err != nil {
		return nil, failuref("carry env files into %s: %w", b.target, err)
	}

	if err := writeMarker(b.target, Marker{Copy: copyPath, Name: opts.Name, Created: time.Now().UTC().Truncate(time.Second), MWSVersion: opts.Version}); err != nil {
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

// carryRoot fans the harness out into the worktree root, then mirrors the
// copy root: regular files are copied, non-repo directories (except the
// worktrees dir) are linked. A copy-root entry shadows a harness entry of the
// same name, as it does in the copy; the anchor's checked-out .gitignore is
// never replaced.
func carryRoot(r Reporter, metaRoot, copyPath, target, worktreesDir string) error {
	return mirrorRoot(r, metaRoot, copyPath, target, worktreesDir, true)
}

// Relink refreshes the links of the worktree at root: harness entries and the
// copy-root directories it mirrors, with copy-root entries still shadowing
// harness ones. Files copied into the root at creation are left alone; they
// are the worktree's own from then on.
func Relink(metaRoot, copyPath, root string, policy config.WorktreePolicy) error {
	return mirrorRoot(silent{}, metaRoot, copyPath, root, policy.Dir, false)
}

// silent discards progress lines.
type silent struct{}

func (silent) OK(string)   {}
func (silent) Warn(string) {}

// mirrorRoot is carryRoot, with copying the copy root's regular files
// optional so a relink never overwrites the worktree's own copies.
func mirrorRoot(r Reporter, metaRoot, copyPath, target, worktreesDir string, copyFiles bool) error {
	if _, err := project.LinkHarnessIntoWorkingCopy(metaRoot, target); err != nil {
		return err
	}
	r.OK("Linked harness")
	entries, err := os.ReadDir(copyPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		src := filepath.Join(copyPath, name)
		dst := filepath.Join(target, name)
		if name == worktreesDir {
			continue
		}
		st, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() && !st.IsDir() {
			continue // harness symlinks were linked above
		}
		if st.IsDir() {
			if _, err := os.Lstat(filepath.Join(src, ".git")); err == nil {
				continue // native repo: has its own worktree
			}
		}
		if dst, err := os.Lstat(dst); err == nil && dst.Mode()&os.ModeSymlink == 0 {
			continue // the anchor's checkout or a native repo worktree
		}
		if st.IsDir() {
			rel, err := filepath.Rel(target, src)
			if err != nil {
				return err
			}
			if err := project.AtomicSymlink(rel, dst); err != nil {
				return err
			}
			r.OK(fmt.Sprintf("Linked %s", name))
			continue
		}
		if !copyFiles {
			continue
		}
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := atomicfile.Copy(src, dst); err != nil {
			return err
		}
		r.OK(fmt.Sprintf("Copied %s", name))
	}
	return nil
}

// carryEnvs copies each configured env target from the copy's own repo, which
// already holds this copy's values, into the matching inner worktree.
func carryEnvs(r Reporter, cfg *config.Config, copyPath, target string, plan []plannedRepo) error {
	byFolder := map[string]config.Repo{}
	for _, repo := range append(append([]config.Repo{}, cfg.Repos...), cfg.OptionalRepos...) {
		byFolder[repo.Folder] = repo
	}
	for _, p := range plan {
		for _, env := range byFolder[p.folder].Envs {
			src := filepath.Join(copyPath, p.folder, env.Target)
			if _, err := os.Stat(src); err != nil {
				r.Warn(fmt.Sprintf("%s: env target %s is missing in the working copy, so the worktree has none; run `mws sync-env` for the copy, then copy it in", p.folder, env.Target))
				continue
			}
			if err := atomicfile.Copy(src, filepath.Join(target, p.folder, env.Target)); err != nil {
				return fmt.Errorf("%s: %w", p.folder, err)
			}
			r.OK(fmt.Sprintf("%s: copied env %s", p.folder, env.Target))
		}
	}
	return nil
}

func writeMarker(root string, m Marker) error {
	f, err := os.OpenFile(filepath.Join(root, project.WorktreeMarkerFileName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	encErr := toml.NewEncoder(f).Encode(m)
	return errors.Join(encErr, f.Close())
}

package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
)

func newCloneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clone <name>",
		Short: "Create a new working copy inside this meta workspace",
		Long: `clone creates a new working copy at <meta-root>/<name>/. It clones each
registered native repo into the new working copy (using "git clone --local"
from the invoking working copy when available, falling back to the configured
URL), fans the harness symlinks out from .mws/, and copies any env files
mapped in .mws.toml from env staging into the working copy.

Native repos are checked out on each repo's default branch, not on the
invoking copy's current branch -- a deliberate fresh-start semantic.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runClone(cmd.Context(), newConsoleReporter(), name)
		},
	}
}

func runClone(ctx context.Context, r Reporter, name string) error {
	if name == "" {
		if err := huh.NewInput().
			Title("New working copy name").
			Description("Will be created at <meta-root>/<name>/.").
			Validate(project.ValidateName).
			Value(&name).
			Run(); err != nil {
			return err
		}
		name = strings.TrimSpace(name)
	}
	if err := project.ValidateName(name); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ws, err := project.Locate(cwd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(ws.MetaRoot)
	if err != nil {
		return err
	}

	target := filepath.Join(ws.MetaRoot, name)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("path already exists: %s", target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}

	linked, err := project.LinkHarnessIntoWorkingCopy(ws.MetaRoot, target)
	if err != nil {
		return err
	}
	for _, n := range linked {
		r.OK(fmt.Sprintf("Linked %s/%s", name, n))
	}

	invoker, err := chooseInvokingCopy(ws)
	if err != nil {
		return err
	}

	var failed []string
	for _, repo := range cfg.Repos {
		if err := cloneNative(ctx, r, repo, invoker, target); err != nil {
			r.Fail(fmt.Sprintf("%s: %v", repo.Folder, err))
			failed = append(failed, repo.Folder)
			continue
		}
		copyEnvsFor(r, ws.MetaRoot, repo, target)
	}

	if len(failed) > 0 {
		return fmt.Errorf("clone completed with errors: %d repo(s) failed: %s", len(failed), strings.Join(failed, ", "))
	}
	r.OK(fmt.Sprintf("Working copy ready at %s", target))
	return nil
}

// chooseInvokingCopy picks a working copy whose native repo clones we should prefer for
// `git clone --local`. Order:
//  1. The working copy the user invoked from.
//  2. The first peer alphabetically (so newer clones can bootstrap from any existing copy).
//  3. None: fall back to URL clones.
func chooseInvokingCopy(ws *project.Workspace) (string, error) {
	if ws.WorkingCopy != "" {
		return ws.WorkingCopy, nil
	}
	peers, err := project.EnumerateWorkingCopies(ws.MetaRoot)
	if err != nil {
		return "", err
	}
	if len(peers) == 0 {
		return "", nil
	}
	return peers[0], nil
}

// cloneNative populates target/<folder> with a clone of the repo. Tries
// `git clone --local invoker/<folder>` first, then falls back to URL clone.
// After cloning, checks out the repo's default branch.
func cloneNative(ctx context.Context, r Reporter, repo config.Repo, invoker, target string) error {
	dst := filepath.Join(target, repo.Folder)
	if _, err := os.Stat(dst); err == nil {
		r.Info(fmt.Sprintf("%s: already present, skipping", repo.Folder))
		return nil
	}

	r.Heading(fmt.Sprintf("Cloning %s ...", repo.Folder))

	var lastErr error
	if invoker != "" {
		src := filepath.Join(invoker, repo.Folder)
		if _, err := os.Stat(filepath.Join(src, ".git")); err == nil {
			if err := git.CloneLocal(ctx, src, dst); err != nil {
				lastErr = err
				r.Warn(fmt.Sprintf("%s: --local clone failed (%v), falling back to remote", repo.Folder, err))
			} else {
				return checkoutDefault(ctx, r, dst)
			}
		}
	}

	if repo.URL == "" {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("no local source and no remote URL configured")
	}

	if err := git.Clone(ctx, repo.URL, dst); err != nil {
		return err
	}
	return checkoutDefault(ctx, r, dst)
}

func checkoutDefault(ctx context.Context, r Reporter, repoDir string) error {
	br, err := git.DefaultBranch(ctx, repoDir, "origin")
	if err != nil {
		r.Warn(fmt.Sprintf("%s: could not determine default branch (%v)", filepath.Base(repoDir), err))
		return nil
	}
	if cur, err := git.CurrentBranch(ctx, repoDir); err == nil && cur == br {
		r.OK(fmt.Sprintf("%s: on default branch %s", filepath.Base(repoDir), br))
		return nil
	}
	if err := git.Checkout(ctx, repoDir, br); err != nil {
		r.Warn(fmt.Sprintf("%s: could not checkout %s (%v)", filepath.Base(repoDir), br, err))
		return nil
	}
	r.OK(fmt.Sprintf("%s: checked out default branch %s", filepath.Base(repoDir), br))
	return nil
}

// copyEnvsFor materialises every env mapping for repo into the new working copy.
// Missing staged sources warn but do not fail the clone.
func copyEnvsFor(r Reporter, metaRoot string, repo config.Repo, workingCopy string) {
	if len(repo.Envs) == 0 {
		return
	}
	stagingRoot := filepath.Join(envStagingDir(metaRoot), repo.Folder)
	for _, env := range repo.Envs {
		src := filepath.Join(stagingRoot, env.Source)
		dst := filepath.Join(workingCopy, repo.Folder, env.Target)
		if _, err := os.Stat(src); err != nil {
			r.Warn(fmt.Sprintf("%s: staged env %s not found, skipping (%s -> %s)", repo.Folder, src, env.Source, env.Target))
			continue
		}
		if err := copyFile(src, dst); err != nil {
			r.Fail(fmt.Sprintf("%s: copy env %s -> %s: %v", repo.Folder, env.Source, env.Target, err))
			continue
		}
		r.OK(fmt.Sprintf("%s: env %s -> %s", repo.Folder, env.Source, env.Target))
	}
}

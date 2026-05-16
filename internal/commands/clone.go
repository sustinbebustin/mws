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
		Use:   "clone <suffix>",
		Short: "Create a new peer working copy sharing this meta workspace",
		Long: `clone creates a new peer working copy as a sibling of the meta workspace.
The new directory is named "<project>-<suffix>". For example, in a project
"my-project", "mws clone bug-fix" creates "my-project-bug-fix/".

It symlinks every top-level meta entry (excluding .git) into the new peer and
clones each registered native repo using "git clone --local" from the invoking
peer when available, falling back to the URL from .mws/config.toml.

The new peer's native repos check out each repo's default branch, not the
invoking peer's current branch -- a deliberate fresh-start semantic.

Clone failures for individual native repos are reported but do not abort the
overall flow.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var suffix string
			if len(args) == 1 {
				suffix = args[0]
			}
			return runClone(cmd.Context(), newConsoleReporter(), suffix)
		},
	}
}

func runClone(ctx context.Context, r Reporter, suffix string) error {
	if suffix == "" {
		if err := huh.NewInput().
			Title("Peer name suffix").
			Description("New peer will be created at <project>-<suffix>/ as a sibling of the meta.").
			Validate(validateProjectName).
			Value(&suffix).
			Run(); err != nil {
			return err
		}
		suffix = strings.TrimSpace(suffix)
	}
	if err := validateProjectName(suffix); err != nil {
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

	parent := filepath.Dir(ws.MetaRoot)
	projectName := project.Name(ws.MetaRoot)
	peerName := projectName + "-" + suffix
	newPeer := filepath.Join(parent, peerName)
	if _, err := os.Stat(newPeer); err == nil {
		return fmt.Errorf("path already exists: %s", newPeer)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if filepath.Base(newPeer) == filepath.Base(ws.MetaRoot) {
		return fmt.Errorf("new peer name conflicts with meta workspace")
	}

	if err := os.MkdirAll(newPeer, 0o755); err != nil {
		return err
	}

	linked, err := project.LinkMetaIntoWorkingCopy(ws.MetaRoot, newPeer)
	if err != nil {
		return err
	}
	for _, name := range linked {
		r.OK(fmt.Sprintf("Linked %s", filepath.Join(filepath.Base(newPeer), name)))
	}

	// Prefer the invoking peer as the local clone source.
	invoker := ws.WorkingCopy

	for _, repo := range cfg.Repos {
		if err := cloneNative(ctx, r, repo, invoker, newPeer); err != nil {
			r.Fail(fmt.Sprintf("%s: %v", repo.Folder, err))
			continue
		}
	}

	r.OK(fmt.Sprintf("Peer ready at %s", newPeer))
	return nil
}

// cloneNative populates newPeer/<folder> with a clone of the repo.
// Tries `git clone --local invoker/<folder>` first, then falls back to `git clone <url>`.
// After cloning, checks out the repo's default branch.
func cloneNative(ctx context.Context, r Reporter, repo config.Repo, invoker, newPeer string) error {
	target := filepath.Join(newPeer, repo.Folder)
	if _, err := os.Stat(target); err == nil {
		r.Info(fmt.Sprintf("%s: already present, skipping", repo.Folder))
		return nil
	}

	r.Heading(fmt.Sprintf("Cloning %s ...", repo.Folder))

	var lastErr error
	if invoker != "" {
		src := filepath.Join(invoker, repo.Folder)
		if _, err := os.Stat(filepath.Join(src, ".git")); err == nil {
			if err := git.CloneLocal(ctx, src, target); err != nil {
				lastErr = err
				r.Warn(fmt.Sprintf("%s: --local clone failed (%v), falling back to remote", repo.Folder, err))
			} else {
				return checkoutDefault(ctx, r, target)
			}
		}
	}

	if repo.URL == "" {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("no local source and no remote URL configured")
	}

	if err := git.Clone(ctx, repo.URL, target); err != nil {
		return err
	}
	return checkoutDefault(ctx, r, target)
}

func checkoutDefault(ctx context.Context, r Reporter, repoDir string) error {
	br, err := git.DefaultBranch(ctx, repoDir, "origin")
	if err != nil {
		// Not fatal: leave the clone on whatever HEAD git picked.
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

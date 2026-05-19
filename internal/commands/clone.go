package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
)

// setupChoice models the three policies for running [[repos.setup]] commands
// at the end of `mws clone`. Collapsing the (--setup, --no-setup) flag pair
// into a single enum at the command boundary keeps the rest of the file free
// of a two-bool state that has no meaningful "both true" representation.
type setupChoice int

const (
	setupAsk      setupChoice = iota // no flag -- prompt the user
	setupForceRun                    // --setup
	setupSkip                        // --no-setup
)

func newCloneCmd() *cobra.Command {
	var doSetup, noSetup bool
	cmd := &cobra.Command{
		Use:   "clone <name>",
		Short: "Create a new working copy inside this meta workspace",
		Long: `clone creates a new working copy at <meta-root>/<name>/. It clones each
registered native repo into the new working copy (using "git clone --local"
from the invoking working copy when available, falling back to the configured
URL), fans the harness symlinks out from .mws/, and copies any env files
mapped in .mws.toml from env staging into the working copy.

Native repos are checked out on each repo's default branch, not on the
invoking copy's current branch -- a deliberate fresh-start semantic.

After clone and env-copy succeed, any [[repos.setup]] commands run inside
each cloned repo via sh -c. By default a single confirmation prompt summarises
all configured commands. Use --setup to run without prompting or --no-setup
to skip entirely.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			choice := setupAsk
			switch {
			case doSetup:
				choice = setupForceRun
			case noSetup:
				choice = setupSkip
			}
			return runClone(cmd.Context(), newConsoleReporter(), name, choice)
		},
	}
	cmd.Flags().BoolVar(&doSetup, "setup", false, "run [[repos.setup]] commands without prompting")
	cmd.Flags().BoolVar(&noSetup, "no-setup", false, "skip [[repos.setup]] commands without prompting")
	cmd.MarkFlagsMutuallyExclusive("setup", "no-setup")
	return cmd
}

func runClone(ctx context.Context, r Reporter, name string, choice setupChoice) error {
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
	if err := project.ValidateWorkingCopiesDir(ws.WorkingCopiesDir); err != nil {
		return fmt.Errorf("invalid working_copies_dir %q in .mws.toml: %w", ws.WorkingCopiesDir, err)
	}

	if name == "" {
		if err := huh.NewInput().
			Title("New working copy name").
			Description(clonePromptDescription(ws)).
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

	target := filepath.Join(ws.CopiesRoot(), name)
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

	items := collectSetup(cfg)
	run, err := confirmSetup(choice, items)
	if err != nil {
		return err
	}
	if run {
		setupFailed := runSetup(ctx, r, target, items, os.Stdout, os.Stderr)
		if len(setupFailed) > 0 {
			return fmt.Errorf("clone completed but %d setup command(s) failed:\n  %s",
				len(setupFailed), strings.Join(setupFailed, "\n  "))
		}
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
	peers, err := ws.EnumerateCopies()
	if err != nil {
		return "", err
	}
	if len(peers) == 0 {
		return "", nil
	}
	return peers[0], nil
}

// clonePromptDescription renders the help text shown under the "New working
// copy name" prompt, reflecting the configured working_copies_dir (if any).
func clonePromptDescription(ws *project.Workspace) string {
	if ws.WorkingCopiesDir != "" {
		return fmt.Sprintf("Will be created at <meta-root>/%s/<name>/.", ws.WorkingCopiesDir)
	}
	return "Will be created at <meta-root>/<name>/."
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
				// --local left origin pointing at the sibling working copy. Rewrite it to
				// the canonical URL from .mws.toml so push/pull/fetch hit the real remote.
				if repo.URL != "" {
					if err := git.SetRemoteURL(ctx, dst, "origin", repo.URL); err != nil {
						r.Warn(fmt.Sprintf("%s: could not retarget origin to %s (%v); push/pull will target the sibling copy", repo.Folder, repo.URL, err))
					}
				}
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

// confirmSetup decides whether [[repos.setup]] commands run. With explicit
// flags the answer is immediate; otherwise the user is prompted with a
// grouped listing of every configured command. An empty items slice always
// returns (false, nil) without prompting -- nothing to ask about.
func confirmSetup(choice setupChoice, items []setupItem) (bool, error) {
	if len(items) == 0 {
		return false, nil
	}
	switch choice {
	case setupForceRun:
		return true, nil
	case setupSkip:
		return false, nil
	}
	var ok bool
	if err := huh.NewConfirm().
		Title("Run setup commands?").
		Description(setupPromptBody(items)).
		Affirmative("Run").
		Negative("Skip").
		Value(&ok).
		Run(); err != nil {
		return false, err
	}
	return ok, nil
}

// setupPromptBody renders items grouped by repo folder, two-space indented
// under each folder heading. See docs/adr/0006-post-clone-setup-commands.md.
func setupPromptBody(items []setupItem) string {
	var b strings.Builder
	var lastFolder string
	for i, it := range items {
		if it.Folder != lastFolder {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(it.Folder)
			b.WriteString("\n")
			lastFolder = it.Folder
		}
		b.WriteString("  ")
		b.WriteString(it.Cmd)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// setupItem is one flat (repo folder, shell command) pair scheduled by
// collectSetup. The flat shape lets confirmSetup/runSetup iterate without
// re-walking the config and decouples them from the config types.
type setupItem struct {
	Folder string
	Cmd    string
}

// collectSetup returns the [[repos.setup]] commands across cfg as a flat,
// ordered slice. Each cmd is trimmed; entries that are empty after trimming
// are dropped. Repos with no setup contribute nothing. An empty return means
// callers should short-circuit without prompting or executing.
func collectSetup(cfg *config.Config) []setupItem {
	var out []setupItem
	for _, repo := range cfg.Repos {
		for _, sc := range repo.Setup {
			cmd := strings.TrimSpace(sc.Cmd)
			if cmd == "" {
				continue
			}
			out = append(out, setupItem{Folder: repo.Folder, Cmd: cmd})
		}
	}
	return out
}

// runSetup executes items in order against target/<folder>. Commands inside a
// single repo run sequentially and stop at the first non-zero exit; failures
// in one repo do not block other repos. Returns "<folder>: <cmd>" strings for
// each failure -- nil if everything passed. stdout/stderr are injected so
// production can stream live to the terminal and tests can discard.
func runSetup(ctx context.Context, r Reporter, target string, items []setupItem, stdout, stderr io.Writer) []string {
	var failed []string
	var skipFolder, lastHeading string
	for _, it := range items {
		if it.Folder == skipFolder {
			continue
		}
		if it.Folder != lastHeading {
			r.Heading(fmt.Sprintf("Setup: %s ...", it.Folder))
			lastHeading = it.Folder
		}
		r.Info(fmt.Sprintf("$ %s", it.Cmd))

		cmd := exec.CommandContext(ctx, "sh", "-c", it.Cmd)
		cmd.Dir = filepath.Join(target, it.Folder)
		cmd.Env = os.Environ()
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.Stdin = nil

		if err := cmd.Run(); err != nil {
			r.Fail(fmt.Sprintf("%s: %s (%v)", it.Folder, it.Cmd, err))
			failed = append(failed, fmt.Sprintf("%s: %s", it.Folder, it.Cmd))
			skipFolder = it.Folder
			continue
		}
		r.OK(fmt.Sprintf("%s: %s", it.Folder, it.Cmd))
	}
	return failed
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

package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/worktree"
)

const worktreeLong = `worktree manages worktrees of a working copy: children at
<copy>/<worktrees dir>/<name>/ (default .wt) holding one git worktree of every
native repo on branch <prefix><name> (default wt/<name>), the harness symlinks,
and copies of the working copy's per-copy state.

Every subcommand is non-interactive. Results go to stdout, progress to stderr.
--json prints one JSON object, or {"error": {"code", "message"}} on failure.

Exit codes: 0 ok, 1 failure, 2 usage, 3 refused by a safety check, 4 no
worktree by that name.`

func newWorktreeCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "worktree",
		Aliases: []string{"wt"},
		Short:   "Create, list, run in and remove worktrees of a working copy",
		Long:    worktreeLong,
	}
	cmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "print results as JSON")
	cmd.AddCommand(
		newWorktreeNewCmd(&jsonOut),
		newWorktreePathCmd(&jsonOut),
		newWorktreeRmCmd(&jsonOut),
		newWorktreeExecCmd(&jsonOut),
		newWorktreeLsCmd(&jsonOut),
		newWorktreePruneCmd(&jsonOut),
		newWorktreeHookCmd(),
	)
	return cmd
}

type wtNewFlags struct {
	branch, base string
	noSetup      bool
}

func newWorktreeNewCmd(jsonOut *bool) *cobra.Command {
	var f wtNewFlags
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a worktree of this working copy and print its path",
		Long: `new creates <copy>/<worktrees dir>/<name>/ and prints its absolute path as the
last stdout line. Run it from anywhere inside a working copy; run from inside a
worktree, the new one is based on that worktree's checkouts.

Each native repo gets a git worktree on <prefix><name> (or --branch), started
at the launching checkout's HEAD (or --base). An existing branch that no
worktree has checked out is reused. Then the harness is linked, root files are
copied, other root directories are linked, env targets are copied from the
working copy's repos, [[repos.setup]] runs in each repo (unless repo_setup is
false) and [[worktrees.setup]] runs in the root. --no-setup skips both.

Re-running for an existing worktree prints its path and exits 0. A failure
before the worktree is complete removes everything it created. A setup failure
keeps the worktree, prints its path and exits 1. Refuses (exit 3) a branch
checked out elsewhere and a repo in the middle of a merge or rebase.`,
		Example: `  mws wt new 04
  mws wt new 04 --branch feat/save-roof --base main --json`,
		Args: usageCheckedArgs(cobra.ExactArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := newOutput(cmd, *jsonOut)
			ws, cfg, err := o.locate()
			if err != nil {
				return err
			}
			return runWorktreeNew(cmd.Context(), o, ws, cfg, args[0], f)
		},
	}
	cmd.Flags().StringVar(&f.branch, "branch", "", "branch to create or reuse in every native repo (default <prefix><name>)")
	cmd.Flags().StringVar(&f.base, "base", "", "start point for new branches (default the launching checkout's HEAD)")
	cmd.Flags().BoolVar(&f.noSetup, "no-setup", false, "skip [[repos.setup]] and [[worktrees.setup]]")
	return cmd
}

func newWorktreePathCmd(jsonOut *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "path <name>",
		Short: "Print the absolute path of a worktree",
		Long: `path prints the absolute path of the managed worktree <name> of this working
copy. A name with no worktree exits 4; one mws does not manage exits 3.`,
		Example: `  mws wt path 04
  cd "$(mws wt path 04)"`,
		Args: usageCheckedArgs(cobra.ExactArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorktreePath(cmd.Context(), newOutput(cmd, *jsonOut), args[0])
		},
	}
}

// wtNewJSON is the `wt new --json` shape. Error is set only when setup failed
// after the worktree was created.
type wtNewJSON struct {
	worktree.Result
	Error *jsonError `json:"error,omitempty"`
}

// runWorktreeNew is `wt new` in an already located workspace; the
// WorktreeCreate hook calls it with the workspace of Claude's project dir.
func runWorktreeNew(ctx context.Context, o output, ws *project.Workspace, cfg *config.Config, name string, f wtNewFlags) error {
	r := o.reporter()
	res, err := worktree.New(ctx, r, worktree.NewOptions{
		Workspace: ws,
		Config:    cfg,
		Name:      name,
		Branch:    f.branch,
		Base:      f.base,
		Version:   version,
	})
	if err != nil {
		return o.fail(err)
	}
	if res.Reused {
		r.Info(fmt.Sprintf("Reused existing worktree %s", res.Path))
	} else {
		r.OK(fmt.Sprintf("Worktree %s ready", res.Name))
	}

	var setupErr error
	if !res.Reused && !f.noSetup {
		setupErr = runWorktreeSetup(ctx, r, ws.MetaRoot, cfg, res, o.stderr)
	}

	payload := wtNewJSON{Result: res}
	if setupErr != nil {
		payload.Error = toJSONError(setupErr)
	}
	err = o.result(payload, func(w io.Writer) error {
		_, err := fmt.Fprintln(w, res.Path)
		return err
	})
	if err != nil {
		return errors.Join(setupErr, err)
	}
	return setupErr
}

// runWorktreeSetup runs [[repos.setup]] in each inner repo (when the policy
// allows) and then [[worktrees.setup]] in the root, each with the working
// copy's export set, since a worktree shares its copy's Port slot. Command
// output goes to w so stdout stays reserved for the result.
func runWorktreeSetup(ctx context.Context, r Reporter, metaRoot string, cfg *config.Config, res worktree.Result, w io.Writer) error {
	exports, err := workingCopy{metaRoot: metaRoot, cfg: cfg, path: res.Copy}.exports()
	if err != nil {
		return &setupFailedError{err: fmt.Errorf(
			"worktree %s was created at %s, but no setup command ran: %w; the worktree is kept and usable",
			res.Name, res.Path, err)}
	}
	policy := cfg.WorktreePolicy()
	var items []setupItem
	if policy.RepoSetup {
		present := map[string]bool{}
		for _, repo := range res.Repos {
			present[repo.Folder] = true
		}
		var repos []config.Repo
		for _, repo := range append(append([]config.Repo{}, cfg.Repos...), cfg.OptionalRepos...) {
			if present[repo.Folder] {
				repos = append(repos, repo)
			}
		}
		items = collectSetup(repos)
	}
	items = append(items, collectSetup([]config.Repo{{Folder: ".", Setup: policy.Setup}})...)
	failed := runSetup(ctx, r, res.Path, exports, items, w, w)
	if len(failed) == 0 {
		return nil
	}
	return &setupFailedError{err: fmt.Errorf(
		"worktree %s was created at %s, but %d setup command(s) failed:\n  %s\nthe worktree is kept and usable; fix the cause, then rerun the command(s) inside it",
		res.Name, res.Path, len(failed), strings.Join(failed, "\n  "))}
}

func runWorktreePath(ctx context.Context, o output, name string) error {
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	p, err := worktree.Resolve(ctx, ws, cfg.WorktreePolicy(), name)
	if err != nil {
		return o.fail(err)
	}
	return o.result(struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Copy string `json:"copy"`
	}{name, p, ws.WorkingCopy}, func(w io.Writer) error {
		_, err := fmt.Fprintln(w, p)
		return err
	})
}

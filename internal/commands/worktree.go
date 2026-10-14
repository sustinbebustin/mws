package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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

Exit codes: 0 ok, 1 failure, 2 usage, 3 refused by a safety check.`

func newWorktreeCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "worktree",
		Aliases: []string{"wt"},
		Short:   "Create and locate worktrees of a working copy",
		Long:    worktreeLong,
	}
	cmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "print results as JSON")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &worktree.Error{Kind: worktree.Usage, Err: err}
	})
	cmd.AddCommand(newWorktreeNewCmd(&jsonOut), newWorktreePathCmd(&jsonOut))
	return cmd
}

// wtArgs validates positional args, tagging a mismatch as a usage error and
// printing it as JSON under --json (flags are parsed before args).
func wtArgs(n int, jsonOut *bool) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(n)(cmd, args); err != nil {
			return emitWtError(cmd.OutOrStdout(), *jsonOut, &worktree.Error{Kind: worktree.Usage, Err: fmt.Errorf("%w; see `%s --help`", err, cmd.CommandPath())})
		}
		return nil
	}
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
		Args: wtArgs(1, jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorktreeNew(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), *jsonOut, args[0], f)
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
		Args:  wtArgs(1, jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorktreePath(cmd.Context(), cmd.OutOrStdout(), *jsonOut, args[0])
		},
	}
}

// newJSON is the `wt new --json` shape. Error is set only when setup failed
// after the worktree was created.
type newJSON struct {
	worktree.Result
	Error *jsonError `json:"error,omitempty"`
}

type jsonError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// setupFailedError reports setup commands that failed in a worktree that was
// otherwise created and is kept.
type setupFailedError struct{ err error }

func (e *setupFailedError) Error() string { return e.err.Error() }
func (e *setupFailedError) ExitCode() int { return 1 }

func runWorktreeNew(ctx context.Context, out, errW io.Writer, jsonOut bool, name string, f wtNewFlags) error {
	r := consoleReporter{w: errW}
	ws, cfg, err := locateForWorktree()
	if err != nil {
		return emitWtError(out, jsonOut, err)
	}
	res, err := worktree.New(ctx, r, worktree.NewOptions{
		Workspace: ws,
		Config:    cfg,
		Name:      name,
		Branch:    f.branch,
		Base:      f.base,
		Version:   version,
	})
	if err != nil {
		return emitWtError(out, jsonOut, err)
	}
	if res.Reused {
		r.Info(fmt.Sprintf("Reused existing worktree %s", res.Path))
	} else {
		r.OK(fmt.Sprintf("Worktree %s ready", res.Name))
	}

	var setupErr error
	if !res.Reused && !f.noSetup {
		setupErr = runWorktreeSetup(ctx, r, cfg, res, errW)
	}

	if jsonOut {
		payload := newJSON{Result: res}
		if setupErr != nil {
			payload.Error = toJSONError(setupErr)
		}
		if err := writeJSON(out, payload); err != nil {
			return errors.Join(setupErr, err)
		}
		return setupErr
	}
	if _, err := fmt.Fprintln(out, res.Path); err != nil {
		return errors.Join(setupErr, err)
	}
	return setupErr
}

// runWorktreeSetup runs [[repos.setup]] in each inner repo (when the policy
// allows) and then [[worktrees.setup]] in the root. Command output goes to w
// so stdout stays reserved for the result.
func runWorktreeSetup(ctx context.Context, r Reporter, cfg *config.Config, res worktree.Result, w io.Writer) error {
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
	failed := runSetup(ctx, r, res.Path, items, w, w)
	if len(failed) == 0 {
		return nil
	}
	return &setupFailedError{err: fmt.Errorf(
		"worktree %s was created at %s, but %d setup command(s) failed:\n  %s\nthe worktree is kept and usable; fix the cause, then rerun the command(s) inside it",
		res.Name, res.Path, len(failed), strings.Join(failed, "\n  "))}
}

func runWorktreePath(ctx context.Context, out io.Writer, jsonOut bool, name string) error {
	ws, cfg, err := locateForWorktree()
	if err != nil {
		return emitWtError(out, jsonOut, err)
	}
	if ws.WorkingCopy == "" {
		return emitWtError(out, jsonOut, &worktree.Error{Kind: worktree.Usage, Err: fmt.Errorf("not inside a working copy: run mws worktree from a working copy (or a worktree of one) under %s", ws.MetaRoot)})
	}
	if err := project.ValidateName(name); err != nil {
		return emitWtError(out, jsonOut, &worktree.Error{Kind: worktree.Usage, Err: fmt.Errorf("invalid worktree name %q: %w", name, err)})
	}
	p := worktree.Path(ws.WorkingCopy, cfg.WorktreePolicy(), name)
	managed, err := worktree.IsManaged(ctx, ws.MetaRoot, p)
	if err != nil {
		return emitWtError(out, jsonOut, &worktree.Error{Kind: worktree.Failure, Err: fmt.Errorf("check worktree %s: %w", p, err)})
	}
	if !managed {
		return emitWtError(out, jsonOut, &worktree.Error{Kind: worktree.Failure, Err: fmt.Errorf("no worktree named %s in working copy %s (nothing managed at %s); create it with `mws wt new %s`", name, ws.WorkingCopy, p, name)})
	}
	if jsonOut {
		return writeJSON(out, struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Copy string `json:"copy"`
		}{name, p, ws.WorkingCopy})
	}
	_, err = fmt.Fprintln(out, p)
	return err
}

// locateForWorktree resolves the workspace from cwd and loads its config.
func locateForWorktree() (*project.Workspace, *config.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, &worktree.Error{Kind: worktree.Failure, Err: fmt.Errorf("read current directory: %w", err)}
	}
	ws, err := project.Locate(cwd)
	if err != nil {
		return nil, nil, &worktree.Error{Kind: worktree.Usage, Err: fmt.Errorf("%s: %w; run mws worktree from inside a working copy", cwd, err)}
	}
	cfg, err := config.Load(ws.MetaRoot)
	if err != nil {
		return nil, nil, &worktree.Error{Kind: worktree.Failure, Err: err}
	}
	return ws, cfg, nil
}

// emitWtError prints err as the JSON error object when jsonOut is set, and
// returns it for the exit code and the stderr message.
func emitWtError(out io.Writer, jsonOut bool, err error) error {
	if !jsonOut {
		return err
	}
	return errors.Join(err, writeJSON(out, struct {
		Error *jsonError `json:"error"`
	}{toJSONError(err)}))
}

func toJSONError(err error) *jsonError {
	code := "failure"
	var we *worktree.Error
	var se *setupFailedError
	switch {
	case errors.As(err, &se):
		code = "setup_failed"
	case errors.As(err, &we) && we.Kind == worktree.Usage:
		code = "usage"
	case errors.As(err, &we) && we.Kind == worktree.Refused:
		code = "refused"
	}
	return &jsonError{Code: code, Message: err.Error()}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ExitCode maps an error returned by the root command to the process exit
// code: 0 for nil, the error's own code when it carries one, else 1.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 1
}

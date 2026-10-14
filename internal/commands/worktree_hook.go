package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/worktree"
)

// claudeProjectDirEnv is set by Claude Code for every hook: the session's
// project directory, which for subagent and entered worktrees stays at the
// working copy.
const claudeProjectDirEnv = "CLAUDE_PROJECT_DIR"

const worktreeHookLong = `hook adapts Claude Code's worktree hooks to mws worktrees, so Claude's
built-in worktrees (claude -w, EnterWorktree, Agent isolation: "worktree") use
the mws layout. Each subcommand reads the hook's JSON payload on stdin.

  create         WorktreeCreate. Payload {name, cwd}. Creates (or reuses) the
                 worktree <name> of the working copy at $CLAUDE_PROJECT_DIR,
                 or at cwd when that is unset, based on the HEAD of that
                 directory's checkouts, runs setup, and prints only its path.
                 A setup failure warns and still exits 0; any other failure
                 exits 1 so Claude creates nothing.
  remove         WorktreeRemove. Payload {worktree_path}. Removes the worktree
                 like ` + "`mws wt rm`" + ` without --force; exits 1 on any refusal
                 so Claude keeps the directory.
  subagent-stop  SubagentStop. Payload {cwd}. Always exits 0. When cwd is in
                 a worktree mws manages, removes it only if every native repo
                 is clean and no branch holds a commit no other ref reaches;
                 otherwise keeps it and says why on stderr.

Wire them in .claude/settings.json as command hooks, e.g.
  "WorktreeCreate": [{"hooks": [{"type": "command", "command": "mws wt hook create"}]}]`

func newWorktreeHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Claude Code hook adapters (create, remove, subagent-stop)",
		Long:   worktreeHookLong,
		Hidden: true,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "create",
			Short: "WorktreeCreate hook: create a worktree and print its path",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return hookFailed(runHookCreate(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()))
			},
		},
		&cobra.Command{
			Use:   "remove",
			Short: "WorktreeRemove hook: remove a worktree, exit 1 on any refusal",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return hookFailed(runHookRemove(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr()))
			},
		},
	)
	return cmd
}

// hookError makes every hook failure exit 1, the code Claude Code treats as
// "do not proceed", whatever kind of failure caused it.
type hookError struct{ err error }

func (e *hookError) Error() string { return e.err.Error() }
func (e *hookError) Unwrap() error { return e.err }
func (e *hookError) ExitCode() int { return 1 }

func hookFailed(err error) error {
	if err == nil {
		return nil
	}
	return &hookError{err: err}
}

// createPayload is the part of the WorktreeCreate payload mws reads.
type createPayload struct {
	Name string `json:"name"`
	Cwd  string `json:"cwd"`
}

// decodePayload parses the hook payload on stdin into v.
func decodePayload(in io.Reader, v any) error {
	if err := json.NewDecoder(in).Decode(v); err != nil {
		return fmt.Errorf("read the hook payload on stdin: %w; mws wt hook expects the JSON object Claude Code sends", err)
	}
	return nil
}

func runHookCreate(ctx context.Context, in io.Reader, out, errW io.Writer) error {
	var p createPayload
	if err := decodePayload(in, &p); err != nil {
		return err
	}
	if p.Name == "" {
		return errors.New(`the WorktreeCreate payload has no "name"; nothing was created`)
	}
	start := os.Getenv(claudeProjectDirEnv)
	if start == "" {
		start = p.Cwd
	}
	if start == "" {
		return fmt.Errorf(`neither $%s nor the payload's "cwd" is set, so there is no working copy to create worktree %s in; nothing was created`, claudeProjectDirEnv, p.Name)
	}
	ws, cfg, err := locateWorktreeAt(start, "Claude Code's project directory must be a working copy (or a worktree of one) of a meta workspace")
	if err != nil {
		return err
	}
	err = createWorktree(ctx, out, errW, false, ws, cfg, p.Name, wtNewFlags{})
	var se *setupFailedError
	if errors.As(err, &se) {
		consoleReporter{w: errW}.Warn(se.Error())
		return nil
	}
	return err
}

// removePayload is the part of the WorktreeRemove payload mws reads.
type removePayload struct {
	WorktreePath string `json:"worktree_path"`
}

func runHookRemove(ctx context.Context, in io.Reader, errW io.Writer) error {
	var p removePayload
	if err := decodePayload(in, &p); err != nil {
		return err
	}
	if p.WorktreePath == "" {
		return errors.New(`the WorktreeRemove payload has no "worktree_path"; nothing was removed`)
	}
	ws, cfg, name, err := locateManagedRoot(p.WorktreePath)
	if err != nil {
		return err
	}
	if name == "" || ws.Worktree != filepath.Clean(p.WorktreePath) {
		return fmt.Errorf("%s is not the root of a worktree mws manages, so mws will not remove it; nothing was removed; inspect it and remove it by hand if it is yours", p.WorktreePath)
	}
	r := consoleReporter{w: errW}
	removed, err := worktree.Remove(ctx, r, worktree.RemoveOptions{Workspace: ws, Config: cfg, Names: []string{name}})
	if err != nil {
		return err
	}
	reportKeptBranches(r, removed)
	return nil
}

// locateManagedRoot resolves the workspace of dir and, when dir lies in a
// marked worktree in its copy's worktrees dir, that worktree's name ("" when
// it does not). Registration is checked later by worktree.Resolve.
func locateManagedRoot(dir string) (*project.Workspace, *config.Config, string, error) {
	ws, cfg, err := locateWorktreeAt(dir, "it is not inside a meta workspace")
	if err != nil {
		return nil, nil, "", err
	}
	if ws.Worktree == "" {
		return ws, cfg, "", nil
	}
	name := filepath.Base(ws.Worktree)
	if worktree.Path(ws.WorkingCopy, cfg.WorktreePolicy(), name) != ws.Worktree {
		return ws, cfg, "", nil
	}
	return ws, cfg, name, nil
}

// reportKeptBranches says which branches a removal kept, and why.
func reportKeptBranches(r Reporter, removed []worktree.Removed) {
	for _, rm := range removed {
		for _, b := range rm.Branches {
			if !b.Deleted {
				r.Info(fmt.Sprintf("kept %s:%s, %s", b.Folder, b.Branch, b.Reason))
			}
		}
	}
}

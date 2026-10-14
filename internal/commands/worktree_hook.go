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

	"github.com/sustinbebustin/mws/internal/clierr"
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
  subagent-stop  SubagentStop. Payload {cwd, agent_id}. Always exits 0. When
                 cwd is in the worktree agent-<agent_id> and mws manages it,
                 removes it only if every native repo is clean and no branch
                 holds a commit no other ref reaches; otherwise keeps it and
                 says why on stderr. Any other worktree is left alone.

Wire them in .claude/settings.json as command hooks, e.g.
  "WorktreeCreate": [{"hooks": [{"type": "command", "command": "mws wt hook create"}]}]`

func newWorktreeHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Claude Code hook adapters (create, remove, subagent-stop)",
		Long:  worktreeHookLong,
		Example: `  echo '{"name":"04","cwd":"'"$PWD"'"}' | mws wt hook create
  echo '{"worktree_path":"'"$PWD"'/.wt/04"}' | mws wt hook remove
  echo '{"cwd":"'"$PWD"'/.wt/agent-a1b2","agent_id":"a1b2"}' | mws wt hook subagent-stop`,
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
		&cobra.Command{
			Use:   "subagent-stop",
			Short: "SubagentStop hook: remove a finished subagent's worktree if it holds no work; always exits 0",
			// Exit 2 from a SubagentStop hook would stop the subagent from
			// finishing, so even stray arguments must not fail it.
			Args: cobra.ArbitraryArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				r := reporterFor(cmd.ErrOrStderr())
				if err := runHookSubagentStop(cmd.Context(), r, cmd.InOrStdin()); err != nil {
					r.Warn(err.Error())
				}
				return nil
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
	ws, cfg, err := locateWorkspace(start, "Claude Code's project directory must be a working copy (or a worktree of one) of a meta workspace")
	if err != nil {
		return err
	}
	err = runWorktreeNew(ctx, output{stdout: out, stderr: errW}, ws, cfg, p.Name, wtNewFlags{})
	var se *setupFailedError
	if errors.As(err, &se) {
		reporterFor(errW).Warn(se.Error())
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
	// Locate resolves symlinks, so compare against the resolved payload path.
	root, err := filepath.EvalSymlinks(p.WorktreePath)
	if err != nil {
		root = filepath.Clean(p.WorktreePath)
	}
	if name == "" || ws.Worktree != root {
		return fmt.Errorf("%s is not the root of a worktree mws manages, so mws will not remove it; nothing was removed; inspect it and remove it by hand if it is yours", p.WorktreePath)
	}
	r := reporterFor(errW)
	removed, err := worktree.Remove(ctx, r, worktree.RemoveOptions{Workspace: ws, Config: cfg, Names: []string{name}})
	if err != nil {
		return err
	}
	reportKeptBranches(r, removed)
	return nil
}

// subagentStopPayload is the part of the SubagentStop payload mws reads.
type subagentStopPayload struct {
	Cwd     string `json:"cwd"`
	AgentID string `json:"agent_id"`
}

// agentWorktreePrefix starts the name Claude gives an isolated subagent's
// worktree: agent-<agent_id>.
const agentWorktreePrefix = "agent-"

// runHookSubagentStop removes the stopping subagent's own isolation worktree
// when it is managed and holds no work. A subagent without isolation stops
// with its session's cwd, which may be another worktree; that one is never
// touched. A nil error with nothing removed means cwd was not in the agent's
// own worktree.
func runHookSubagentStop(ctx context.Context, r Reporter, in io.Reader) error {
	var p subagentStopPayload
	if err := decodePayload(in, &p); err != nil {
		return err
	}
	if p.Cwd == "" || p.AgentID == "" {
		return nil
	}
	ws, cfg, name, err := locateManagedRoot(p.Cwd)
	if err != nil && clierr.KindOf(err) == clierr.Usage {
		return nil // outside every meta workspace: not ours
	}
	if err != nil {
		return fmt.Errorf("left %s alone: %w", p.Cwd, err)
	}
	if name != agentWorktreePrefix+p.AgentID {
		return nil
	}
	managed, err := worktree.IsManaged(ctx, ws.MetaRoot, ws.Worktree)
	if err != nil {
		return fmt.Errorf("kept worktree %s: cannot check its registration: %w", name, err)
	}
	if !managed {
		return nil
	}
	removed, err := worktree.Remove(ctx, r, worktree.RemoveOptions{Workspace: ws, Config: cfg, Names: []string{name}, OnlyIfMerged: true})
	if err != nil {
		return fmt.Errorf("kept worktree %s: %w", name, err)
	}
	reportKeptBranches(r, removed)
	return nil
}

// locateManagedRoot resolves the workspace of dir and, when dir lies in a
// marked worktree in its copy's worktrees dir, that worktree's name ("" when
// it does not). Registration is checked later by worktree.Resolve.
func locateManagedRoot(dir string) (*project.Workspace, *config.Config, string, error) {
	ws, cfg, err := locateWorkspace(dir, "it is not inside a meta workspace")
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

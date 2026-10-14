package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/worktree"
)

func newWorktreeExecCmd(jsonOut *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "exec <name> -- <cmd> [args...]",
		Short: "Run a command in a worktree's root",
		Long: `exec runs <cmd> with its working directory at the worktree's root and stdin,
stdout and stderr inherited. mws exits with the command's exit code; it fails
with 1 only when the worktree is not found or the command cannot start.`,
		Example: `  mws wt exec 04 -- just typecheck
  mws wt exec 04 -- git -C frontend log --oneline -3`,
		Args: wtArgs(func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 || cmd.ArgsLenAtDash() != 1 {
				return fmt.Errorf("want a worktree name, then -- and the command to run, got %q", args)
			}
			return nil
		}, jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorktreeExec(cmd.Context(), cmd, *jsonOut, args[0], args[1:])
		},
	}
}

func runWorktreeExec(ctx context.Context, cmd *cobra.Command, jsonOut bool, name string, argv []string) error {
	out := cmd.OutOrStdout()
	ws, cfg, err := locateForWorktree()
	if err != nil {
		return emitWtError(out, jsonOut, err)
	}
	root, err := worktree.Resolve(ctx, ws, cfg.WorktreePolicy(), name)
	if err != nil {
		return emitWtError(out, jsonOut, err)
	}
	return runIn(ctx, root, argv, cmd.InOrStdin(), out, cmd.ErrOrStderr())
}

// runIn runs argv in dir with the given stdio and turns a non-zero exit into
// an exitStatusError carrying the same code.
func runIn(ctx context.Context, dir string, argv []string, in io.Reader, out, errW io.Writer) error {
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dir
	c.Stdin, c.Stdout, c.Stderr = in, out, errW
	err := c.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() >= 0:
		return &exitStatusError{code: exitErr.ExitCode()}
	case errors.As(err, &exitErr):
		return &worktree.Error{Kind: worktree.Failure, Err: fmt.Errorf("%q in %s did not exit normally: %w", argv[0], dir, err)}
	default:
		return &worktree.Error{Kind: worktree.Failure, Err: fmt.Errorf("run %q in %s: %w; nothing ran; check the command name and PATH", argv[0], dir, err)}
	}
}

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/worktree"
)

// wtOutput is where a `mws wt` subcommand reports: its result on stdout (one
// JSON object under --json), progress and command output on stderr.
type wtOutput struct {
	stdout, stderr io.Writer
	json           bool
}

func newWtOutput(cmd *cobra.Command, jsonOut bool) wtOutput {
	return wtOutput{stdout: cmd.OutOrStdout(), stderr: cmd.ErrOrStderr(), json: jsonOut}
}

func (o wtOutput) reporter() consoleReporter { return consoleReporter{w: o.stderr} }

// fail prints err as the JSON error object under --json and returns it for
// the exit code and the stderr message.
func (o wtOutput) fail(err error) error {
	if !o.json {
		return err
	}
	return errors.Join(err, writeJSON(o.stdout, struct {
		Error *jsonError `json:"error"`
	}{toJSONError(err)}))
}

// result prints v as JSON under --json, and calls text otherwise.
func (o wtOutput) result(v any, text func(w io.Writer) error) error {
	if o.json {
		return writeJSON(o.stdout, v)
	}
	return text(o.stdout)
}

// locate resolves the workspace from cwd and loads its config, reporting a
// failure through fail.
func (o wtOutput) locate() (*project.Workspace, *config.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, o.fail(&worktree.Error{Kind: worktree.Failure, Err: fmt.Errorf("read current directory: %w", err)})
	}
	ws, cfg, err := locateWorkspace(cwd, "run mws worktree from inside a working copy")
	if err != nil {
		return nil, nil, o.fail(err)
	}
	return ws, cfg, nil
}

// locateWorkspace resolves the workspace containing dir and loads its
// config; hint says how to recover when dir is outside every workspace.
func locateWorkspace(dir, hint string) (*project.Workspace, *config.Config, error) {
	ws, err := project.Locate(dir)
	if err != nil {
		return nil, nil, &worktree.Error{Kind: worktree.Usage, Err: fmt.Errorf("%s: %w; %s", dir, err, hint)}
	}
	cfg, err := config.Load(ws.MetaRoot)
	if err != nil {
		return nil, nil, &worktree.Error{Kind: worktree.Failure, Err: err}
	}
	return ws, cfg, nil
}

// usageCheckedArgs validates positional args with check, tagging a mismatch
// as a usage error and printing it as JSON under --json (flags are parsed
// before args).
func usageCheckedArgs(check cobra.PositionalArgs, jsonOut *bool) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := check(cmd, args); err != nil {
			return newWtOutput(cmd, *jsonOut).fail(&worktree.Error{Kind: worktree.Usage, Err: fmt.Errorf("%w; see `%s --help`", err, cmd.CommandPath())})
		}
		return nil
	}
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

func toJSONError(err error) *jsonError {
	var se *setupFailedError
	if errors.As(err, &se) {
		return &jsonError{Code: "setup_failed", Message: err.Error()}
	}
	kind := worktree.Failure
	var we *worktree.Error
	if errors.As(err, &we) {
		kind = we.Kind
	}
	return &jsonError{Code: kind.Code(), Message: err.Error()}
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

// exitStatusError passes a child command's exit status through. The child
// already reported its own failure, so it carries no message of its own.
type exitStatusError struct{ code int }

func (e *exitStatusError) Error() string { return fmt.Sprintf("command exited with status %d", e.code) }
func (e *exitStatusError) ExitCode() int { return e.code }

// Silent reports whether err only carries an exit status, so main prints
// nothing for it.
func Silent(err error) bool {
	var es *exitStatusError
	return errors.As(err, &es)
}

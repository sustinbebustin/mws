package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/ui"
)

// output is where a command reports: its result on stdout (one JSON object
// under --json), progress and command output on stderr.
type output struct {
	stdout, stderr io.Writer
	json           bool
}

func newOutput(cmd *cobra.Command, jsonOut bool) output {
	return output{stdout: cmd.OutOrStdout(), stderr: cmd.ErrOrStderr(), json: jsonOut}
}

func (o output) reporter() consoleReporter { return reporterFor(o.stderr) }

// PrintError writes a command's final error to w, coloured when w is a
// terminal.
func PrintError(w io.Writer, err error) {
	fmt.Fprintln(w, ui.Detect(w).Error(err))
}

// fail prints err as the JSON error object under --json and returns it for
// the exit code and the stderr message.
func (o output) fail(err error) error {
	if !o.json || err == nil {
		return err
	}
	return errors.Join(err, writeJSON(o.stdout, struct {
		Error *jsonError `json:"error"`
	}{toJSONError(err)}))
}

// result prints v as JSON under --json, and calls text otherwise.
func (o output) result(v any, text func(w io.Writer) error) error {
	if o.json {
		return writeJSON(o.stdout, v)
	}
	return text(o.stdout)
}

// locate resolves the workspace from cwd and loads its config, reporting a
// failure through fail.
func (o output) locate() (*project.Workspace, *config.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, o.fail(clierr.Failuref("read current directory: %w", err))
	}
	ws, cfg, err := locateWorkspace(cwd, "run mws from inside a meta workspace or one of its working copies")
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
		return nil, nil, clierr.Usagef("%s: %w; %s", dir, err, hint)
	}
	cfg, err := config.Load(ws.MetaRoot)
	if err != nil {
		return nil, nil, clierr.Failuref("%w", err)
	}
	return ws, cfg, nil
}

func usagef(format string, args ...any) error { return clierr.Usagef(format, args...) }

// usageArgs tags a positional-argument mismatch as a usage error that points
// at the command's help. An error that already carries an exit code passes
// through unchanged.
func usageArgs(check cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		err := check(cmd, args)
		var coded interface{ ExitCode() int }
		if err == nil || errors.As(err, &coded) {
			return err
		}
		return usagef("%w; see `%s --help`", err, cmd.CommandPath())
	}
}

// usageCheckedArgs is usageArgs that also prints the error as JSON under
// --json (flags are parsed before args).
func usageCheckedArgs(check cobra.PositionalArgs, jsonOut *bool) cobra.PositionalArgs {
	tagged := usageArgs(check)
	return func(cmd *cobra.Command, args []string) error {
		return newOutput(cmd, *jsonOut).fail(tagged(cmd, args))
	}
}

// exclusiveFlags returns a usage error when more than one of the named flags
// is set. It replaces cobra's MarkFlagsMutuallyExclusive, whose error cannot
// be told apart from a runtime failure.
func exclusiveFlags(cmd *cobra.Command, names ...string) error {
	var set []string
	for _, n := range names {
		if cmd.Flags().Changed(n) {
			set = append(set, "--"+n)
		}
	}
	if len(set) > 1 {
		return usagef("%s cannot be used together; see `%s --help`", strings.Join(set, " and "), cmd.CommandPath())
	}
	return nil
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
	return &jsonError{Code: clierr.KindOf(err).Code(), Message: err.Error()}
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

// exitStatusError carries an exit status whose cause was already reported: a
// child command's own failure, or a status probe's answer. It has no message
// of its own.
type exitStatusError struct{ code int }

func (e *exitStatusError) Error() string { return fmt.Sprintf("command exited with status %d", e.code) }
func (e *exitStatusError) ExitCode() int { return e.code }

// Silent reports whether err only carries an exit status, so main prints
// nothing for it.
func Silent(err error) bool {
	var es *exitStatusError
	return errors.As(err, &es)
}

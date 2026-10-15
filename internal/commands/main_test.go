package commands

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// cliEnv makes the test binary act as the mws CLI, so a test can run mws as
// a real child process (e.g. under a shell whose cwd is a worktree).
const cliEnv = "MWS_TEST_AS_CLI"

func TestMain(m *testing.M) {
	if os.Getenv(cliEnv) == "1" {
		err := Execute(context.Background(), os.Args[1:])
		if err != nil && !Silent(err) {
			PrintError(os.Stderr, err)
		}
		os.Exit(ExitCode(err))
	}
	os.Exit(m.Run())
}

// mwsResult is one in-process invocation of mws.
type mwsResult struct {
	stdout, stderr string
	code           int
}

// lastLine is the final non-empty stdout line: the path contract of the
// commands that print one.
func (r mwsResult) lastLine() string {
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	return lines[len(lines)-1]
}

// runMWS executes `mws <args>` from cwd through the real root command, as an
// agent would run it: stdin is not a terminal, stdout and stderr are
// captured, and the returned error is printed and mapped to the exit code the
// way main does.
func runMWS(t *testing.T, cwd string, args ...string) mwsResult {
	t.Helper()
	return runMWSIn(t, cwd, "", args...)
}

// runMWSIn is runMWS with stdin fed from the given string.
func runMWSIn(t *testing.T, cwd, stdin string, args ...string) mwsResult {
	t.Helper()
	noTerminal(t)
	var out, errb bytes.Buffer
	var err error
	withCwd(t, cwd, func() {
		root := NewRootCmd()
		root.SetIn(strings.NewReader(stdin))
		root.SetOut(&out)
		root.SetErr(&errb)
		err = execute(t.Context(), root, args)
	})
	if err != nil && !Silent(err) {
		PrintError(&errb, err)
	}
	return mwsResult{stdout: out.String(), stderr: errb.String(), code: ExitCode(err)}
}

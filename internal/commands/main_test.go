package commands

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// cliEnv makes the test binary act as the mws CLI, so a test can run mws as
// a real child process (e.g. under a shell whose cwd is a worktree).
const cliEnv = "MWS_TEST_AS_CLI"

func TestMain(m *testing.M) {
	if os.Getenv(cliEnv) == "1" {
		root := NewRootCmd()
		root.SetArgs(os.Args[1:])
		err := root.ExecuteContext(context.Background())
		if err != nil && !Silent(err) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(ExitCode(err))
	}
	os.Exit(m.Run())
}

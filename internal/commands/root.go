// Package commands wires the mws CLI: cobra command definitions, interactive
// prompts, and run-functions that orchestrate the internal/{config,project,git,skeleton}
// packages.
package commands

import (
	"context"
	"strings"

	"github.com/spf13/cobra"
)

const rootLong = `mws spins up an AI-harness layer (meta workspace) around one or more native
git repos, with cheap parallel working copies that share the harness via
symlinks.

Every command runs without prompts when given its arguments and flags. Without
a terminal on stdin, mws never prompts: a missing input is a usage error that
shows the invocation supplying it, and a confirmation needs --yes. Results go
to stdout, progress to stderr; list commands take --json.

Exit codes: 0 ok, 1 failure, 2 usage, 3 refused by a safety check, 4 not found.`

// NewRootCmd builds the mws root cobra command with all subcommands registered.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mws",
		Short: "Manage meta workspaces around native git repos",
		Long:  rootLong,
		Example: `  mws init demo --repo git@github.com:org/api.git --yes
  mws clone feature-x --no-optional --no-setup
  mws wt new 04 --json`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return usagef("%w; see `%s --help`", err, c.CommandPath())
	})

	root.AddCommand(
		newInitCmd(),
		newAddRepoCmd(),
		newCloneCmd(),
		newIncludeCmd(),
		newPromoteCmd(),
		newListCmd(),
		newRmCmd(),
		newRestoreCmd(),
		newTrashCmd(),
		newRelinkCmd(),
		newMigrateCmd(),
		newSyncEnvCmd(),
		newStageEnvCmd(),
		newShellInitCmd(),
		newWorktreeCmd(),
		newVersionCmd(),
	)
	tagUsageArgs(root)

	return root
}

// tagUsageArgs wraps the positional-argument check of c and every command
// below it with usageArgs.
func tagUsageArgs(c *cobra.Command) {
	if c.Args != nil {
		c.Args = usageArgs(c.Args)
	}
	for _, sub := range c.Commands() {
		tagUsageArgs(sub)
	}
}

// Execute runs the mws command line in args. An unknown subcommand is a usage
// error, at the root (cobra reports it as a plain error) and under a command
// group such as `mws wt` (cobra prints the group's help and exits 0).
func Execute(ctx context.Context, args []string) error {
	root := NewRootCmd()
	root.SetArgs(args)
	// Find does not know the help command cobra adds lazily, nor the hidden
	// shell-completion commands; let those through to cobra.
	root.InitDefaultHelpCmd()
	if len(args) > 0 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		return root.ExecuteContext(ctx)
	}
	found, rest, err := root.Find(args)
	if err != nil {
		return usagef("%w; run `mws --help` to list the commands", err)
	}
	if !found.Runnable() && found.HasSubCommands() {
		for _, a := range rest {
			if !strings.HasPrefix(a, "-") {
				return usagef("unknown command %q for %q; run `%s --help` to list its commands", a, found.CommandPath(), found.CommandPath())
			}
		}
	}
	return root.ExecuteContext(ctx)
}

// Package commands wires the mws CLI: cobra command definitions, interactive
// prompts, and run-functions that orchestrate the internal/{config,project,git,skeleton}
// packages.
package commands

import "github.com/spf13/cobra"

// NewRootCmd builds the mws root cobra command with all subcommands registered.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "mws",
		Short:         "Manage meta workspaces around native git repos",
		Long:          "mws spins up an AI-harness layer (meta workspace) around one or more native git repos, with cheap parallel working copies that share the harness via symlinks.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newInitCmd(),
		newAddRepoCmd(),
		newCloneCmd(),
		newIncludeCmd(),
		newPromoteCmd(),
		newListCmd(),
		newRmCmd(),
		newRelinkCmd(),
		newMigrateCmd(),
		newSyncEnvCmd(),
		newStageEnvCmd(),
		newShellInitCmd(),
		newVersionCmd(),
	)

	return root
}

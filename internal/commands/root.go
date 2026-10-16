// Package commands wires the mws CLI: cobra command definitions, interactive
// prompts, and run-functions that orchestrate the internal/{config,project,git,skeleton}
// packages.
package commands

import (
	"context"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/ui"
)

const rootLong = `mws spins up an AI-harness layer (meta workspace) around one or more native
git repos, with cheap parallel working copies that share the harness via
symlinks.

Every command runs without prompts when given its arguments and flags. Without
a terminal on stdin, mws never prompts: a missing input is a usage error that
shows the invocation supplying it, and a confirmation needs --yes. Results go
to stdout, progress to stderr; commands with a result (ls, path, clone, rm,
restore, trash, env, ports, wt) take --json.

Exit codes: 0 ok, 1 failure, 2 usage, 3 refused by a safety check, 4 not found.`

// Command groups, in the order help lists them.
const (
	groupWorkspace = "workspace"
	groupCopies    = "copies"
	groupEnv       = "env"
	groupPorts     = "ports"
	groupWorktrees = "worktrees"
	groupShell     = "shell"
)

// usageTemplate is cobra's default usage template with the section labels
// and command names passed through the style template funcs, so help is
// coloured on a terminal and unchanged elsewhere.
const usageTemplate = `{{heading "Usage:"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{heading "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{heading "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{heading "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{cmdName (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{heading .Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{cmdName (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{heading "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{cmdName (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{heading "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{heading "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

{{heading "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{cmdName (rpad .CommandPath .CommandPathPadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

func init() {
	// Help always goes to stdout, so the styles follow the process's stdout.
	cobra.AddTemplateFunc("heading", func(s string) string { return ui.Detect(os.Stdout).Heading(s) })
	cobra.AddTemplateFunc("cmdName", func(s string) string {
		name := strings.TrimRight(s, " ")
		return ui.Detect(os.Stdout).Bold(name) + s[len(name):]
	})
}

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
	root.SetUsageTemplate(usageTemplate)
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return usagef("%w; see `%s --help`", err, c.CommandPath())
	})
	root.AddGroup(
		&cobra.Group{ID: groupWorkspace, Title: "Workspace:"},
		&cobra.Group{ID: groupCopies, Title: "Working copies:"},
		&cobra.Group{ID: groupEnv, Title: "Env files:"},
		&cobra.Group{ID: groupPorts, Title: "Ports:"},
		&cobra.Group{ID: groupWorktrees, Title: "Worktrees:"},
		&cobra.Group{ID: groupShell, Title: "Shell and info:"},
	)
	root.SetHelpCommandGroupID(groupShell)

	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add(groupWorkspace, newInitCmd(), newAddRepoCmd(), newPromoteCmd(), newRelinkCmd())
	add(groupCopies, newCloneCmd(), newIncludeCmd(), newListCmd(), newPathCmd(), newRmCmd(), newRestoreCmd(), newTrashCmd())
	add(groupEnv, newEnvCmd())
	add(groupPorts, newPortsCmd())
	add(groupWorktrees, newWorktreeCmd())
	add(groupShell, newShellInitCmd(), newVersionCmd())
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
	return execute(ctx, NewRootCmd(), args)
}

// execute is Execute on a caller-built root, so tests can bind its streams.
func execute(ctx context.Context, root *cobra.Command, args []string) error {
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

package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

func newIncludeCmd() *cobra.Command {
	var doSetup, noSetup bool
	cmd := &cobra.Command{
		Use:   "include <folder> [working-copy]",
		Short: "Clone a registered optional repo into a working copy",
		Long: `include clones a repo registered under [[optional_repos]] in .mws.toml into a
working copy (the current one by default, or a named copy). It checks out the
repo's default branch and copies any mapped env files, mirroring 'mws clone'.

Register optional repos first with 'mws add-repo --optional <url> <folder>'.

After clone and env-copy succeed, any [[optional_repos.setup]] commands for the
repo run via sh -c. By default a confirmation prompt summarises them; use
--setup to run without prompting or --no-setup to skip entirely.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			folder := args[0]
			copyName := ""
			if len(args) == 2 {
				copyName = args[1]
			}
			choice := setupAsk
			switch {
			case doSetup:
				choice = setupForceRun
			case noSetup:
				choice = setupSkip
			}
			return runInclude(cmd.Context(), newConsoleReporter(), folder, copyName, choice)
		},
	}
	cmd.Flags().BoolVar(&doSetup, "setup", false, "run [[optional_repos.setup]] commands without prompting")
	cmd.Flags().BoolVar(&noSetup, "no-setup", false, "skip [[optional_repos.setup]] commands without prompting")
	cmd.MarkFlagsMutuallyExclusive("setup", "no-setup")
	return cmd
}

func runInclude(ctx context.Context, r Reporter, folder, copyName string, choice setupChoice) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ws, err := project.Locate(cwd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(ws.MetaRoot)
	if err != nil {
		return err
	}

	repo, ok := cfg.OptionalRepo(strings.TrimSpace(folder))
	if !ok {
		return fmt.Errorf("no optional repo registered with folder %q (registered: %s)\nRegister one with: mws add-repo --optional <url> %s",
			folder, listOptionalFolders(cfg), folder)
	}

	target, err := ws.ResolveCopy(copyName)
	if err != nil {
		return err
	}

	if err := cloneNative(ctx, r, repo, target); err != nil {
		return fmt.Errorf("%s: %w", repo.Folder, err)
	}
	copyEnvsFor(r, ws.MetaRoot, repo, target)

	items := collectSetup([]config.Repo{repo})
	run, err := confirmSetup(choice, items)
	if err != nil {
		return err
	}
	if run {
		if failed := runSetup(ctx, r, target, items, os.Stdout, os.Stderr); len(failed) > 0 {
			return fmt.Errorf("included %s but %d setup command(s) failed:\n  %s",
				repo.Folder, len(failed), strings.Join(failed, "\n  "))
		}
	}

	r.OK(fmt.Sprintf("Included %s in %s", repo.Folder, filepath.Base(target)))
	return nil
}

package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

func newSyncEnvCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync-env [name]",
		Short: "Copy staged env files from .envs/ into a working copy (overwrites)",
		Long: `sync-env reads [[repos.envs]] mappings from .mws.toml and copies each
staged file from <meta>/.envs/<repo>/<source> into <working-copy>/<repo>/<target>.
Targets are overwritten. Missing staged sources are skipped with a warning.

With no argument, defaults to the working copy that contains cwd. Run from
the meta root and pass a name to target a specific copy.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runSyncEnv(newConsoleReporter(), name)
		},
	}
}

func runSyncEnv(r Reporter, name string) error {
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

	target, err := ws.ResolveCopy(name)
	if err != nil {
		return err
	}

	staging := envStagingDir(ws.MetaRoot)
	copied := 0
	for _, repo := range cfg.Repos {
		for _, env := range repo.Envs {
			src := filepath.Join(staging, repo.Folder, env.Source)
			dst := filepath.Join(target, repo.Folder, env.Target)
			if _, err := os.Stat(src); err != nil {
				r.Warn(fmt.Sprintf("%s: staged %s missing, skipping", repo.Folder, env.Source))
				continue
			}
			if err := copyFile(src, dst); err != nil {
				r.Fail(fmt.Sprintf("%s: %s -> %s: %v", repo.Folder, env.Source, env.Target, err))
				continue
			}
			r.OK(fmt.Sprintf("%s: %s -> %s", repo.Folder, env.Source, env.Target))
			copied++
		}
	}
	if copied == 0 {
		r.Info("Nothing to sync.")
	}
	return nil
}

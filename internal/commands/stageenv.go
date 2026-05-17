package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

func newStageEnvCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stage-env [name]",
		Short: "Capture env files from a working copy into .envs/ staging (overwrites)",
		Long: `stage-env is the inverse of sync-env. It reads [[repos.envs]] mappings from
.mws.toml and copies each <working-copy>/<repo>/<target> into
<meta>/.envs/<repo>/<source>. The staged file becomes the default that future
clones (and explicit sync-env calls) will reproduce.

With no argument, defaults to the working copy that contains cwd. Run from
the meta root and pass a name to target a specific copy.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runStageEnv(newConsoleReporter(), name)
		},
	}
}

func runStageEnv(r Reporter, name string) error {
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
	captured := 0
	for _, repo := range cfg.Repos {
		for _, env := range repo.Envs {
			src := filepath.Join(target, repo.Folder, env.Target)
			dst := filepath.Join(staging, repo.Folder, env.Source)
			if _, err := os.Stat(src); err != nil {
				r.Warn(fmt.Sprintf("%s: live %s missing, skipping", repo.Folder, env.Target))
				continue
			}
			if err := copyFile(src, dst); err != nil {
				r.Fail(fmt.Sprintf("%s: %s -> %s: %v", repo.Folder, env.Target, env.Source, err))
				continue
			}
			r.OK(fmt.Sprintf("%s: %s -> %s", repo.Folder, env.Target, env.Source))
			captured++
		}
	}
	if captured == 0 {
		r.Info("Nothing to stage.")
	}
	return nil
}

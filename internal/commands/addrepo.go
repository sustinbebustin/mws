package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
)

func newAddRepoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-repo [url] [folder]",
		Short: "Register a native repo and clone it into every working copy",
		Long: `add-repo appends a native repo to the meta workspace's .mws.toml and clones
it into every existing working copy. If folder is omitted it is derived from
the repo URL. With no arguments, prompts interactively.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoURL := ""
			folder := ""
			if len(args) >= 1 {
				repoURL = args[0]
			}
			if len(args) == 2 {
				folder = args[1]
			}
			return runAddRepo(cmd.Context(), newConsoleReporter(), repoURL, folder)
		},
	}
}

func runAddRepo(ctx context.Context, r Reporter, repoURL, folder string) error {
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

	if repoURL == "" {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("Repo URL").
				Description("git@host:org/name.git or https://host/org/name").
				Validate(nonEmpty).
				Value(&repoURL),
			huh.NewInput().
				Title("Local folder").
				Description("Defaults to the repo name from the URL.").
				Value(&folder),
		)).Run(); err != nil {
			return err
		}
		repoURL = strings.TrimSpace(repoURL)
		folder = strings.TrimSpace(folder)
	}

	if folder == "" {
		folder = deriveFolderFromURL(repoURL)
	}
	if folder == "" {
		return fmt.Errorf("could not derive folder from URL %q; provide one explicitly", repoURL)
	}

	if !cfg.AddRepo(config.Repo{Folder: folder, URL: repoURL}) {
		return fmt.Errorf("repo with folder %q is already registered", folder)
	}
	if err := config.Save(ws.MetaRoot, cfg); err != nil {
		return err
	}
	r.OK(fmt.Sprintf("Registered %s -> %s", folder, repoURL))

	peers, err := project.EnumerateWorkingCopies(ws.MetaRoot)
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		r.Info("No working copies found. Run `mws clone <name>` to create one.")
		return nil
	}

	var failed []string
	for _, peer := range peers {
		target := filepath.Join(peer, folder)
		if _, err := os.Stat(target); err == nil {
			r.Info(fmt.Sprintf("%s: %s already exists, skipping clone", filepath.Base(peer), folder))
			continue
		}
		r.Heading(fmt.Sprintf("Cloning into %s ...", filepath.Base(peer)))
		if err := git.Clone(ctx, repoURL, target); err != nil {
			r.Fail(fmt.Sprintf("%s: %v", filepath.Base(peer), err))
			failed = append(failed, filepath.Base(peer))
			continue
		}
		r.OK(fmt.Sprintf("%s: cloned %s", filepath.Base(peer), folder))
	}
	if len(failed) > 0 {
		return fmt.Errorf("add-repo completed with errors: %d working copy clone(s) failed: %s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

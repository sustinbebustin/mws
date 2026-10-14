package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
)

func newAddRepoCmd() *cobra.Command {
	var optional bool
	cmd := &cobra.Command{
		Use:   "add-repo [url] [folder]",
		Short: "Register a native repo and clone it into every working copy",
		Long: `add-repo appends a native repo to the meta workspace's .mws.toml and clones
it into every existing working copy. If folder is omitted it is derived from
the repo URL. With no arguments, prompts interactively.

With --optional the repo is registered under [[optional_repos]] instead and is
NOT cloned into existing working copies. Pull it into a copy on demand with
'mws clone --with <folder>' (or the clone prompt) or 'mws include <folder>'.

Re-running with the same URL and folder changes no config and clones into any
working copy that still lacks the repo.`,
		Example: `  mws add-repo git@github.com:org/api.git
  mws add-repo https://github.com/org/web frontend
  mws add-repo git@github.com:org/docs.git --optional`,
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
			return runAddRepo(cmd.Context(), newConsoleReporter(), repoURL, folder, optional)
		},
	}
	cmd.Flags().BoolVar(&optional, "optional", false, "register under [[optional_repos]] without cloning into existing working copies")
	return cmd
}

func runAddRepo(ctx context.Context, r Reporter, repoURL, folder string, optional bool) error {
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
		if !canPrompt() {
			return noPrompt("no repo URL given", "mws add-repo <url> [folder]", "mws add-repo <url> [folder] --optional")
		}
		if err := runForm(huh.NewGroup(
			huh.NewInput().
				Title("Repo URL").
				Description("git@host:org/name.git or https://host/org/name").
				Validate(nonEmpty).
				Value(&repoURL),
			huh.NewInput().
				Title("Local folder").
				Description("Defaults to the repo name from the URL.").
				Value(&folder),
		)); err != nil {
			return err
		}
		repoURL = strings.TrimSpace(repoURL)
		folder = strings.TrimSpace(folder)
	}

	if folder == "" {
		folder = deriveFolderFromURL(repoURL)
	}
	if folder == "" {
		return usagef("could not derive a folder from URL %q; pass one: mws add-repo %s <folder>", repoURL, repoURL)
	}

	list := config.ListDefault
	if optional {
		list = config.ListOptional
	}
	switch cfg.Register(config.Repo{Folder: folder, URL: repoURL}, list) {
	case config.AlreadyRegistered:
		// A retry: keep the config and go on cloning into any copy that
		// still lacks the repo.
		r.Info(fmt.Sprintf("%s is already registered as %s", repoURL, folder))
	case config.FolderTaken:
		return clierr.Refusedf("folder %q is already registered, with another URL or in the other repo list; nothing was changed; check .mws.toml in %s or pass another folder", folder, ws.MetaRoot)
	case config.Registered:
		if err := config.Save(ws.MetaRoot, cfg); err != nil {
			return err
		}
		r.OK(fmt.Sprintf("Registered %s -> %s", folder, repoURL))
	}
	if optional {
		r.Info(fmt.Sprintf("Pull it into a working copy with `mws clone --with %s` or `mws include %s`.", folder, folder))
		return nil
	}

	peers, err := ws.EnumerateCopies()
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
		start := time.Now()
		if err := git.Clone(ctx, repoURL, target); err != nil {
			r.Fail(fmt.Sprintf("%s: %v", filepath.Base(peer), err))
			failed = append(failed, filepath.Base(peer))
			continue
		}
		r.Done(fmt.Sprintf("%s: cloned %s", filepath.Base(peer), folder), time.Since(start))
	}
	if len(failed) > 0 {
		return fmt.Errorf("add-repo completed with errors: %d working copy clone(s) failed: %s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

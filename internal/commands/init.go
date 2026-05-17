package commands

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/skeleton"
)

// firstWorkingCopyName is the conventional name for the working copy created
// alongside the meta on `mws init`.
const firstWorkingCopyName = "main"

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [name]",
		Short: "Create a new meta workspace with a first working copy",
		Long: `init creates a meta-at-root workspace: a directory <parent>/<name>/ that
is a git repo at its root, contains the AI harness under .mws/, the mws config
at .mws.toml, an allowlist .gitignore, and a first working copy at <name>/main/.
Prompts collect everything up front, then executes once confirmed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var arg string
			if len(args) == 1 {
				arg = args[0]
			}
			return runInit(cmd.Context(), newConsoleReporter(), arg)
		},
	}
}

type initPlan struct {
	ParentDir   string
	ProjectName string
	Description string
	Repos       []config.Repo
}

func runInit(ctx context.Context, r Reporter, arg string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found on PATH: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	plan, err := collectInitPlan(arg, cwd)
	if err != nil {
		return err
	}

	showSummary(r, plan)

	var confirm bool
	if err := huh.NewConfirm().
		Title("Create meta workspace?").
		Affirmative("Yes").
		Negative("Cancel").
		Value(&confirm).
		Run(); err != nil {
		return err
	}
	if !confirm {
		r.Warn("Cancelled.")
		return nil
	}

	return executeInit(ctx, r, plan)
}

func collectInitPlan(arg, cwd string) (*initPlan, error) {
	p := &initPlan{ParentDir: cwd, ProjectName: arg}

	fields := []huh.Field{}
	if p.ProjectName == "" {
		fields = append(fields, huh.NewInput().
			Title("Project name").
			Description("Used as the meta workspace directory name.").
			Validate(project.ValidateName).
			Value(&p.ProjectName))
	}
	fields = append(fields, huh.NewInput().
		Title("Description").
		Description("Short one-line description -- shown in CLAUDE.md and README.md.").
		Value(&p.Description))
	parent := p.ParentDir
	fields = append(fields, huh.NewInput().
		Title("Parent directory").
		Description("The meta workspace will be created here.").
		Validate(validateExistingDir).
		Value(&parent))

	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return nil, err
	}
	p.ParentDir = strings.TrimSpace(parent)

	repos, err := promptRepoLoop("Register a native repo to clone into the working copy?")
	if err != nil {
		return nil, err
	}
	p.Repos = repos
	return p, nil
}

func promptRepoLoop(prompt string) ([]config.Repo, error) {
	var repos []config.Repo
	for {
		var add bool
		if err := huh.NewConfirm().
			Title(prompt).
			Affirmative("Add another").
			Negative("Done").
			Value(&add).
			Run(); err != nil {
			return nil, err
		}
		if !add {
			return repos, nil
		}
		var repoURL, folder string
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
			return nil, err
		}
		repoURL = strings.TrimSpace(repoURL)
		folder = strings.TrimSpace(folder)
		if folder == "" {
			folder = deriveFolderFromURL(repoURL)
		}
		repos = append(repos, config.Repo{Folder: folder, URL: repoURL})
	}
}

func showSummary(r Reporter, p *initPlan) {
	metaDir := filepath.Join(p.ParentDir, p.ProjectName)
	mainCopy := filepath.Join(metaDir, firstWorkingCopyName)

	r.Heading("\nPlanned actions")
	r.Info(fmt.Sprintf("  meta workspace : %s", metaDir))
	r.Info(fmt.Sprintf("  working copy   : %s", mainCopy))
	r.Info(fmt.Sprintf("  description    : %s", or(p.Description, "(none)")))
	if len(p.Repos) == 0 {
		r.Info("  native repos   : (none)")
	} else {
		r.Info("  native repos   :")
		for _, repo := range p.Repos {
			r.Info(fmt.Sprintf("    - %s  %s", repo.Folder, repo.URL))
		}
	}
}

func executeInit(ctx context.Context, r Reporter, p *initPlan) error {
	metaDir := filepath.Join(p.ParentDir, p.ProjectName)

	if _, err := os.Stat(metaDir); err == nil {
		return fmt.Errorf("path already exists at %s", metaDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	skeletonData := skeleton.Data{
		ProjectName: p.ProjectName,
		Description: p.Description,
		Repos:       p.Repos,
	}

	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}
	if err := skeleton.Render(metaDir, skeletonData); err != nil {
		return err
	}
	if err := config.Save(metaDir, &config.Config{
		ProjectName: p.ProjectName,
		Description: p.Description,
		Repos:       p.Repos,
	}); err != nil {
		return err
	}
	if err := git.InitQuiet(ctx, metaDir); err != nil {
		return err
	}
	r.OK(fmt.Sprintf("Created meta workspace at %s", metaDir))

	mainCopy := filepath.Join(metaDir, firstWorkingCopyName)
	if err := os.MkdirAll(mainCopy, 0o755); err != nil {
		return err
	}
	linked, err := project.LinkHarnessIntoWorkingCopy(metaDir, mainCopy)
	if err != nil {
		return err
	}
	for _, name := range linked {
		r.OK(fmt.Sprintf("Linked %s/%s", firstWorkingCopyName, name))
	}

	var failed []string
	for _, repo := range p.Repos {
		target := filepath.Join(mainCopy, repo.Folder)
		if _, err := os.Stat(target); err == nil {
			r.Info(fmt.Sprintf("Native repo %s already present, skipping clone", repo.Folder))
			continue
		}
		if repo.URL == "" {
			r.Warn(fmt.Sprintf("Native repo %s has no URL configured, skipping clone", repo.Folder))
			continue
		}
		r.Heading(fmt.Sprintf("Cloning %s ...", repo.Folder))
		if err := git.Clone(ctx, repo.URL, target); err != nil {
			r.Fail(fmt.Sprintf("%s: %v", repo.Folder, err))
			failed = append(failed, repo.Folder)
			continue
		}
		r.OK(fmt.Sprintf("Cloned %s", repo.Folder))
	}

	if anyEnvMapping(p.Repos) {
		r.Info(fmt.Sprintf("Populate env files inside %s and run `mws stage-env %s` to capture them into staging.", mainCopy, firstWorkingCopyName))
	}
	if len(failed) > 0 {
		return fmt.Errorf("init completed with errors: %d repo(s) failed to clone: %s", len(failed), strings.Join(failed, ", "))
	}
	r.OK(fmt.Sprintf("Done. cd %s to start working.", mainCopy))
	return nil
}

func anyEnvMapping(repos []config.Repo) bool {
	for _, r := range repos {
		if len(r.Envs) > 0 {
			return true
		}
	}
	return false
}

func validateExistingDir(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("path is required")
	}
	st, err := os.Stat(s)
	if err != nil {
		return fmt.Errorf("stat %s: %w", s, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("not a directory: %s", s)
	}
	return nil
}

func nonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("value is required")
	}
	return nil
}

func deriveFolderFromURL(repoURL string) string {
	repoURL = strings.TrimSuffix(repoURL, ".git")
	// SSH form: git@host:org/name -> name
	if i := strings.LastIndex(repoURL, ":"); i >= 0 {
		repoURL = repoURL[i+1:]
	}
	if u, err := url.Parse(repoURL); err == nil && u.Path != "" {
		repoURL = u.Path
	}
	repoURL = strings.Trim(repoURL, "/")
	if i := strings.LastIndex(repoURL, "/"); i >= 0 {
		return repoURL[i+1:]
	}
	return repoURL
}

func or(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

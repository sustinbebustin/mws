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

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [name]",
		Short: "Create a new meta workspace and working copy",
		Long: `init creates a sibling-meta layout: a meta workspace directory holding the AI
harness (.claude, .workspace, CLAUDE.md, ...) and an adjacent working copy that
symlinks back into it. Prompts collect everything up front, then executes once
confirmed.`,
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
	Brownfield  bool
}

func runInit(ctx context.Context, r Reporter, arg string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found on PATH: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	plan, err := collectInitPlan(r, arg, cwd)
	if err != nil {
		return err
	}

	showSummary(r, plan)

	var confirm bool
	if err := huh.NewConfirm().
		Title("Create meta workspace and working copy?").
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

func collectInitPlan(r Reporter, arg, cwd string) (*initPlan, error) {
	p := &initPlan{ParentDir: cwd, ProjectName: arg}

	if err := huh.NewConfirm().
		Title("Are there existing native repos to bring under mws?").
		Description("Yes: import repos already cloned somewhere. No: start fresh.").
		Affirmative("Yes (brownfield)").
		Negative("No (greenfield)").
		Value(&p.Brownfield).
		Run(); err != nil {
		return nil, err
	}

	if p.Brownfield {
		return collectBrownfieldPlan(r, p)
	}
	return collectGreenfieldPlan(p)
}

func collectGreenfieldPlan(p *initPlan) (*initPlan, error) {
	fields := []huh.Field{}
	if p.ProjectName == "" {
		fields = append(fields, huh.NewInput().
			Title("Project name").
			Description("Used as the working-copy and meta-workspace directory base name.").
			Validate(validateProjectName).
			Value(&p.ProjectName))
	}
	fields = append(fields, huh.NewInput().
		Title("Description").
		Description("Short one-line description -- ends up in CLAUDE.md and README.md.").
		Value(&p.Description))
	parent := p.ParentDir
	fields = append(fields, huh.NewInput().
		Title("Parent directory").
		Description("Both the meta and the working copy will be created here.").
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

func collectBrownfieldPlan(r Reporter, p *initPlan) (*initPlan, error) {
	workingCopy := p.ParentDir
	fields := []huh.Field{
		huh.NewInput().
			Title("Working copy path").
			Description("The directory that already contains your native repo clones. Will become a working copy.").
			Validate(validateExistingDir).
			Value(&workingCopy),
	}
	if p.ProjectName == "" {
		fields = append(fields, huh.NewInput().
			Title("Project name").
			Description("Used as the meta-workspace directory base name (working copy keeps its name).").
			Validate(validateProjectName).
			Value(&p.ProjectName))
	}
	fields = append(fields, huh.NewInput().
		Title("Description").
		Value(&p.Description))

	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return nil, err
	}

	workingCopy = strings.TrimSpace(workingCopy)
	abs, err := filepath.Abs(workingCopy)
	if err != nil {
		return nil, err
	}
	p.ParentDir = filepath.Dir(abs)
	if p.ProjectName == "" {
		p.ProjectName = filepath.Base(abs)
	}

	detected, err := detectNativeRepos(abs)
	if err != nil {
		return nil, err
	}

	if len(detected) == 0 {
		r.Info("No git repos detected in working copy. You can add some later with `mws add-repo`.")
		extras, err := promptRepoLoop("Add another native repo to register?")
		if err != nil {
			return nil, err
		}
		p.Repos = extras
		return p, nil
	}

	var selected []string
	if err := huh.NewMultiSelect[string]().
		Title("Select native repos to register").
		Options(detectedToOptions(detected)...).
		Value(&selected).
		Run(); err != nil {
		return nil, err
	}

	for _, name := range selected {
		repoURL := detected[name]
		p.Repos = append(p.Repos, config.Repo{Folder: name, URL: repoURL})
	}

	extras, err := promptRepoLoop("Register an additional native repo?")
	if err != nil {
		return nil, err
	}
	p.Repos = append(p.Repos, extras...)
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

func detectedToOptions(m map[string]string) []huh.Option[string] {
	var opts []huh.Option[string]
	for name, repoURL := range m {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s  (%s)", name, repoURL), name).Selected(true))
	}
	return opts
}

// detectNativeRepos returns a map of folder name -> origin URL for direct subdirectories of root
// that contain a .git directory. Subdirs without an origin are still included with an empty URL.
func detectNativeRepos(root string) (map[string]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		gitDir := filepath.Join(root, e.Name(), ".git")
		if _, err := os.Stat(gitDir); err != nil {
			continue
		}
		out[e.Name()] = readOriginURL(filepath.Join(root, e.Name()))
	}
	return out, nil
}

func readOriginURL(repoDir string) string {
	// Best-effort: ignore errors, return empty when no origin.
	cmd := exec.Command("git", "-C", repoDir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func showSummary(r Reporter, p *initPlan) {
	metaDir := filepath.Join(p.ParentDir, project.MetaDirName(p.ProjectName))
	workingCopy := filepath.Join(p.ParentDir, p.ProjectName)

	r.Heading("\nPlanned actions")
	r.Info(fmt.Sprintf("  meta workspace : %s", metaDir))
	r.Info(fmt.Sprintf("  working copy   : %s", workingCopy))
	r.Info(fmt.Sprintf("  description    : %s", or(p.Description, "(none)")))
	if len(p.Repos) == 0 {
		r.Info("  native repos   : (none)")
	} else {
		r.Info("  native repos   :")
		for _, repo := range p.Repos {
			r.Info(fmt.Sprintf("    - %s  %s", repo.Folder, repo.URL))
		}
	}
	mode := "greenfield"
	if p.Brownfield {
		mode = "brownfield"
	}
	r.Info(fmt.Sprintf("  mode           : %s\n", mode))
}

func executeInit(ctx context.Context, r Reporter, p *initPlan) error {
	metaDir := filepath.Join(p.ParentDir, project.MetaDirName(p.ProjectName))
	workingCopy := filepath.Join(p.ParentDir, p.ProjectName)

	if _, err := os.Stat(metaDir); err == nil {
		return fmt.Errorf("meta workspace already exists at %s", metaDir)
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

	if _, err := os.Stat(workingCopy); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(workingCopy, 0o755); err != nil {
			return err
		}
	}

	linked, err := project.LinkMetaIntoWorkingCopy(metaDir, workingCopy)
	if err != nil {
		return err
	}
	for _, name := range linked {
		r.OK(fmt.Sprintf("Linked %s -> ../%s/%s", filepath.Join(p.ProjectName, name), filepath.Base(metaDir), name))
	}

	for _, repo := range p.Repos {
		target := filepath.Join(workingCopy, repo.Folder)
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
			continue
		}
		r.OK(fmt.Sprintf("Cloned %s", repo.Folder))
	}

	r.OK(fmt.Sprintf("Done. cd %s to start working.", workingCopy))
	return nil
}

// validateProjectName accepts a single path-safe segment: ASCII letters/digits/_-./
// must start with a letter or digit, must not start with '.' or '-', and must not
// contain path separators, spaces, or control characters.
func validateProjectName(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("project name is required")
	}
	if strings.HasPrefix(s, ".") {
		return errors.New("must not start with '.'")
	}
	if strings.HasPrefix(s, "-") {
		return errors.New("must not start with '-'")
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return fmt.Errorf("invalid character %q (allowed: letters, digits, '-', '_', '.')", r)
		}
	}
	return nil
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

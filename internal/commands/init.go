package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/skeleton"
	"github.com/sustinbebustin/mws/internal/ui"
)

// firstWorkingCopyName is the conventional name for the working copy created
// alongside the meta on `mws init`.
const firstWorkingCopyName = "main"

// initFlags holds `mws init` input given on the command line. A nil value was
// not given: it is prompted for on a terminal and defaulted without one.
type initFlags struct {
	description, copiesDir, parent *string
	repos                          []string
	yes                            bool
}

// givenString returns the value of the string flag name, or nil when it was
// not set on the command line.
func givenString(cmd *cobra.Command, name string) *string {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	v, err := cmd.Flags().GetString(name)
	if err != nil {
		return nil
	}
	return &v
}

func newInitCmd() *cobra.Command {
	var f initFlags
	cmd := &cobra.Command{
		Use:   "init [name]",
		Short: "Create a new meta workspace with a first working copy",
		Long: `init creates a meta-at-root workspace: a directory <parent>/<name>/ that
is a git repo at its root, contains the AI harness under .mws/, the mws config
at .mws.toml, an allowlist .gitignore, and a first working copy at <name>/main/.
It prints the first working copy's path as the last stdout line.

On a terminal, init prompts for every value not given as an argument or flag,
then asks for confirmation. Without one, unset values take their defaults (no
description, no copies subdir, the current directory as parent, no repos) and
--yes is required.

--repo takes a clone URL, or <folder>=<url> to choose the local folder; it is
repeatable.`,
		Example: `  mws init demo
  mws init demo --repo git@github.com:org/api.git --repo web=https://github.com/org/web --yes
  mws init demo --description "Demo tooling" --copies-dir copies --parent ~/dev --yes`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var arg string
			if len(args) == 1 {
				arg = args[0]
			}
			f.description = givenString(cmd, "description")
			f.copiesDir = givenString(cmd, "copies-dir")
			f.parent = givenString(cmd, "parent")
			return runInit(cmd.Context(), newConsoleReporter(), cmd.OutOrStdout(), arg, f)
		},
	}
	cmd.Flags().String("description", "", "one-line description shown in CLAUDE.md and README.md")
	cmd.Flags().String("copies-dir", "", "subdirectory for working copies (default: directly under the meta root)")
	cmd.Flags().String("parent", "", "directory to create the meta workspace in (default: the current directory)")
	cmd.Flags().StringArrayVar(&f.repos, "repo", nil, "native repo to register and clone: <url> or <folder>=<url> (repeatable)")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "skip confirmation")
	return cmd
}

type initPlan struct {
	ParentDir        string
	ProjectName      string
	Description      string
	WorkingCopiesDir string
	Repos            []config.Repo
}

func (p *initPlan) metaDir() string { return filepath.Join(p.ParentDir, p.ProjectName) }

func (p *initPlan) mainCopy() string {
	return filepath.Join(p.metaDir(), p.WorkingCopiesDir, firstWorkingCopyName)
}

func runInit(ctx context.Context, r Reporter, out io.Writer, arg string, f initFlags) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found on PATH: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	plan, err := collectInitPlan(arg, cwd, f)
	if err != nil {
		return err
	}

	showSummary(r, plan)

	ok, err := confirm(r, f.yes, confirmation{
		title:       "Create meta workspace?",
		affirmative: "Yes",
		missing:     "init needs confirmation",
		try:         []string{fmt.Sprintf("mws init %s --yes", plan.ProjectName)},
	})
	if err != nil || !ok {
		return err
	}

	if err := executeInit(ctx, r, plan); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, plan.mainCopy())
	return err
}

// collectInitPlan builds the plan from arg and f, prompting on a terminal for
// every value f does not set.
func collectInitPlan(arg, cwd string, f initFlags) (*initPlan, error) {
	p := &initPlan{ParentDir: cwd, ProjectName: arg}
	if f.parent != nil {
		p.ParentDir = *f.parent
	}
	if f.description != nil {
		p.Description = *f.description
	}
	if f.copiesDir != nil {
		p.WorkingCopiesDir = *f.copiesDir
	}
	interactive := canPrompt()
	if p.ProjectName == "" && !interactive {
		return nil, noPrompt("no project name given", "mws init <name> --yes")
	}

	var fields []huh.Field
	if p.ProjectName == "" {
		fields = append(fields, huh.NewInput().
			Title("Project name").
			Description("Used as the meta workspace directory name.").
			Validate(project.ValidateName).
			Value(&p.ProjectName))
	}
	if interactive && f.description == nil {
		fields = append(fields, huh.NewInput().
			Title("Description").
			Description("Short one-line description -- shown in CLAUDE.md and README.md.").
			Value(&p.Description))
	}
	if interactive && f.copiesDir == nil {
		fields = append(fields, huh.NewInput().
			Title("Subdirectory for working copies (optional)").
			Description("Leave empty to keep working copies directly under the meta root.").
			Validate(project.ValidateWorkingCopiesDir).
			Value(&p.WorkingCopiesDir))
	}
	if interactive && f.parent == nil {
		fields = append(fields, huh.NewInput().
			Title("Parent directory").
			Description("The meta workspace will be created here.").
			Validate(validateExistingDir).
			Value(&p.ParentDir))
	}
	if len(fields) > 0 {
		if err := runForm(huh.NewGroup(fields...)); err != nil {
			return nil, err
		}
	}
	p.ProjectName = strings.TrimSpace(p.ProjectName)
	p.ParentDir = strings.TrimSpace(p.ParentDir)
	p.WorkingCopiesDir = strings.TrimSpace(p.WorkingCopiesDir)
	if err := project.ValidateName(p.ProjectName); err != nil {
		return nil, usagef("project name %q: %w", p.ProjectName, err)
	}
	if err := validateExistingDir(p.ParentDir); err != nil {
		return nil, usagef("--parent: %w", err)
	}
	abs, err := filepath.Abs(p.ParentDir)
	if err != nil {
		return nil, usagef("--parent %q: %w", p.ParentDir, err)
	}
	p.ParentDir = abs
	if err := project.ValidateWorkingCopiesDir(p.WorkingCopiesDir); err != nil {
		return nil, usagef("--copies-dir %q: %w", p.WorkingCopiesDir, err)
	}

	switch {
	case len(f.repos) > 0:
		seen := map[string]string{}
		for _, spec := range f.repos {
			repo, err := parseRepoFlag(spec)
			if err != nil {
				return nil, err
			}
			if prev, ok := seen[repo.Folder]; ok {
				return nil, usagef("--repo %q and --repo %q both use folder %q; give one of them another folder with --repo <folder>=<url>", prev, spec, repo.Folder)
			}
			seen[repo.Folder] = spec
			p.Repos = append(p.Repos, repo)
		}
	case interactive:
		repos, err := promptRepoLoop("Register a native repo to clone into the working copy?")
		if err != nil {
			return nil, err
		}
		p.Repos = repos
	}
	return p, nil
}

// parseRepoFlag reads one --repo value: a clone URL, or <folder>=<url>. The
// folder form is recognised only when the part before the first "=" is a valid
// working-copy entry name, which a URL's scheme or host never is.
func parseRepoFlag(spec string) (config.Repo, error) {
	spec = strings.TrimSpace(spec)
	if folder, repoURL, ok := strings.Cut(spec, "="); ok && !strings.ContainsAny(folder, ":/@") && project.ValidateName(folder) == nil {
		if strings.TrimSpace(repoURL) == "" {
			return config.Repo{}, usagef("--repo %q: no URL after %q", spec, folder+"=")
		}
		return config.Repo{Folder: folder, URL: strings.TrimSpace(repoURL)}, nil
	}
	if spec == "" {
		return config.Repo{}, usagef("--repo needs a clone URL, e.g. --repo git@github.com:org/api.git")
	}
	folder := deriveFolderFromURL(spec)
	if err := project.ValidateName(folder); err != nil {
		return config.Repo{}, usagef("--repo %q: the folder derived from the URL, %q, is not usable (%v); pass --repo <folder>=<url>", spec, folder, err)
	}
	return config.Repo{Folder: folder, URL: spec}, nil
}

func promptRepoLoop(prompt string) ([]config.Repo, error) {
	var repos []config.Repo
	for {
		var add bool
		if err := runField(huh.NewConfirm().
			Title(prompt).
			Affirmative("Add another").
			Negative("Done").
			Value(&add)); err != nil {
			return nil, err
		}
		if !add {
			return repos, nil
		}
		var repoURL, folder string
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
	rows := []ui.KV{
		{Key: "meta workspace", Value: p.metaDir()},
		{Key: "working copy", Value: p.mainCopy()},
	}
	if p.WorkingCopiesDir != "" {
		rows = append(rows, ui.KV{Key: "copies subdir", Value: p.WorkingCopiesDir + "/"})
	}
	rows = append(rows, ui.KV{Key: "description", Value: or(p.Description, "(none)")})
	repos := ui.KV{Key: "native repos", Value: "(none)"}
	if len(p.Repos) > 0 {
		repos.Value = ""
		for _, repo := range p.Repos {
			repos.Items = append(repos.Items, fmt.Sprintf("- %s  %s", repo.Folder, repo.URL))
		}
	}
	r.Summary("Planned actions", append(rows, repos))
}

func executeInit(ctx context.Context, r Reporter, p *initPlan) error {
	// Validate before any on-disk writes: an invalid value here would otherwise
	// leave a half-rendered meta directory behind after the error returns.
	if err := project.ValidateWorkingCopiesDir(p.WorkingCopiesDir); err != nil {
		return fmt.Errorf("invalid working_copies_dir %q: %w", p.WorkingCopiesDir, err)
	}

	metaDir := p.metaDir()

	if _, err := os.Stat(metaDir); err == nil {
		return fmt.Errorf("path already exists at %s; choose another name or --parent", metaDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := git.CheckIdentity(ctx, p.ParentDir); err != nil {
		return fmt.Errorf("cannot commit the new meta workspace: git has no user identity, and nothing was created.\n"+
			"Set one, then re-run:\n"+
			"  git config --global user.name \"Your Name\"\n"+
			"  git config --global user.email \"you@example.com\"\n"+
			"(detail: %v)", err)
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
		ProjectName:      p.ProjectName,
		Description:      p.Description,
		WorkingCopiesDir: p.WorkingCopiesDir,
		Repos:            p.Repos,
	}); err != nil {
		return err
	}
	if err := git.InitQuiet(ctx, metaDir); err != nil {
		return err
	}
	// Without a commit HEAD is unborn and `mws wt new` cannot base a worktree
	// anchor on it. The .gitignore allowlist limits this to the meta files.
	if err := git.CommitAll(ctx, metaDir, "chore: initialize meta workspace"); err != nil {
		return err
	}
	r.OK(fmt.Sprintf("Created meta workspace at %s", metaDir))

	mainCopy := p.mainCopy()
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
		start := time.Now()
		if err := git.Clone(ctx, repo.URL, target); err != nil {
			r.Fail(fmt.Sprintf("%s: %v", repo.Folder, err))
			failed = append(failed, repo.Folder)
			continue
		}
		r.Done(fmt.Sprintf("Cloned %s", repo.Folder), time.Since(start))
	}

	if anyEnvMapping(p.Repos) {
		r.Info(fmt.Sprintf("Populate env files inside %s and run `mws env stage %s` to capture them into staging.", mainCopy, firstWorkingCopyName))
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

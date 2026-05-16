package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/skeleton"
)

func newMigrateCmd() *cobra.Command {
	yes := false
	cmd := &cobra.Command{
		Use:   "migrate <path>",
		Short: "Convert a meta-at-root layout into sibling-meta layout",
		Long: `migrate detects a directory whose top level mixes harness content (.claude,
.workspace, CLAUDE.md, ...) with native git repos as subdirs, and moves the
harness into a new sibling <name>-meta/ directory. The original directory
becomes a working copy: every meta entry is symlinked back, native repos stay
in place, and a .mws/config.toml is initialized with the detected native repos.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var arg string
			if len(args) == 1 {
				arg = args[0]
			}
			return runMigrate(cmd.Context(), newConsoleReporter(), arg, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	return cmd
}

type migratePlan struct {
	Source         string
	ProjectName    string
	Description    string
	NativeRepoDirs []string
	NativeRepos    []config.Repo
	MetaEntries    []string
}

func runMigrate(ctx context.Context, r Reporter, arg string, yes bool) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found on PATH: %w", err)
	}
	if arg == "" {
		if err := huh.NewInput().
			Title("Path to migrate").
			Description("Directory whose top level mixes harness content with native git repos.").
			Validate(validateExistingDir).
			Value(&arg).
			Run(); err != nil {
			return err
		}
	}
	source, err := filepath.Abs(arg)
	if err != nil {
		return err
	}
	st, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("stat %s: %w", source, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("not a directory: %s", source)
	}

	plan, err := planMigrate(r, source)
	if err != nil {
		return err
	}

	r.Heading("Planned migration")
	r.Info(fmt.Sprintf("  source         : %s", plan.Source))
	r.Info(fmt.Sprintf("  meta workspace : %s", filepath.Join(filepath.Dir(plan.Source), project.MetaDirName(plan.ProjectName))))
	r.Info("  native repos stay in place:")
	for _, repo := range plan.NativeRepos {
		r.Info(fmt.Sprintf("    - %s  %s", repo.Folder, repo.URL))
	}
	r.Info("  entries to move into meta:")
	for _, name := range plan.MetaEntries {
		r.Info("    - " + name)
	}

	if !yes {
		var ok bool
		if err := huh.NewConfirm().
			Title("Proceed with migration?").
			Affirmative("Migrate").
			Negative("Cancel").
			Value(&ok).
			Run(); err != nil {
			return err
		}
		if !ok {
			r.Warn("Cancelled.")
			return nil
		}
	}

	return executeMigrate(ctx, r, plan)
}

func planMigrate(r Reporter, source string) (*migratePlan, error) {
	name := filepath.Base(source)
	parent := filepath.Dir(source)
	metaDir := filepath.Join(parent, project.MetaDirName(name))
	if _, err := os.Stat(metaDir); err == nil {
		return nil, fmt.Errorf("target meta workspace already exists at %s", metaDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	entries, err := os.ReadDir(source)
	if err != nil {
		return nil, err
	}

	plan := &migratePlan{Source: source, ProjectName: name}
	for _, e := range entries {
		path := filepath.Join(source, e.Name())
		if e.IsDir() && hasGitDir(path) {
			plan.NativeRepoDirs = append(plan.NativeRepoDirs, e.Name())
			plan.NativeRepos = append(plan.NativeRepos, config.Repo{
				Folder: e.Name(),
				URL:    readOriginURL(path),
			})
			continue
		}
		plan.MetaEntries = append(plan.MetaEntries, e.Name())
	}

	if len(plan.MetaEntries) == 0 {
		return nil, fmt.Errorf("no harness content found in %s; nothing to migrate", source)
	}
	if len(plan.NativeRepos) == 0 {
		r.Warn(fmt.Sprintf("%s contains no native git repos; migration will still produce a meta workspace.", source))
	}
	return plan, nil
}

func executeMigrate(ctx context.Context, r Reporter, p *migratePlan) error {
	parent := filepath.Dir(p.Source)
	metaDir := filepath.Join(parent, project.MetaDirName(p.ProjectName))

	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}

	for _, name := range p.MetaEntries {
		src := filepath.Join(p.Source, name)
		dst := filepath.Join(metaDir, name)
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("move %s -> %s: %w", src, dst, err)
		}
	}
	r.OK(fmt.Sprintf("Moved %d entries into %s", len(p.MetaEntries), metaDir))

	// Render any skeleton-only docs that didn't exist (CLAUDE.md, README.md, .mws/, etc.).
	// We only fill *gaps* -- never overwrite migrated content.
	data := skeleton.Data{
		ProjectName: p.ProjectName,
		Repos:       p.NativeRepos,
	}
	if err := renderSkeletonGaps(metaDir, data); err != nil {
		return err
	}

	cfg, err := config.Load(metaDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("load existing config at %s: %w", metaDir, err)
		}
		cfg = &config.Config{ProjectName: p.ProjectName, Description: p.Description}
	}
	if cfg.ProjectName == "" {
		cfg.ProjectName = p.ProjectName
	}
	for _, repo := range p.NativeRepos {
		cfg.AddRepo(repo)
	}
	if err := config.Save(metaDir, cfg); err != nil {
		return err
	}

	if !hasGitDir(metaDir) {
		if err := git.InitQuiet(ctx, metaDir); err != nil {
			return err
		}
	}
	r.OK(fmt.Sprintf("Meta workspace ready at %s", metaDir))

	linked, err := project.LinkMetaIntoWorkingCopy(metaDir, p.Source)
	if err != nil {
		return err
	}
	for _, name := range linked {
		r.OK(fmt.Sprintf("Linked %s", filepath.Join(filepath.Base(p.Source), name)))
	}
	return nil
}

// renderSkeletonGaps renders the embedded skeleton into metaDir but skips any file that already exists.
func renderSkeletonGaps(metaDir string, data skeleton.Data) error {
	staging, err := os.MkdirTemp("", "mws-skel-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := skeleton.Render(staging, data); err != nil {
		return err
	}
	return filepath.Walk(staging, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == staging {
			return nil
		}
		rel, _ := filepath.Rel(staging, path)
		dst := filepath.Join(metaDir, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if _, err := os.Stat(dst); err == nil {
			return nil // gap-fill only
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode().Perm())
	})
}

func hasGitDir(p string) bool {
	st, err := os.Stat(filepath.Join(p, ".git"))
	return err == nil && st.IsDir()
}

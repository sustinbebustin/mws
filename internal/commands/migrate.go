package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

// legacyMetaSuffix is the directory suffix the old sibling-meta layout used.
const legacyMetaSuffix = "-meta"

// legacyConfigName is the config file name in the old sibling-meta layout.
const legacyConfigName = "config.toml"

func newMigrateCmd() *cobra.Command {
	yes := false
	cmd := &cobra.Command{
		Use:   "migrate <path>",
		Short: "Convert an old sibling-meta layout to meta-at-root",
		Long: `migrate converts a workspace that uses the original sibling-meta layout
(<parent>/<name>-meta/ alongside <parent>/<name>/ and <parent>/<name>-<peer>/)
to the new meta-at-root layout (<parent>/<name>/ as a git repo with
.mws/, .mws.toml, and working copies as untracked children).

It moves the old meta's .git/ to the new meta root, relocates the harness
into .mws/, renames the config to .mws.toml, writes an allowlist .gitignore,
and rebuilds every peer's symlinks against the new harness location. Native
repos inside working copies are left alone -- they move with their parent
dir during the rename.

Pass either the old meta dir (<parent>/<name>-meta/) or any working copy
of it; migrate figures out the rest.`,
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
	OldMeta     string            // <parent>/<name>-meta/
	NewMeta     string            // <parent>/<name>/
	ProjectName string            // derived from old config or old meta basename
	OldPeers    []string          // absolute paths of old peer working copies
	Renames     map[string]string // OldPeers path -> new subdir name inside NewMeta
}

func runMigrate(ctx context.Context, r Reporter, arg string, yes bool) error {
	_ = ctx
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found on PATH: %w", err)
	}
	if arg == "" {
		if err := huh.NewInput().
			Title("Path to migrate").
			Description("Path to the old <name>-meta directory or any of its working copies.").
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

	plan, err := planMigrate(source)
	if err != nil {
		return err
	}

	r.Heading("Planned migration")
	r.Info(fmt.Sprintf("  old meta : %s", plan.OldMeta))
	r.Info(fmt.Sprintf("  new meta : %s", plan.NewMeta))
	if len(plan.OldPeers) == 0 {
		r.Info("  peers    : (none)")
	} else {
		r.Info("  peers:")
		for _, peer := range plan.OldPeers {
			r.Info(fmt.Sprintf("    %s -> %s/", peer, plan.Renames[peer]))
		}
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

	return executeMigrate(r, plan)
}

// planMigrate accepts a path to either an old <name>-meta/ dir or one of its
// working copies, discovers the meta and peers, and computes the new layout.
func planMigrate(source string) (*migratePlan, error) {
	oldMeta, err := findOldMeta(source)
	if err != nil {
		return nil, err
	}

	projectName := strings.TrimSuffix(filepath.Base(oldMeta), legacyMetaSuffix)
	cfg, err := loadLegacyConfig(oldMeta)
	if err != nil {
		return nil, fmt.Errorf("read legacy config at %s: %w", oldMeta, err)
	}
	if cfg.ProjectName != "" {
		projectName = cfg.ProjectName
	}
	if projectName == "" {
		return nil, fmt.Errorf("could not derive project name from %s: legacy config has no project_name and dir name yields empty after stripping %q", oldMeta, legacyMetaSuffix)
	}

	parent := filepath.Dir(oldMeta)
	newMeta := filepath.Join(parent, projectName)
	if _, err := os.Stat(newMeta); err == nil {
		// The bare working copy at <parent>/<projectName>/ will be renamed to main/ during
		// migration -- a final-shape conflict, not a precondition error. Only fail if the
		// path exists AND isn't a recognizable peer.
		if !isOldPeerOf(oldMeta, newMeta) {
			return nil, fmt.Errorf("target meta path %s exists and is not a peer of %s", newMeta, oldMeta)
		}
	}

	peers, err := findOldPeers(oldMeta)
	if err != nil {
		return nil, err
	}

	renames := map[string]string{}
	owners := map[string]string{} // proposed name -> peer that claimed it
	for _, peer := range peers {
		proposed := proposePeerName(projectName, filepath.Base(peer))
		if proposed == "" {
			proposed = filepath.Base(peer)
		}
		if other, taken := owners[proposed]; taken {
			return nil, fmt.Errorf("peers %s and %s both map to working-copy name %q; rename one before migrating", other, peer, proposed)
		}
		owners[proposed] = peer
		renames[peer] = proposed
	}

	return &migratePlan{
		OldMeta:     oldMeta,
		NewMeta:     newMeta,
		ProjectName: projectName,
		OldPeers:    peers,
		Renames:     renames,
	}, nil
}

// findOldMeta returns the absolute path to the legacy meta dir given any path
// that is either the meta itself or one of its working copies.
func findOldMeta(start string) (string, error) {
	st, err := os.Stat(start)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", start, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("not a directory: %s", start)
	}
	// Case 1: start IS the old meta -- has .mws/config.toml as a regular file.
	if isOldMeta(start) {
		return start, nil
	}
	// Case 2: start is a working copy -- .mws is a symlink whose parent (after EvalSymlinks)
	// is the old meta dir.
	mwsPath := filepath.Join(start, project.HarnessDirName)
	st, err = os.Lstat(mwsPath)
	if err == nil && st.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(mwsPath)
		if err == nil {
			meta := filepath.Dir(resolved)
			if isOldMeta(meta) {
				return meta, nil
			}
		}
	}
	return "", fmt.Errorf("could not locate an old sibling-meta workspace from %s", start)
}

// isOldMeta reports whether dir is the old-style meta. The dir's basename must
// end in legacyMetaSuffix ("-meta"), <dir>/.mws/ must be a REAL directory (not
// a symlink), and <dir>/.mws/config.toml must be a regular file. The suffix
// check stops migrate from operating on an unrelated tree that happens to
// contain a .mws/config.toml.
func isOldMeta(dir string) bool {
	if !strings.HasSuffix(filepath.Base(dir), legacyMetaSuffix) {
		return false
	}
	mws := filepath.Join(dir, project.HarnessDirName)
	st, err := os.Lstat(mws)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return false
	}
	cfg, err := os.Stat(filepath.Join(mws, legacyConfigName))
	return err == nil && cfg.Mode().IsRegular()
}

// isOldPeerOf reports whether candidate is an old-style working copy of oldMeta.
func isOldPeerOf(oldMeta, candidate string) bool {
	if candidate == oldMeta {
		return false
	}
	st, err := os.Lstat(candidate)
	if err != nil || !st.IsDir() {
		return false
	}
	peerMws := filepath.Join(candidate, project.HarnessDirName)
	peerTarget, err := filepath.EvalSymlinks(peerMws)
	if err != nil {
		return false
	}
	metaTarget, err := filepath.EvalSymlinks(filepath.Join(oldMeta, project.HarnessDirName))
	if err != nil {
		return false
	}
	return peerTarget == metaTarget
}

// findOldPeers returns sorted paths of working copies in the same parent dir as oldMeta.
func findOldPeers(oldMeta string) ([]string, error) {
	parent := filepath.Dir(oldMeta)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, fmt.Errorf("read parent dir %s: %w", parent, err)
	}
	var peers []string
	for _, e := range entries {
		candidate := filepath.Join(parent, e.Name())
		if isOldPeerOf(oldMeta, candidate) {
			peers = append(peers, candidate)
		}
	}
	sort.Strings(peers)
	return peers, nil
}

// proposePeerName converts an old peer dir name to its new subdir name inside the new meta.
//
//	<projectName>                   -> "main"          (bare working copy)
//	<projectName>-<suffix>          -> "<suffix>"      (suffixed peer)
//	(anything else)                 -> peer name unchanged
func proposePeerName(projectName, peerBase string) string {
	if peerBase == projectName {
		return firstWorkingCopyName
	}
	if rest, ok := strings.CutPrefix(peerBase, projectName+"-"); ok {
		return rest
	}
	return peerBase
}

// loadLegacyConfig reads the old <meta>/.mws/config.toml file.
func loadLegacyConfig(oldMeta string) (*config.Config, error) {
	legacyPath := filepath.Join(oldMeta, project.HarnessDirName, legacyConfigName)
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return nil, err
	}
	var c config.Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func executeMigrate(r Reporter, p *migratePlan) error {
	parent := filepath.Dir(p.OldMeta)
	staging := filepath.Join(parent, fmt.Sprintf(".%s-mws-migrate-%d", p.ProjectName, os.Getpid()))
	if _, err := os.Stat(staging); err == nil {
		return fmt.Errorf("staging dir %s already exists; remove it and retry", staging)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Step 1: rename old meta -> staging. Before this point, no on-disk state has
	// changed; an error needs no staging-path hint.
	if err := os.Rename(p.OldMeta, staging); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", p.OldMeta, staging, err)
	}

	// Any error past this point leaves the workspace partially-migrated inside the
	// staging dir. Wrap with staging-path guidance so the user can recover.
	if err := executeMigrateAfterRename(r, p, staging); err != nil {
		return fmt.Errorf("migration aborted; partial state at %s -- inspect and either re-run migrate after fixing the issue, or rename %s back to %s manually: %w", staging, staging, p.OldMeta, err)
	}
	r.OK(fmt.Sprintf("Meta workspace ready at %s", p.NewMeta))
	return nil
}

// executeMigrateAfterRename runs every migration step past the initial rename
// of the old meta to the staging dir. Any error is surfaced unwrapped so the
// caller can attach a single, uniform "partial state at <staging>" hint.
func executeMigrateAfterRename(r Reporter, p *migratePlan, staging string) error {
	// Step 2: inside staging, hoist .git/ to staging root and move everything else into staging/.mws/.
	// Note: .git is already at staging/.git after the rename. We need to move every other top-level
	// entry into a freshly-merged staging/.mws/. The old .mws/ already exists inside staging; we
	// keep its contents but also move staging/CLAUDE.md, staging/.workspace/, etc. INTO it.
	if err := flattenStagingIntoHarness(staging); err != nil {
		return fmt.Errorf("restructure into .mws/: %w", err)
	}

	// Step 3: rename config inside harness and write new .mws.toml at staging root.
	oldCfgPath := filepath.Join(staging, project.HarnessDirName, legacyConfigName)
	newCfgPath := filepath.Join(staging, config.ConfigFileName)
	if _, err := os.Stat(oldCfgPath); err == nil {
		if err := os.Rename(oldCfgPath, newCfgPath); err != nil {
			return fmt.Errorf("move config %s -> %s: %w", oldCfgPath, newCfgPath, err)
		}
	}

	// Step 4: write allowlist .gitignore at staging root.
	if err := writeAllowlistGitignore(staging); err != nil {
		return err
	}

	// Step 5: move every old peer into staging/<new-name>/ and refresh its symlinks.
	// Track which peers were already moved so we can attempt a reverse-rename on
	// failure -- best-effort, but it shortens the manual recovery list.
	var moved []struct{ src, dst string }
	for _, peer := range p.OldPeers {
		newName := p.Renames[peer]
		dst := filepath.Join(staging, newName)
		if err := os.Rename(peer, dst); err != nil {
			rollbackMovedPeers(r, moved)
			return fmt.Errorf("move peer %s -> %s: %w", peer, dst, err)
		}
		moved = append(moved, struct{ src, dst string }{peer, dst})
		if err := refreshPeerSymlinks(staging, dst); err != nil {
			rollbackMovedPeers(r, moved)
			return fmt.Errorf("refresh symlinks in %s: %w", dst, err)
		}
		r.OK(fmt.Sprintf("Migrated peer %s -> %s/", peer, newName))
	}

	// Step 6: rename staging -> final meta path. After this succeeds, the migration
	// is committed; no further partial-state recovery is possible from a failure
	// here, but the rename is a single atomic syscall so it either succeeds or
	// leaves staging in place.
	if err := os.Rename(staging, p.NewMeta); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", staging, p.NewMeta, err)
	}
	return nil
}

// rollbackMovedPeers best-effort returns each already-moved peer to its
// original location. Failures are reported but don't propagate -- the caller
// is already returning an error that names the staging dir.
func rollbackMovedPeers(r Reporter, moved []struct{ src, dst string }) {
	for i := len(moved) - 1; i >= 0; i-- {
		m := moved[i]
		if err := os.Rename(m.dst, m.src); err != nil {
			r.Warn(fmt.Sprintf("rollback: failed to restore %s -> %s: %v", m.dst, m.src, err))
		}
	}
}

// flattenStagingIntoHarness moves every entry at staging-root (except .git/ and .mws/) into
// staging/.mws/. If .mws/ does not yet exist, it is created. Entries that already exist
// inside .mws/ (collision) cause an error so the user can resolve manually.
func flattenStagingIntoHarness(staging string) error {
	harness := filepath.Join(staging, project.HarnessDirName)
	if err := os.MkdirAll(harness, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if name == ".git" || name == project.HarnessDirName {
			continue
		}
		src := filepath.Join(staging, name)
		dst := filepath.Join(harness, name)
		if _, err := os.Lstat(dst); err == nil {
			return fmt.Errorf("harness already has %s; resolve manually before migrating", name)
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("move %s -> %s: %w", src, dst, err)
		}
	}
	return nil
}

// writeAllowlistGitignore writes the meta-root allowlist .gitignore into staging.
func writeAllowlistGitignore(staging string) error {
	body := "# Allowlist: ignore everything at the meta workspace root, then explicitly\n" +
		"# un-ignore the paths that belong to the meta. Working copies and env staging\n" +
		"# live alongside .mws/ and are intentionally invisible to this git repo.\n" +
		"/*\n" +
		"!/.gitignore\n" +
		"!/.mws.toml\n" +
		"!/.mws/\n" +
		"!/README.md\n"
	return os.WriteFile(filepath.Join(staging, ".gitignore"), []byte(body), 0o644)
}

// refreshPeerSymlinks removes every symlink in peer (top-level only) and re-creates harness
// symlinks pointing at staging/.mws/. Non-symlink files are preserved.
func refreshPeerSymlinks(staging, peer string) error {
	entries, err := os.ReadDir(peer)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(peer, e.Name())
		st, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("remove old symlink %s: %w", p, err)
			}
		}
	}
	_, err = project.LinkHarnessIntoWorkingCopy(staging, peer)
	return err
}

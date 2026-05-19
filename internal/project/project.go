// Package project locates a meta workspace from any directory inside it and
// enumerates working copies (untracked children of the meta root).
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sustinbebustin/mws/internal/config"
)

// ErrNotInWorkspace indicates the cwd is not inside any meta workspace.
var ErrNotInWorkspace = errors.New("not inside a meta workspace")

// HarnessDirName is the meta-root directory that holds harness content that
// fans out into every working copy.
const HarnessDirName = ".mws"

// EnvStagingDirName is the meta-root directory that stages env files for copy
// into working copies on clone/sync.
const EnvStagingDirName = ".envs"

// Workspace describes the meta and working copy paths discovered from a starting directory.
type Workspace struct {
	// MetaRoot is the absolute path to the meta workspace directory.
	MetaRoot string
	// WorkingCopiesDir is the optional single-segment subdirectory under MetaRoot
	// where working copies live. Empty means working copies sit directly under
	// MetaRoot. Locate populates this from .mws.toml on a best-effort basis.
	WorkingCopiesDir string
	// WorkingCopy is the absolute path to the working copy the search started from,
	// or empty when the starting directory was the meta root itself.
	WorkingCopy string
}

// Locate walks from start towards filesystem root looking for a directory that
// contains .mws.toml. The first match is the meta root. If start was nested
// inside the meta, WorkingCopy is set to the direct child of the copies root
// that contains start. The copies root is the meta root, or
// <metaRoot>/<working_copies_dir> when that key is set in .mws.toml.
func Locate(start string) (*Workspace, error) {
	start, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}

	dir := start
	for {
		if isValidMeta(dir) {
			// Best-effort config load: if the file is malformed, downstream
			// commands that need the config will surface the real parse error.
			// We only use it here to discover working_copies_dir.
			copiesDir := ""
			if cfg, err := config.Load(dir); err == nil {
				copiesDir = cfg.WorkingCopiesDir
			}
			return resolveWorkspace(dir, start, copiesDir), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotInWorkspace
		}
		dir = parent
	}
}

// resolveWorkspace builds a Workspace given the meta root, the original
// starting directory, and the configured working-copies subdir (may be empty).
// If start lives inside the copies root, WorkingCopy is the direct child of
// the copies root containing start.
func resolveWorkspace(metaRoot, start, copiesDir string) *Workspace {
	ws := &Workspace{MetaRoot: metaRoot, WorkingCopiesDir: copiesDir}
	if start == metaRoot {
		return ws
	}
	rel, err := filepath.Rel(metaRoot, start)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ws
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if copiesDir != "" {
		if len(parts) < 2 || parts[0] != copiesDir {
			return ws
		}
		parts = parts[1:]
	}
	// The post-peel guard below cannot fire when copiesDir != "" (the len(parts) < 2
	// check above guarantees a non-empty parts[0] here); it exists for the
	// copiesDir == "" branch where rel may be a single empty segment.
	if len(parts) == 0 || parts[0] == "" {
		return ws
	}
	ws.WorkingCopy = filepath.Join(metaRoot, copiesDir, parts[0])
	return ws
}

// CopiesRoot returns the directory under which working copies live: the meta
// root by default, or <metaRoot>/<WorkingCopiesDir> when configured.
func (w *Workspace) CopiesRoot() string {
	return filepath.Join(w.MetaRoot, w.WorkingCopiesDir)
}

// isValidMeta reports whether dir is a meta workspace root: <dir>/.mws.toml exists as a regular file.
func isValidMeta(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, config.ConfigFileName))
	return err == nil && st.Mode().IsRegular()
}

// EnumerateCopies returns all working copies inside w.CopiesRoot(), sorted by path.
//
// A working copy is any direct subdirectory of the copies root whose name does
// NOT start with a dot. The defensive dotfile filter excludes harness-internal
// dirs like .mws, .envs, .git plus any future system dir. The copies root
// itself is not included.
//
// When working_copies_dir is configured but the directory does not yet exist
// (e.g. before the first clone), an empty slice is returned without error.
func (w *Workspace) EnumerateCopies() ([]string, error) {
	root, err := filepath.Abs(w.CopiesRoot())
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read copies root %s: %w", root, err)
	}

	var copies []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		copies = append(copies, filepath.Join(root, name))
	}
	sort.Strings(copies)
	return copies, nil
}

// ResolveCopy resolves an optional name to an absolute working-copy path
// inside w.CopiesRoot(). If name is empty, w.WorkingCopy is used as a fallback
// (so commands run from inside a working copy default to that copy). The
// resulting path is verified to exist as a directory.
func (w *Workspace) ResolveCopy(name string) (string, error) {
	if name == "" {
		if w.WorkingCopy == "" {
			return "", errors.New("working copy name required (none could be inferred from cwd)")
		}
		name = filepath.Base(w.WorkingCopy)
	}
	if err := ValidateName(name); err != nil {
		return "", err
	}
	target := filepath.Join(w.CopiesRoot(), name)
	st, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("working copy %s: %w", target, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("working copy %s is not a directory", target)
	}
	return target, nil
}

// ValidateWorkingCopiesDir checks the optional working_copies_dir config value.
// Empty is allowed and means "working copies live directly under the meta
// root"; any non-empty value must satisfy ValidateName (single path-safe
// segment). Centralising the rule lets clone, init, and the init prompt
// share one source of truth.
func ValidateWorkingCopiesDir(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return ValidateName(s)
}

// ValidateName checks that name is a path-safe single segment: ASCII letters,
// digits, '-', '_', or '.', and not starting with '.' or '-'. This is the
// segment shape used by both project names and working-copy names; the dot-
// prefix ban means the harness (.mws), env staging (.envs), and .git can
// never collide with a working-copy name.
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name is required")
	}
	if strings.HasPrefix(name, ".") {
		return errors.New("must not start with '.'")
	}
	if strings.HasPrefix(name, "-") {
		return errors.New("must not start with '-'")
	}
	for _, r := range name {
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

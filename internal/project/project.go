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
	// WorkingCopy is the absolute path to the working copy the search started from,
	// or empty when the starting directory was the meta root itself.
	WorkingCopy string
}

// Locate walks from start towards filesystem root looking for a directory that
// contains .mws.toml. The first match is the meta root. If start was nested
// inside the meta, WorkingCopy is set to the direct child of the meta that
// contains start.
func Locate(start string) (*Workspace, error) {
	start, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}

	dir := start
	for {
		if isValidMeta(dir) {
			return resolveWorkspace(dir, start), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotInWorkspace
		}
		dir = parent
	}
}

// resolveWorkspace builds a Workspace given the meta root and the original
// starting directory. If start lives inside metaRoot, WorkingCopy is the direct
// child of metaRoot containing start.
func resolveWorkspace(metaRoot, start string) *Workspace {
	if start == metaRoot {
		return &Workspace{MetaRoot: metaRoot}
	}
	rel, err := filepath.Rel(metaRoot, start)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return &Workspace{MetaRoot: metaRoot}
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) == 0 || parts[0] == "" {
		return &Workspace{MetaRoot: metaRoot}
	}
	return &Workspace{
		MetaRoot:    metaRoot,
		WorkingCopy: filepath.Join(metaRoot, parts[0]),
	}
}

// isValidMeta reports whether dir is a meta workspace root: <dir>/.mws.toml exists as a regular file.
func isValidMeta(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, config.ConfigFileName))
	return err == nil && st.Mode().IsRegular()
}

// EnumerateWorkingCopies returns all working copies inside metaRoot, sorted by path.
//
// A working copy is any direct subdirectory of metaRoot whose name does NOT
// start with a dot. The defensive dotfile filter excludes harness-internal
// dirs like .mws, .envs, .git plus any future system dir. The meta root
// itself is not included.
func EnumerateWorkingCopies(metaRoot string) ([]string, error) {
	metaRoot, err := filepath.Abs(metaRoot)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(metaRoot)
	if err != nil {
		return nil, fmt.Errorf("read meta root %s: %w", metaRoot, err)
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
		copies = append(copies, filepath.Join(metaRoot, name))
	}
	sort.Strings(copies)
	return copies, nil
}

// ResolveCopy resolves an optional name to an absolute working-copy path
// inside w.MetaRoot. If name is empty, w.WorkingCopy is used as a fallback
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
	target := filepath.Join(w.MetaRoot, name)
	st, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("working copy %s: %w", target, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("working copy %s is not a directory", target)
	}
	return target, nil
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

// Package project locates a meta workspace from a working copy and enumerates peers.
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

// ErrNotInWorkspace indicates the cwd is neither a meta workspace nor a working copy of one.
var ErrNotInWorkspace = errors.New("not inside a meta workspace or working copy")

// MetaSuffix is the directory-name suffix that identifies a meta workspace.
const MetaSuffix = "-meta"

// Workspace describes the meta and working copy paths discovered from a starting directory.
type Workspace struct {
	// MetaRoot is the absolute path to the meta workspace directory.
	MetaRoot string
	// WorkingCopy is the absolute path to the working copy the search started from,
	// or empty when the starting directory was the meta itself.
	WorkingCopy string
}

// Locate walks from start towards root, returning the meta workspace path.
//
// Three discovery modes:
//  1. start is itself a meta (has .mws/config.toml directly).
//  2. start is a working copy whose .mws is a symlink into a sibling meta.
//  3. ancestors are searched up to the filesystem root.
func Locate(start string) (*Workspace, error) {
	start, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}

	dir := start
	for {
		if ws, ok := workspaceFor(dir); ok {
			return ws, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotInWorkspace
		}
		dir = parent
	}
}

// workspaceFor checks whether dir is a meta or working copy and, if so, returns the resolved workspace.
func workspaceFor(dir string) (*Workspace, bool) {
	mwsDir := filepath.Join(dir, config.DirName)
	st, err := os.Lstat(mwsDir)
	if err != nil {
		return nil, false
	}

	// Working copy: .mws is a symlink whose target sits inside a valid meta.
	if st.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(mwsDir)
		if err != nil {
			return nil, false
		}
		metaRoot := filepath.Dir(resolved)
		if !isValidMeta(metaRoot) {
			return nil, false
		}
		return &Workspace{MetaRoot: metaRoot, WorkingCopy: dir}, true
	}

	// Direct meta: real .mws directory with a real config file.
	if st.IsDir() && isValidMeta(dir) {
		return &Workspace{MetaRoot: dir}, true
	}

	return nil, false
}

// isValidMeta reports whether dir is a meta workspace: has .mws/config.toml as a regular file.
func isValidMeta(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, config.DirName, config.FileName))
	return err == nil
}

// isPeerOf reports whether candidate is a working-copy peer of metaRoot: candidate/.mws is a symlink
// resolving to metaRoot/.mws, and metaRoot is a valid meta.
func isPeerOf(metaRoot, candidate string) bool {
	if candidate == metaRoot {
		return false
	}
	st, err := os.Lstat(candidate)
	if err != nil || !st.IsDir() {
		return false
	}
	peerMws := filepath.Join(candidate, config.DirName)
	peerTarget, err := filepath.EvalSymlinks(peerMws)
	if err != nil {
		return false
	}
	metaTarget, err := filepath.EvalSymlinks(filepath.Join(metaRoot, config.DirName))
	if err != nil {
		return false
	}
	return peerTarget == metaTarget && isValidMeta(metaRoot)
}

// EnumeratePeers returns all working-copy peers of metaRoot living in metaRoot's parent directory.
//
// A peer is any sibling directory satisfying isPeerOf. The meta directory itself is excluded.
func EnumeratePeers(metaRoot string) ([]string, error) {
	metaRoot, err := filepath.Abs(metaRoot)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(metaRoot)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, fmt.Errorf("read parent dir %s: %w", parent, err)
	}

	var peers []string
	for _, e := range entries {
		candidate := filepath.Join(parent, e.Name())
		if isPeerOf(metaRoot, candidate) {
			peers = append(peers, candidate)
		}
	}
	sort.Strings(peers)
	return peers, nil
}

// Name derives the project name from a meta directory: strips the trailing "-meta".
func Name(metaRoot string) string {
	base := filepath.Base(metaRoot)
	return strings.TrimSuffix(base, MetaSuffix)
}

// MetaDirName returns the conventional meta directory name for a given project name.
func MetaDirName(projectName string) string {
	return projectName + MetaSuffix
}

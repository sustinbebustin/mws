package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sustinbebustin/mws/internal/atomicfile"
	"github.com/sustinbebustin/mws/internal/project"
)

// envStagingDir returns the env-staging root inside metaRoot.
func envStagingDir(metaRoot string) string {
	return filepath.Join(metaRoot, project.EnvStagingDirName)
}

// resolveCopy is ws.ResolveCopy with a missing copy name reported as a usage
// error that shows the invocation naming one; invocation is the command line
// with a `<copy>` placeholder, e.g. "mws sync-env <copy>".
func resolveCopy(ws *project.Workspace, name, invocation string) (string, error) {
	target, err := ws.ResolveCopy(name)
	if errors.Is(err, project.ErrNoCopyNamed) {
		return "", usagef("%w (under %s); run `%s` (`mws list` shows the copies) or run from inside a working copy", err, ws.MetaRoot, invocation)
	}
	return target, err
}

// looksLikeWorkingCopy reports whether candidate is plausibly an mws-managed
// working copy of metaRoot: at least one top-level entry is a symlink that
// resolves into <metaRoot>/.mws/. Used as a guard against rm'ing a directory
// that happens to live under the meta root but isn't actually a working copy.
func looksLikeWorkingCopy(metaRoot, candidate string) bool {
	harness, err := filepath.EvalSymlinks(filepath.Join(metaRoot, project.HarnessDirName))
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(candidate)
	if err != nil {
		return false
	}
	for _, e := range entries {
		linkPath := filepath.Join(candidate, e.Name())
		st, err := os.Lstat(linkPath)
		if err != nil || st.Mode()&os.ModeSymlink == 0 {
			continue
		}
		resolved, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(harness, resolved)
		if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			return true
		}
		// Also accept a symlink whose target IS the harness root itself
		// (defensive; unusual but legal).
		if resolved == harness {
			return true
		}
	}
	return false
}

// moveFile moves src to dst. It first tries os.Rename for an atomic move;
// if rename fails with EXDEV (src and dst on different filesystems), it
// falls back to atomicfile.Copy followed by os.Remove(src).
func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := atomicfile.Copy(src, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("copied %s to %s but failed to remove source: %w", src, dst, err)
	}
	return nil
}

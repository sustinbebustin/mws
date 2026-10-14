package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/sustinbebustin/mws/internal/project"
)

// rootChanges lists the entries at the worktree root that removal would lose:
// anything mws did not put there, and copied files whose content changed
// since creation. The root's .gitignore hides all of them from git. Symlinks
// are mws's links and lose nothing; folders are the native repo worktrees,
// checked by git. Directories are listed with a trailing slash.
func rootChanges(root string, folders []string) ([]string, error) {
	m, err := readMarker(root)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{".git": true, ".gitignore": true, project.WorktreeMarkerFileName: true}
	for _, f := range folders {
		known[f] = true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if known[name] || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		if e.IsDir() {
			out = append(out, name+"/")
			continue
		}
		want, ok := m.Copied[name]
		if !ok {
			out = append(out, name)
			continue
		}
		got, err := fileSHA256(filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		if got != want {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// fileSHA256 returns the hex SHA-256 of the file at p.
func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

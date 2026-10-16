package worktree

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/sustinbebustin/mws/internal/atomicfile"
	"github.com/sustinbebustin/mws/internal/project"
)

// Marker is the content of a worktree's marker file. Branches are not
// recorded; they are read live from git.
type Marker struct {
	Copy       string    `toml:"copy"`
	Name       string    `toml:"name"`
	Created    time.Time `toml:"created"`
	MWSVersion string    `toml:"mws_version"`
	// Copied maps each file mws carried from the copy (root files by name,
	// env targets by slash-separated path such as "frontend/.env") to the
	// SHA-256 of the content mws last wrote, so removal and a re-render can
	// tell an edited copy from a pristine one.
	Copied map[string]string `toml:"copied"`
}

// writeMarker creates the marker file of the worktree at root; it fails if
// one already exists.
func writeMarker(root string, m Marker) error {
	f, err := os.OpenFile(filepath.Join(root, project.WorktreeMarkerFileName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	encErr := toml.NewEncoder(f).Encode(m)
	return errors.Join(encErr, f.Close())
}

// updateMarker atomically replaces the marker file of the worktree at root.
func updateMarker(root string, m Marker) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(root, project.WorktreeMarkerFileName), buf.Bytes(), 0o644)
}

// readMarker decodes the marker file of the worktree at root.
func readMarker(root string) (Marker, error) {
	var m Marker
	_, err := toml.DecodeFile(filepath.Join(root, project.WorktreeMarkerFileName), &m)
	return m, err
}

// Package skeleton renders the embedded meta-workspace template tree to disk.
package skeleton

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/sustinbebustin/mws"
	"github.com/sustinbebustin/mws/internal/config"
)

// Data is the template context exposed to *.tmpl files in the skeleton tree.
type Data struct {
	ProjectName string
	Description string
	Repos       []config.Repo
}

const (
	rootPrefix    = "skeleton" // mirrors the embed root in mws.SkeletonFS
	tmplExtension = ".tmpl"
)

// Render copies the embedded skeleton tree into dst, rendering *.tmpl files through text/template
// with data and stripping the .tmpl extension.
//
// Empty placeholder files (e.g., skeleton/.gitkeep, skeleton/docs/adr/.gitkeep) are skipped.
func Render(dst string, data Data) error {
	root, err := fs.Sub(mws.SkeletonFS, rootPrefix)
	if err != nil {
		return fmt.Errorf("open embedded skeleton: %w", err)
	}

	return fs.WalkDir(root, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		if filepath.Base(path) == ".gitkeep" {
			return nil
		}

		target := filepath.Join(dst, strings.TrimSuffix(path, tmplExtension))

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		content, err := fs.ReadFile(root, path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}

		if strings.HasSuffix(path, tmplExtension) {
			tmpl, err := template.New(path).Option("missingkey=error").Parse(string(content))
			if err != nil {
				return fmt.Errorf("parse template %s: %w", path, err)
			}
			out, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			if err := tmpl.Execute(out, data); err != nil {
				_ = out.Close()
				_ = os.Remove(target)
				return fmt.Errorf("render %s: %w", path, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close %s: %w", target, err)
			}
			return nil
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644)
	})
}

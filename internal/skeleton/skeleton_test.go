package skeleton

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

func TestRenderEmbeddedSkeleton(t *testing.T) {
	dst := t.TempDir()
	data := Data{
		ProjectName: "demo",
		Description: "A demo workspace",
		Repos: []config.Repo{
			{Folder: "frontend", URL: "git@github.com:demo/frontend.git"},
			{Folder: "backend", URL: "git@github.com:demo/backend.git"},
		},
	}

	if err := Render(dst, data); err != nil {
		t.Fatalf("Render: %v", err)
	}

	// CLAUDE.md.tmpl -> CLAUDE.md with rendered substitutions.
	claude, err := os.ReadFile(filepath.Join(dst, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(claude), "# demo") {
		t.Fatalf("CLAUDE.md missing project name; got:\n%s", string(claude))
	}
	if !strings.Contains(string(claude), "frontend/") {
		t.Fatalf("CLAUDE.md missing repo folder; got:\n%s", string(claude))
	}

	// README.md.tmpl -> README.md
	readme, err := os.ReadFile(filepath.Join(dst, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if !strings.Contains(string(readme), "demo-meta") {
		t.Fatalf("README.md missing meta name; got:\n%s", string(readme))
	}
	if !strings.Contains(string(readme), "git@github.com:demo/frontend.git") {
		t.Fatalf("README.md missing repo URL")
	}

	// .gitkeep files are not materialised.
	if _, err := os.Stat(filepath.Join(dst, ".claude", "skills", ".gitkeep")); err == nil {
		t.Fatalf(".gitkeep should not be materialised")
	}
	// Parent directory of a .gitkeep still gets created.
	if _, err := os.Stat(filepath.Join(dst, ".claude", "skills")); err != nil {
		t.Fatalf(".claude/skills/ should exist: %v", err)
	}

	// Sanity check: a known harness file copied through verbatim.
	if _, err := os.Stat(filepath.Join(dst, ".claude", "settings.json")); err != nil {
		t.Fatalf(".claude/settings.json missing: %v", err)
	}
}

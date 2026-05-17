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

	// .mws/CLAUDE.md.tmpl -> .mws/CLAUDE.md with rendered substitutions.
	claude, err := os.ReadFile(filepath.Join(dst, ".mws", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read .mws/CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(claude), "# demo") {
		t.Fatalf(".mws/CLAUDE.md missing project name; got:\n%s", string(claude))
	}
	if !strings.Contains(string(claude), "frontend/") {
		t.Fatalf(".mws/CLAUDE.md missing repo folder; got:\n%s", string(claude))
	}

	// README.md.tmpl -> README.md at meta root.
	readme, err := os.ReadFile(filepath.Join(dst, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if !strings.Contains(string(readme), "demo") {
		t.Fatalf("README.md missing project name; got:\n%s", string(readme))
	}
	if !strings.Contains(string(readme), "git@github.com:demo/frontend.git") {
		t.Fatalf("README.md missing repo URL")
	}

	// Meta-root allowlist .gitignore.
	gi, err := os.ReadFile(filepath.Join(dst, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	body := string(gi)
	for _, want := range []string{"/*", "!/.gitignore", "!/.mws.toml", "!/.mws/", "!/README.md"} {
		if !strings.Contains(body, want) {
			t.Fatalf("meta-root .gitignore missing %q; got:\n%s", want, body)
		}
	}

	// .gitkeep files are not materialised but their parent dirs are.
	if _, err := os.Stat(filepath.Join(dst, ".mws", ".claude", "skills", ".gitkeep")); err == nil {
		t.Fatalf(".gitkeep should not be materialised")
	}
	if _, err := os.Stat(filepath.Join(dst, ".mws", ".claude", "skills")); err != nil {
		t.Fatalf(".mws/.claude/skills/ should exist: %v", err)
	}

	// Verbatim harness file copied through.
	if _, err := os.Stat(filepath.Join(dst, ".mws", ".claude", "settings.json")); err != nil {
		t.Fatalf(".mws/.claude/settings.json missing: %v", err)
	}

	// CLAUDE.md should NOT appear at meta root -- it lives in .mws/ for fan-out.
	if _, err := os.Stat(filepath.Join(dst, "CLAUDE.md")); err == nil {
		t.Fatalf("CLAUDE.md should not exist at meta root; it belongs in .mws/")
	}
}

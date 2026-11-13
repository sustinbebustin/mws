package skeleton

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

	// CLAUDE.md.tmpl -> CLAUDE.md at meta root, holding the mws workspace notes
	// every working copy inherits by walk-up.
	rootClaude, err := os.ReadFile(filepath.Join(dst, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(rootClaude), "mws clone") {
		t.Fatalf("CLAUDE.md missing mws workflow; got:\n%s", string(rootClaude))
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
	for _, want := range []string{"/*", "!/.gitignore", "!/.mws.toml", "!/.mws/", "!/README.md", "!/CLAUDE.md"} {
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

	// Claude's worktree hooks are wired to the mws adapters.
	raw, err := os.ReadFile(filepath.Join(dst, ".mws", ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var settings struct {
		FileSuggestion struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"fileSuggestion"`
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}
	for event, want := range map[string]string{
		"WorktreeCreate": "mws wt hook create",
		"WorktreeRemove": "mws wt hook remove",
		"SubagentStop":   "mws wt hook subagent-stop",
	} {
		var got []string
		for _, group := range settings.Hooks[event] {
			for _, h := range group.Hooks {
				got = append(got, h.Command)
			}
		}
		if !slices.Contains(got, want) {
			t.Fatalf("settings.json hooks.%s = %q; want it to contain %q", event, got, want)
		}
	}
	if fs := settings.FileSuggestion; fs.Type != "command" || fs.Command != "$CLAUDE_PROJECT_DIR/.claude/file-suggestion.sh" {
		t.Fatalf("settings.json fileSuggestion = %+v; want the harness file-suggestion.sh command", fs)
	}

	// mws workspace notes live only in the meta-root CLAUDE.md; .mws/CLAUDE.md is project content.
	if strings.Contains(string(claude), "mws clone") {
		t.Fatalf(".mws/CLAUDE.md should not carry mws workflow notes; got:\n%s", string(claude))
	}
}

// embed.FS drops file modes, so Render must restore the executable bit on the
// scripts and binaries settings.json runs, and only on those.
func TestRenderMarksExecutables(t *testing.T) {
	dst := t.TempDir()
	if err := Render(dst, Data{ProjectName: "demo"}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	claudeDir := filepath.Join(dst, ".mws", ".claude")
	for rel, wantExec := range map[string]bool{
		"file-suggestion.sh":             true,
		"hooks/enforce-root-bin":         true,
		"hooks/git-repo-reminder-bin":    true,
		"settings.json":                  false,
		"hooks/enforce-root/main.go":     false,
		"hooks/git-repo-reminder/go.mod": false,
		"rules/use-full-paths.md":        false,
	} {
		st, err := os.Stat(filepath.Join(claudeDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		if gotExec := st.Mode().Perm()&0o111 != 0; gotExec != wantExec {
			t.Errorf("%s mode %v; want executable=%v", rel, st.Mode().Perm(), wantExec)
		}
	}
}

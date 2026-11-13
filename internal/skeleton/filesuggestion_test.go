package skeleton

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestFileSuggestion runs the rendered file-suggestion.sh against a working
// copy laid out the way mws builds one: harness entries symlinked in from the
// meta and a native repo as a nested directory.
func TestFileSuggestion(t *testing.T) {
	for _, tool := range []string{"bash", "jq", "rg"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	meta := t.TempDir()
	if err := Render(meta, Data{ProjectName: "demo"}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	copyDir := filepath.Join(meta, "main")
	if err := os.MkdirAll(copyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{".claude", ".workspace", "CLAUDE.md"} {
		if err := os.Symlink(filepath.Join("..", ".mws", entry), filepath.Join(copyDir, entry)); err != nil {
			t.Fatal(err)
		}
	}
	for rel, body := range map[string]string{
		"web/.gitignore":            "node_modules/\n",
		"web/src/app.ts":            "",
		"web/node_modules/dep/x.js": "",
	} {
		p := filepath.Join(copyDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	suggest := func(query string) []string {
		t.Helper()
		cmd := exec.Command(filepath.Join(copyDir, ".claude", "file-suggestion.sh"))
		cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+copyDir)
		cmd.Stdin = strings.NewReader(`{"query": "` + query + `"}`)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		return strings.Fields(string(out))
	}

	if got := suggest("web"); len(got) == 0 || got[0] != "web/" {
		t.Errorf(`query "web" = %q; want the repo folder "web/" first`, got)
	}
	if got := suggest("app"); !slices.Contains(got, "web/src/app.ts") {
		t.Errorf(`query "app" = %q; want "web/src/app.ts"`, got)
	}
	if got := suggest("node_modules"); len(got) != 0 {
		t.Errorf(`query "node_modules" = %q; want nothing, the nested .gitignore excludes it`, got)
	}
	// Harness files resolve through the symlink but keep copy-relative paths.
	if got := suggest("use-full-paths"); !slices.Equal(got, []string{".claude/rules/use-full-paths.md"}) {
		t.Errorf(`query "use-full-paths" = %q; want only ".claude/rules/use-full-paths.md"`, got)
	}
	// Hidden top-level entries outside the everyday set need a leading dot.
	if got := suggest("settings"); slices.Contains(got, ".claude/settings.json") {
		t.Errorf(`query "settings" = %q; want ".claude/settings.json" gated`, got)
	}
	if got := suggest(".claude/settings"); !slices.Contains(got, ".claude/settings.json") {
		t.Errorf(`query ".claude/settings" = %q; want ".claude/settings.json"`, got)
	}
}

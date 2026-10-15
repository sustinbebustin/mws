package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

// handWrittenConfig carries everything a re-encode would lose: comments,
// blank lines, alignment, and a key mws does not know.
const handWrittenConfig = `# shop meta workspace
project_name = "shop"   # keep this name

unknown_key = "kept"

[[repos]]
  folder = "web"
  url    = "git@github.com:example/shop-web.git"

[trash]
  # a week is plenty
  retention_days = 7
`

// seedConfigMeta creates a meta workspace whose .mws.toml is body verbatim
// and returns the meta root.
func seedConfigMeta(t *testing.T, body string) string {
	t.Helper()
	meta := filepath.Join(t.TempDir(), "shop")
	mustMkdir(t, filepath.Join(meta, ".mws"))
	mustWriteFile(t, config.Path(meta), body)
	return meta
}

func readConfigBytes(t *testing.T, meta string) string {
	t.Helper()
	b, err := os.ReadFile(config.Path(meta))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(b)
}

func TestAddRepoAppendsBlockAndKeepsHandWrittenConfig(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
		block string
	}{
		{"default", nil, "\n[[repos]]\n  folder = \"api\"\n  url    = \"git@github.com:example/api.git\"\n"},
		{"optional", []string{"--optional"}, "\n[[optional_repos]]\n  folder = \"api\"\n  url    = \"git@github.com:example/api.git\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta := seedConfigMeta(t, handWrittenConfig)
			args := append([]string{"add-repo", "git@github.com:example/api.git", "api"}, tc.flags...)
			res := runMWS(t, meta, args...)
			if res.code != 0 {
				t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
			}
			if got, want := readConfigBytes(t, meta), handWrittenConfig+tc.block; got != want {
				t.Fatalf("config after add-repo:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestAddRepoAppendsAfterFileWithoutTrailingNewline(t *testing.T) {
	meta := seedConfigMeta(t, `project_name = "shop"`)
	if res := runMWS(t, meta, "add-repo", "git@github.com:example/api.git"); res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	want := "project_name = \"shop\"\n\n[[repos]]\n  folder = \"api\"\n  url    = \"git@github.com:example/api.git\"\n"
	if got := readConfigBytes(t, meta); got != want {
		t.Fatalf("config after add-repo:\n%q\nwant:\n%q", got, want)
	}
}

func TestAddRepoLeavesConfigUntouchedWhenNothingToRegister(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"folder taken", []string{"add-repo", "git@github.com:example/other.git", "web"}, 3},
		{"folder taken in other list", []string{"add-repo", "git@github.com:example/shop-web.git", "web", "--optional"}, 3},
		{"already registered", []string{"add-repo", "git@github.com:example/shop-web.git", "web"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta := seedConfigMeta(t, handWrittenConfig)
			res := runMWS(t, meta, tc.args...)
			if res.code != tc.code {
				t.Fatalf("exit %d, want %d\nstderr: %s", res.code, tc.code, res.stderr)
			}
			if got := readConfigBytes(t, meta); got != handWrittenConfig {
				t.Fatalf("config changed:\n%s", got)
			}
		})
	}
}

func TestAddRepoRestoresConfigWhenAppendedFileIsInvalid(t *testing.T) {
	// An inline `repos = []` array cannot be extended by a [[repos]] table,
	// so the appended file no longer parses.
	body := "project_name = \"shop\"\nrepos = []\n"
	meta := seedConfigMeta(t, body)
	res := runMWS(t, meta, "add-repo", "git@github.com:example/api.git", "api")
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	for _, w := range []string{"repos", config.ConfigFileName, "unchanged"} {
		if !strings.Contains(res.stderr, w) {
			t.Errorf("stderr does not name %q:\n%s", w, res.stderr)
		}
	}
	if got := readConfigBytes(t, meta); got != body {
		t.Fatalf("config not restored:\n%s", got)
	}
}

func TestRunAddRepoOptionalRegistersWithoutCloning(t *testing.T) {
	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))
	if err := config.Save(metaRoot, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	// An existing working copy that must NOT receive a clone of the optional repo.
	peer := filepath.Join(metaRoot, "peer")
	mustMkdir(t, peer)

	withCwd(t, metaRoot, func() {
		if err := runAddRepo(context.Background(), nopReporter{}, "git@github.com:example/worker.git", "worker", true); err != nil {
			t.Fatalf("runAddRepo --optional: %v", err)
		}
	})

	cfg, err := config.Load(metaRoot)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(cfg.Repos) != 0 {
		t.Fatalf("optional registration must not touch [[repos]], got %+v", cfg.Repos)
	}
	if _, ok := cfg.OptionalRepo("worker"); !ok {
		t.Fatalf("worker not registered under optional_repos: %+v", cfg.OptionalRepos)
	}
	// No broadcast clone into existing copies.
	if _, err := os.Stat(filepath.Join(peer, "worker")); err == nil {
		t.Fatal("optional add-repo should not clone into existing working copies")
	}
}

func TestRunAddRepoOptionalRejectsDuplicateFolder(t *testing.T) {
	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))
	if err := config.Save(metaRoot, &config.Config{
		ProjectName: "demo",
		Repos:       []config.Repo{{Folder: "worker", URL: "u"}},
	}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	withCwd(t, metaRoot, func() {
		err := runAddRepo(context.Background(), nopReporter{}, "git@github.com:example/worker.git", "worker", true)
		if err == nil {
			t.Fatal("expected error registering an optional repo whose folder collides with a default repo")
		}
	})
}

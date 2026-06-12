package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPath(t *testing.T) {
	metaRoot := filepath.Join(t.TempDir(), "demo")
	got := Path(metaRoot)
	want := filepath.Join(metaRoot, ConfigFileName)
	if got != want {
		t.Fatalf("Path: got %q want %q", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := &Config{
		ProjectName: "demo",
		Description: "Demo workspace",
		Repos: []Repo{
			{Folder: "frontend", URL: "git@github.com:example/frontend.git"},
			{Folder: "backend", URL: "git@github.com:example/backend.git"},
		},
	}

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Save writes directly to <dir>/.mws.toml, not <dir>/.mws/config.toml.
	if _, err := os.Stat(filepath.Join(dir, ConfigFileName)); err != nil {
		t.Fatalf("expected %s at meta root: %v", ConfigFileName, err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestRoundTripWithEnvs(t *testing.T) {
	dir := t.TempDir()
	want := &Config{
		ProjectName: "demo",
		Repos: []Repo{
			{
				Folder: "frontend",
				URL:    "git@github.com:example/frontend.git",
				Envs: []EnvMapping{
					{Source: "apps-internal.env", Target: "apps/internal/.env"},
					{Source: "apps-public.env", Target: "apps/public/.env"},
				},
			},
			{Folder: "backend", URL: "git@github.com:example/backend.git"},
		},
	}

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch\n got: %+v\nwant: %+v", got, want)
	}

	// Backend has no envs -- omitempty keeps the toml clean.
	body, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Count(string(body), "envs") != 2 { // 2 entries on the frontend repo
		t.Fatalf("expected exactly 2 [[repos.envs]] entries in toml, got:\n%s", string(body))
	}
}

func TestRoundTripWithSetup(t *testing.T) {
	dir := t.TempDir()
	want := &Config{
		ProjectName: "demo",
		Repos: []Repo{
			{
				Folder: "frontend",
				URL:    "git@github.com:example/frontend.git",
				Setup: []SetupCommand{
					{Cmd: "pnpm install --frozen-lockfile"},
					{Cmd: "pnpm build"},
				},
			},
			{Folder: "backend", URL: "git@github.com:example/backend.git"},
		},
	}

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch\n got: %+v\nwant: %+v", got, want)
	}

	body, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if n := strings.Count(string(body), "[[repos.setup]]"); n != 2 {
		t.Fatalf("expected 2 [[repos.setup]] entries in toml, got %d:\n%s", n, string(body))
	}
}

func TestAddRepoDedup(t *testing.T) {
	c := &Config{}
	if !c.AddRepo(Repo{Folder: "frontend", URL: "a"}) {
		t.Fatal("first add should succeed")
	}
	if c.AddRepo(Repo{Folder: "frontend", URL: "b"}) {
		t.Fatal("duplicate folder should be rejected")
	}
	if len(c.Repos) != 1 {
		t.Fatalf("got %d repos, want 1", len(c.Repos))
	}
	if c.Repos[0].URL != "a" {
		t.Fatalf("existing URL was overwritten: %q", c.Repos[0].URL)
	}
}

func TestRoundTripWithOptionalRepos(t *testing.T) {
	dir := t.TempDir()
	want := &Config{
		ProjectName: "demo",
		Repos: []Repo{
			{Folder: "frontend", URL: "git@github.com:example/frontend.git"},
		},
		OptionalRepos: []Repo{
			{
				Folder: "worker",
				URL:    "git@github.com:example/worker.git",
				Setup:  []SetupCommand{{Cmd: "go build ./..."}},
			},
		},
	}

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch\n got: %+v\nwant: %+v", got, want)
	}
	body, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(body), "[[optional_repos]]") {
		t.Fatalf("expected [[optional_repos]] in toml, got:\n%s", string(body))
	}
}

func TestOptionalReposOmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &Config{ProjectName: "demo"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	body, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(body), "optional_repos") {
		t.Fatalf("optional_repos should be omitted when empty, got:\n%s", string(body))
	}
}

func TestAddOptionalRepoDedup(t *testing.T) {
	c := &Config{}
	if !c.AddOptionalRepo(Repo{Folder: "worker", URL: "a"}) {
		t.Fatal("first add should succeed")
	}
	if c.AddOptionalRepo(Repo{Folder: "worker", URL: "b"}) {
		t.Fatal("duplicate optional folder should be rejected")
	}
	if len(c.OptionalRepos) != 1 || c.OptionalRepos[0].URL != "a" {
		t.Fatalf("optional repos unexpected: %+v", c.OptionalRepos)
	}
}

func TestFolderUniqueAcrossLists(t *testing.T) {
	c := &Config{Repos: []Repo{{Folder: "shared", URL: "a"}}}
	if c.AddOptionalRepo(Repo{Folder: "shared", URL: "b"}) {
		t.Fatal("optional folder colliding with a default repo should be rejected")
	}

	c2 := &Config{OptionalRepos: []Repo{{Folder: "shared", URL: "a"}}}
	if c2.AddRepo(Repo{Folder: "shared", URL: "b"}) {
		t.Fatal("default folder colliding with an optional repo should be rejected")
	}
}

func TestOptionalRepoLookup(t *testing.T) {
	c := &Config{OptionalRepos: []Repo{{Folder: "worker", URL: "a"}}}
	if got, ok := c.OptionalRepo("worker"); !ok || got.URL != "a" {
		t.Fatalf("lookup hit: got %+v ok=%v", got, ok)
	}
	if _, ok := c.OptionalRepo("missing"); ok {
		t.Fatal("lookup miss should report ok=false")
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("expected error for missing config")
	}
}

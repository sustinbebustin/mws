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

func TestLoadMissing(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("expected error for missing config")
	}
}

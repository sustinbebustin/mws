package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

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

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestPath(t *testing.T) {
	metaRoot := filepath.Join(t.TempDir(), "foo-meta")
	got := Path(metaRoot)
	want := filepath.Join(metaRoot, ".mws", "config.toml")
	if got != want {
		t.Fatalf("Path: got %q want %q", got, want)
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

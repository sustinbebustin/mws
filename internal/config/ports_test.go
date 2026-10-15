package config

import (
	"bytes"
	"os"
	"reflect"
	"testing"
)

func intp(n int) *int { return &n }

func TestRoundTripPortTables(t *testing.T) {
	dir := t.TempDir()
	want := &Config{
		ProjectName: "shop",
		Repos:       []Repo{{Folder: "web", URL: "git@github.com:example/shop-web.git"}},
		Ports: &Ports{
			Stride: intp(10),
			Slots:  intp(50),
			Port: []Port{
				{Name: "web", Base: 5173},
				{Name: "db", Base: 5432, Var: "DB_PORT"},
			},
		},
		Vars: map[string]string{
			"COMPOSE_PROJECT_NAME": "shop-${MWS_COPY_SLUG}",
			"DATABASE_URL":         "postgres://localhost:${DB_PORT}/shop",
		},
		Renders: []Render{{Source: ".mcp.json.render", Target: ".mcp.json"}},
		Setup:   []SetupCommand{{Cmd: "direnv allow"}},
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

// A config written before the port tables existed must come back
// byte-for-byte: upgrading mws must not rewrite every .mws.toml.
func TestConfigWithoutPortTablesIsByteIdenticalAfterLoadAndSave(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &Config{
		ProjectName: "demo",
		Repos: []Repo{{
			Folder: "frontend",
			URL:    "git@github.com:example/frontend.git",
			Envs:   []EnvMapping{{Source: "a.env", Target: ".env"}},
			Setup:  []SetupCommand{{Cmd: "pnpm install"}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, c); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("config changed after load and save\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, key := range []string{"ports", "vars", "renders", "[[setup]]"} {
		if bytes.Contains(after, []byte(key)) {
			t.Fatalf("Save wrote %q for a config that had none:\n%s", key, after)
		}
	}
}

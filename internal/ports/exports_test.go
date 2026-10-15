package ports

import (
	"os"
	"slices"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"main":          "main",
		"Feature_Login": "feature-login",
		"cdx-fix.2":     "cdx-fix-2",
		"café":          "caf--",
		"A B":           "a-b",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

const shopConfig = `
[ports]
  [[ports.port]]
    name = "web"
    base = 5173
  [[ports.port]]
    name = "db"
    base = 5432
    var  = "DB_HOST_PORT"

[vars]
  DATABASE_URL         = "postgres://localhost:${DB_HOST_PORT}/shop"
  COMPOSE_PROJECT_NAME = "shop-${MWS_COPY_SLUG}"
`

func loadConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(config.Path(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

func TestNewExportsOrderAndValues(t *testing.T) {
	e := NewExports(loadConfig(t, shopConfig), "/work/shop", "Two", 1)
	want := []Var{
		{"MWS_SLOT", "1"},
		{"MWS_COPY", "Two"},
		{"MWS_COPY_SLUG", "two"},
		{"MWS_META", "/work/shop"},
		{"MWS_PORT_WEB", "5183"},
		{"MWS_PORT_DB", "5442"},
		{"DB_HOST_PORT", "5442"},
		{"COMPOSE_PROJECT_NAME", "shop-two"},
		{"DATABASE_URL", "postgres://localhost:5442/shop"},
	}
	if e.Slot != 1 || !slices.Equal(e.Vars, want) {
		t.Fatalf("NewExports =\n %+v\nwant slot 1,\n %+v", e, want)
	}
}

func TestNewExportsWithoutPorts(t *testing.T) {
	e := NewExports(loadConfig(t, "[vars]\n  NAME = \"x-${MWS_COPY}\"\n"), "/m", "main", 0)
	want := []Var{
		{"MWS_SLOT", "0"},
		{"MWS_COPY", "main"},
		{"MWS_COPY_SLUG", "main"},
		{"MWS_META", "/m"},
		{"NAME", "x-main"},
	}
	if !slices.Equal(e.Vars, want) {
		t.Fatalf("NewExports = %+v, want %+v", e.Vars, want)
	}
}

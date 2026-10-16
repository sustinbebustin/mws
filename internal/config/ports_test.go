package config

import (
	"bytes"
	"os"
	"reflect"
	"strings"
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

// validPorts returns a [ports] config that passes Validate; each case below
// breaks or adjusts one thing.
func validPorts() *Config {
	return &Config{
		ProjectName: "demo",
		Ports: &Ports{Port: []Port{
			{Name: "web", Base: 3000, Var: "WEB_PORT"},
			{Name: "db", Base: 5432},
		}},
	}
}

func TestValidatePortTables(t *testing.T) {
	cases := []struct {
		name string
		edit func(c *Config)
		// wantKey is the key the error must name; empty means Validate passes.
		wantKey string
	}{
		{"valid", func(c *Config) {}, ""},

		{"port name not an identifier", func(c *Config) { c.Ports.Port[0].Name = "web-app" }, "ports.port[0].name"},
		{"port name identifier with underscore", func(c *Config) { c.Ports.Port[0].Name = "web_app" }, ""},
		{"empty port name", func(c *Config) { c.Ports.Port[1].Name = "" }, "ports.port[1].name"},
		{"alias not an identifier", func(c *Config) { c.Ports.Port[0].Var = "1WEB" }, "ports.port[0].var"},

		{"duplicate port name", func(c *Config) { c.Ports.Port[1].Name = "web" }, "ports.port[1].name"},
		{"port names differing only in case export one variable", func(c *Config) { c.Ports.Port[1].Name = "WEB" }, "ports.port[1].name"},
		{"duplicate alias", func(c *Config) { c.Ports.Port[1].Var = "WEB_PORT" }, "ports.port[1].var"},
		{"alias equal to another port's variable", func(c *Config) { c.Ports.Port[1].Var = "MWS_PORT_WEB" }, "ports.port[1].var"},
		{"alias equal to a builtin", func(c *Config) { c.Ports.Port[1].Var = "MWS_SLOT" }, "ports.port[1].var"},
		{"alias equal to its own port variable", func(c *Config) { c.Ports.Port[1].Var = "MWS_PORT_DB" }, "ports.port[1].var"},
		{"distinct alias", func(c *Config) { c.Ports.Port[1].Var = "DB_PORT" }, ""},

		{"base 0", func(c *Config) { c.Ports.Port[1].Base = 0 }, "ports.port[1].base"},
		{"base above 65535", func(c *Config) { c.Ports.Port[1].Base = 65536 }, "ports.port[1].base"},
		{"base 65535", func(c *Config) { c.Ports.Port[1].Base = 65535 }, ""},
		{"base 1", func(c *Config) { c.Ports.Port[1].Base = 1 }, ""},

		{"stride 0", func(c *Config) { c.Ports.Stride = intp(0) }, "ports.stride"},
		{"stride 1 with one port", func(c *Config) { c.Ports.Stride = intp(1); c.Ports.Port = c.Ports.Port[:1] }, ""},
		{"slots 0", func(c *Config) { c.Ports.Slots = intp(0) }, "ports.slots"},
		{"slots 1", func(c *Config) { c.Ports.Slots = intp(1) }, ""},
		{"ports table without ports", func(c *Config) { c.Ports.Port = nil }, "ports.port"},

		// 3000 and 3020 at stride 10: slot 2 of web is slot 0 of db.
		{"congruent bases without a cap", func(c *Config) { c.Ports.Port[1].Base = 3020 }, "ports.port[1].base"},
		{"congruent bases capped below the first collision", func(c *Config) { c.Ports.Port[1].Base = 3020; c.Ports.Slots = intp(1) }, ""},
		{"congruent bases capped at the first collision", func(c *Config) { c.Ports.Port[1].Base = 3020; c.Ports.Slots = intp(2) }, "ports.slots"},
		{"congruent bases one stride apart", func(c *Config) { c.Ports.Port[1].Base = 3010; c.Ports.Slots = intp(1) }, "ports.port[1].base"},
		// 3000 and 64000 meet at slot 6100, past the 65535 bound (slot 153).
		{"congruent bases colliding beyond the 65535 bound", func(c *Config) { c.Ports.Port[1].Base = 64000 }, ""},
		{"equal bases", func(c *Config) { c.Ports.Port[1].Base = 3000; c.Ports.Slots = intp(1) }, "ports.port[1].base"},
		{"congruent under a custom stride", func(c *Config) { c.Ports.Stride = intp(100); c.Ports.Port[1].Base = 3100 }, "ports.port[1].base"},
		{"not congruent under a custom stride", func(c *Config) { c.Ports.Stride = intp(100); c.Ports.Port[1].Base = 3010 }, ""},

		{"vars referencing both port spellings and every builtin", func(c *Config) {
			c.Vars = map[string]string{
				"URL":  "http://localhost:${MWS_PORT_WEB}/${WEB_PORT}",
				"NAME": "${MWS_SLOT}-${MWS_COPY}-${MWS_COPY_SLUG}-${MWS_META}-${MWS_PORT_DB}",
			}
		}, ""},
		{"vars passing other dollar sequences through", func(c *Config) { c.Vars = map[string]string{"X": "$HOME ${a:-b} $ ${"} }, ""},
		{"var referencing an unknown name", func(c *Config) { c.Vars = map[string]string{"URL": "http://localhost:${API_PORT}"} }, "vars.URL"},
		{"var referencing another var", func(c *Config) {
			c.Vars = map[string]string{"A": "${MWS_PORT_WEB}", "B": "x${A}"}
		}, "vars.B"},
		{"var key not an identifier", func(c *Config) { c.Vars = map[string]string{"API-URL": "x"} }, "vars.API-URL"},
		{"var key shadowing a builtin", func(c *Config) { c.Vars = map[string]string{"MWS_SLOT": "x"} }, "vars.MWS_SLOT"},
		{"var key shadowing a port alias", func(c *Config) { c.Vars = map[string]string{"WEB_PORT": "x"} }, "vars.WEB_PORT"},
		{"vars without ports referencing a builtin", func(c *Config) {
			c.Ports = nil
			c.Vars = map[string]string{"P": "shop-${MWS_COPY_SLUG}"}
		}, ""},
		{"vars without ports referencing a port", func(c *Config) {
			c.Ports = nil
			c.Vars = map[string]string{"P": "${MWS_PORT_WEB}"}
		}, "vars.P"},

		{"render", func(c *Config) { c.Renders = []Render{{Source: ".mcp.json.render", Target: ".mcp.json"}} }, ""},
		{"render source in a harness subdirectory", func(c *Config) { c.Renders = []Render{{Source: "mcp/server.json.render", Target: ".mcp.json"}} }, ""},
		{"render source without the .render suffix", func(c *Config) { c.Renders = []Render{{Source: ".mcp.json", Target: ".mcp.json"}} }, "renders[0].source"},
		{"render source escaping the harness dir", func(c *Config) { c.Renders = []Render{{Source: "../x.render", Target: "x"}} }, "renders[0].source"},
		{"render source absolute", func(c *Config) { c.Renders = []Render{{Source: "/etc/x.render", Target: "x"}} }, "renders[0].source"},
		{"render target nested", func(c *Config) { c.Renders = []Render{{Source: "x.render", Target: "a/x"}} }, "renders[0].target"},
		{"render target dot-dot", func(c *Config) { c.Renders = []Render{{Source: "x.render", Target: ".."}} }, "renders[0].target"},
		{"render target empty", func(c *Config) { c.Renders = []Render{{Source: "x.render", Target: ""}} }, "renders[0].target"},
		{"render target an mws marker", func(c *Config) { c.Renders = []Render{{Source: "x.render", Target: ".mws-ports.env"}} }, "renders[0].target"},
		{"two renders onto one target", func(c *Config) {
			c.Renders = []Render{{Source: "a.render", Target: "x"}, {Source: "b.render", Target: "x"}}
		}, "renders[1].target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validPorts()
			tc.edit(c)
			err := c.Validate()
			if tc.wantKey == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate passed; want an error naming %s", tc.wantKey)
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Fatalf("error should name %s: %v", tc.wantKey, err)
			}
		})
	}
}

// supabaseAppConfig is the Next.js + Supabase monorepo example from ADR-0015.
const supabaseAppConfig = `project_name = "app"

[ports]
  stride = 10

  [[ports.port]]
    name = "internal"
    base = 3000
    var  = "APP_INTERNAL_PORT"
  [[ports.port]]
    name = "public"
    base = 3001
    var  = "APP_PUBLIC_PORT"
  [[ports.port]]
    name = "hs"
    base = 3002
    var  = "APP_HS_PORT"
  [[ports.port]]
    name = "backend"
    base = 8088
    var  = "APP_BACKEND_PORT"
  [[ports.port]]
    name = "supabase_api"
    base = 54321
    var  = "APP_SUPABASE_API_PORT"
  [[ports.port]]
    name = "supabase_db"
    base = 54322
    var  = "APP_SUPABASE_DB_PORT"
  [[ports.port]]
    name = "supabase_db_shadow"
    base = 54320
    var  = "APP_SUPABASE_DB_SHADOW_PORT"
  [[ports.port]]
    name = "supabase_studio"
    base = 54323
    var  = "APP_SUPABASE_STUDIO_PORT"

[vars]
  APP_SUPABASE_PROJECT_ID                 = "app-${MWS_COPY_SLUG}"
  APP_SUPABASE_API_URL                    = "http://localhost:${APP_SUPABASE_API_PORT}"
  APP_AUTH_SITE_URL                       = "http://127.0.0.1:${APP_INTERNAL_PORT}"
  APP_AUTH_REDIRECT_SITE                  = "https://127.0.0.1:${APP_INTERNAL_PORT}"
  APP_AUTH_REDIRECT_CALLBACK_IP           = "http://127.0.0.1:${APP_INTERNAL_PORT}/auth/callback"
  APP_AUTH_REDIRECT_CALLBACK_LOCALHOST    = "http://localhost:${APP_INTERNAL_PORT}/auth/callback"
  APP_AUTH_REDIRECT_HS_CALLBACK_IP        = "http://127.0.0.1:${APP_HS_PORT}/auth/callback"
  APP_AUTH_REDIRECT_HS_CALLBACK_LOCALHOST = "http://localhost:${APP_HS_PORT}/auth/callback"
  APP_SUPABASE_AZURE_REDIRECT_URI         = "http://localhost:${APP_SUPABASE_API_PORT}/auth/v1/callback"
  APP_AUTH_COOKIE_NAME                    = "app-auth-token-${APP_INTERNAL_PORT}"

[[renders]]
  source = ".mcp.json.render"
  target = ".mcp.json"

[[setup]]
  cmd = "direnv allow"
`

// shopConfig is the second, unrelated workspace from ADR-0015.
const shopConfig = `project_name = "shop"

[[repos]]
  folder = "web"
  url    = "git@github.com:example/shop-web.git"

  [[repos.envs]]
    source = ".env"
    target = ".env"

[ports]
  [[ports.port]]
    name = "web"
    base = 5173
  [[ports.port]]
    name = "db"
    base = 5432

[vars]
  COMPOSE_PROJECT_NAME = "shop-${MWS_COPY_SLUG}"
  DATABASE_URL         = "postgres://postgres:postgres@localhost:${MWS_PORT_DB}/shop"
`

func loadConfig(t *testing.T, body string) *Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

func TestPortPolicyEightPortApp(t *testing.T) {
	p, ok := loadConfig(t, supabaseAppConfig).PortPolicy()
	if !ok {
		t.Fatal("PortPolicy reported no [ports] for the eight-port app")
	}
	// The 65535 bound decides: the highest base, 54323, stays in range up to
	// slot 1121 (65533). Some pairs are congruent modulo 10 (3000 and 54320,
	// for one), but their first collision, slot 5132, lies above that bound.
	if p.Limit != 1121 {
		t.Fatalf("Limit = %d, want 1121", p.Limit)
	}
	if p.Stride != 10 {
		t.Fatalf("Stride = %d, want 10", p.Stride)
	}
	if len(p.Ports) != 8 || p.Ports[0].Name != "internal" || p.Ports[7].Name != "supabase_studio" {
		t.Fatalf("Ports not in declaration order: %+v", p.Ports)
	}
}

func TestPortPolicyShop(t *testing.T) {
	p, ok := loadConfig(t, shopConfig).PortPolicy()
	if !ok {
		t.Fatal("PortPolicy reported no [ports] for shop")
	}
	// Default stride 10; 5432 + 6010*10 = 65532, one more slot is 65542.
	if p.Stride != 10 || p.Limit != 6010 {
		t.Fatalf("policy = stride %d limit %d, want stride 10 limit 6010", p.Stride, p.Limit)
	}
}

func TestPortPolicySlotsCap(t *testing.T) {
	c := validPorts()
	c.Ports.Slots = intp(50)
	if p, _ := c.PortPolicy(); p.Limit != 50 {
		t.Fatalf("Limit = %d, want the ports.slots cap 50", p.Limit)
	}
	// A cap above the 65535 bound cannot raise it.
	c.Ports.Slots = intp(100000)
	if p, _ := c.PortPolicy(); p.Limit != 6010 {
		t.Fatalf("Limit = %d, want the 65535 bound 6010", p.Limit)
	}
}

func TestPortPolicyAbsent(t *testing.T) {
	if _, ok := (&Config{}).PortPolicy(); ok {
		t.Fatal("PortPolicy reported ports for a config without [ports]")
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

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

package config

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const configDoc = "../../docs/config.md"

// tomlBlocks returns the body of every ```toml fence in the markdown file at
// path, in order.
func tomlBlocks(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var blocks []string
	var cur []string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case !in && strings.TrimSpace(line) == "```toml":
			in, cur = true, nil
		case in && strings.TrimSpace(line) == "```":
			in = false
			blocks = append(blocks, strings.Join(cur, "\n"))
		case in:
			cur = append(cur, line)
		}
	}
	return blocks
}

// tomlKeys collects every toml tag name reachable from t.
func tomlKeys(t reflect.Type, into map[string]bool) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("toml"), ",")
		if name == "" || name == "-" {
			continue
		}
		into[name] = true
		tomlKeys(t.Field(i).Type, into)
	}
}

var (
	schemaTable = regexp.MustCompile(`^\[\[?([A-Za-z0-9_.]+)\]\]?`)
	schemaKey   = regexp.MustCompile(`^([A-Za-z0-9_]+)\s*=`)
)

func TestConfigDocSchemaListsEveryConfigKey(t *testing.T) {
	blocks := tomlBlocks(t, configDoc)
	if len(blocks) == 0 {
		t.Fatalf("%s has no toml block", configDoc)
	}
	documented := map[string]bool{}
	for _, line := range strings.Split(blocks[0], "\n") {
		line = strings.TrimSpace(line)
		if m := schemaTable.FindStringSubmatch(line); m != nil {
			parts := strings.Split(m[1], ".")
			documented[parts[len(parts)-1]] = true
		} else if m := schemaKey.FindStringSubmatch(line); m != nil {
			documented[m[1]] = true
		}
	}

	actual := map[string]bool{}
	tomlKeys(reflect.TypeOf(Config{}), actual)

	if missing := difference(actual, documented); len(missing) > 0 {
		t.Errorf("schema block in %s lacks config keys %v", configDoc, missing)
	}
	if extra := difference(documented, actual); len(extra) > 0 {
		t.Errorf("schema block in %s lists keys the config types do not have: %v", configDoc, extra)
	}
}

func difference(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Every example after the schema block must load as a real config, so an
// example never teaches a rule Validate rejects.
func TestConfigDocExamplesLoadUnderValidate(t *testing.T) {
	blocks := tomlBlocks(t, configDoc)
	if len(blocks) < 4 {
		t.Fatalf("%s has %d toml blocks, want the schema plus at least three examples", configDoc, len(blocks))
	}
	for i, block := range blocks[1:] {
		if _, err := parse(configDoc, []byte(block)); err != nil {
			t.Errorf("toml example %d in %s does not load: %v\n%s", i+1, configDoc, err, block)
		}
	}
}

func TestReadmeConfigExampleLoadsUnderValidate(t *testing.T) {
	const readme = "../../README.md"
	for i, block := range tomlBlocks(t, readme) {
		if _, err := parse(readme, []byte(block)); err != nil {
			t.Errorf("toml example %d in %s does not load: %v", i, readme, err)
		}
	}
}

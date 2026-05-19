// Package config defines the mws config file format and load/save helpers.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ConfigFileName is the on-disk name of the mws config file, at the meta root.
const ConfigFileName = ".mws.toml"

// Config is the persisted mws config, stored at <meta>/.mws.toml.
type Config struct {
	ProjectName string `toml:"project_name"`
	Description string `toml:"description"`
	// WorkingCopiesDir is an optional single path-safe segment under the meta
	// root where `mws clone` and `mws init` place new working copies. Must
	// satisfy project.ValidateName when non-empty (same alphabet as project
	// and peer names). Empty means working copies live directly under the
	// meta root.
	WorkingCopiesDir string `toml:"working_copies_dir,omitempty"`
	Repos            []Repo `toml:"repos"`
}

// Repo identifies one native git repo by its target folder and clone URL.
// Envs lists optional staged-env-file mappings to materialise when a working
// copy is created or refreshed. Setup lists optional shell commands to run in
// the cloned repo directory at the end of `mws clone`.
type Repo struct {
	Folder string         `toml:"folder"`
	URL    string         `toml:"url"`
	Envs   []EnvMapping   `toml:"envs,omitempty"`
	Setup  []SetupCommand `toml:"setup,omitempty"`
}

// EnvMapping maps a flat-named env file in `.envs/<repo>/` (Source) to its
// target path inside the repo working tree (Target).
type EnvMapping struct {
	Source string `toml:"source"`
	Target string `toml:"target"`
}

// SetupCommand is one shell command run by `mws clone` against the new working
// copy's native repo directory after clone and env-copy succeed.
type SetupCommand struct {
	Cmd string `toml:"cmd"`
}

// Path returns the absolute path to the config file inside a meta directory.
func Path(metaRoot string) string {
	return filepath.Join(metaRoot, ConfigFileName)
}

// Load reads and parses the config at <metaRoot>/.mws.toml.
func Load(metaRoot string) (*Config, error) {
	p := Path(metaRoot)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &c, nil
}

// Save writes the config to <metaRoot>/.mws.toml.
func Save(metaRoot string, c *Config) error {
	if err := os.MkdirAll(metaRoot, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", metaRoot, err)
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	p := Path(metaRoot)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	return nil
}

// AddRepo appends a repo to the config if a repo with the same folder is not already present.
// Returns true if the repo was added, false if it was a duplicate.
func (c *Config) AddRepo(r Repo) bool {
	for _, existing := range c.Repos {
		if existing.Folder == r.Folder {
			return false
		}
	}
	c.Repos = append(c.Repos, r)
	return true
}

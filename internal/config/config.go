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
	// OptionalRepos lists native repos that are NOT cloned into every working
	// copy. They are pulled into a specific copy on demand: chosen at
	// `mws clone` time (the prompt or --with) or added later with
	// `mws include`. Same shape as Repos; omitted from TOML when empty.
	OptionalRepos []Repo `toml:"optional_repos,omitempty"`
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

// folderRegistered reports whether folder is already used by any repo in the
// config, default or optional. A folder maps to a single clone-target dir name
// inside a working copy, so it must be unique across the whole config.
func (c *Config) folderRegistered(folder string) bool {
	for _, r := range c.Repos {
		if r.Folder == folder {
			return true
		}
	}
	for _, r := range c.OptionalRepos {
		if r.Folder == folder {
			return true
		}
	}
	return false
}

// AddRepo appends a repo to the config if its folder is not already registered
// (in either Repos or OptionalRepos). Returns true if added, false on duplicate.
func (c *Config) AddRepo(r Repo) bool {
	if c.folderRegistered(r.Folder) {
		return false
	}
	c.Repos = append(c.Repos, r)
	return true
}

// AddOptionalRepo appends a repo to OptionalRepos if its folder is not already
// registered (in either list). Returns true if added, false on duplicate.
func (c *Config) AddOptionalRepo(r Repo) bool {
	if c.folderRegistered(r.Folder) {
		return false
	}
	c.OptionalRepos = append(c.OptionalRepos, r)
	return true
}

// OptionalRepo returns the registered optional repo with the given folder.
func (c *Config) OptionalRepo(folder string) (Repo, bool) {
	for _, r := range c.OptionalRepos {
		if r.Folder == folder {
			return r, true
		}
	}
	return Repo{}, false
}

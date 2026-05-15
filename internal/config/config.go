// Package config defines the mws config file format and load/save helpers.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// FileName is the on-disk name of the mws config file (inside the meta's .mws/ dir).
const FileName = "config.toml"

// DirName is the directory inside the meta that holds the config.
const DirName = ".mws"

// Config is the persisted mws config, stored at <meta>/.mws/config.toml.
type Config struct {
	ProjectName string `toml:"project_name"`
	Description string `toml:"description"`
	Repos       []Repo `toml:"repos"`
}

// Repo identifies one native git repo by its target folder and clone URL.
type Repo struct {
	Folder string `toml:"folder"`
	URL    string `toml:"url"`
}

// Path returns the absolute path to the config file inside a meta directory.
func Path(metaRoot string) string {
	return filepath.Join(metaRoot, DirName, FileName)
}

// Load reads and parses the config at <metaRoot>/.mws/config.toml.
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

// Save writes the config to <metaRoot>/.mws/config.toml, creating the directory if needed.
func Save(metaRoot string, c *Config) error {
	dir := filepath.Join(metaRoot, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	p := filepath.Join(dir, FileName)
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

// Package config defines the mws config file format and load/save helpers.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// Trash tunes the soft-delete behaviour of `mws rm`. An absent table means
	// defaults; read it through TrashPolicy rather than dereferencing here.
	Trash *Trash `toml:"trash,omitempty"`
	// Worktrees tunes `mws worktree`. An absent table means defaults; read it
	// through WorktreePolicy rather than dereferencing here.
	Worktrees *Worktrees `toml:"worktrees,omitempty"`
	// Ports declares the named ports a working copy shifts when it holds a
	// port slot (ADR-0015). Absent means no port slots; read it through
	// PortPolicy rather than dereferencing here.
	Ports *Ports `toml:"ports,omitempty"`
	// Vars are derived strings exported with every port lease. A value may
	// reference port variables and builtins as ${NAME}, never another var.
	Vars map[string]string `toml:"vars,omitempty"`
	// Renders lists harness files rendered per working copy instead of
	// symlinked.
	Renders []Render `toml:"renders,omitempty"`
	// Setup lists commands run at the working copy root after every render.
	Setup []SetupCommand `toml:"setup,omitempty"`
}

// Default worktree settings, applied when the `[worktrees]` table or one of
// its keys is absent.
const (
	DefaultWorktreesDir         = ".wt"
	DefaultWorktreeBranchPrefix = "wt/"
)

// Worktrees is the raw on-disk `[worktrees]` table. Pointer fields separate
// "absent" (take the default) from an explicit value; callers use
// Config.WorktreePolicy instead of reading these fields.
type Worktrees struct {
	// Dir is the directory inside each working copy that holds its worktrees.
	Dir *string `toml:"dir,omitempty"`
	// BranchPrefix is prepended to a worktree's name to form its default
	// branch in every native repo.
	BranchPrefix *string `toml:"branch_prefix,omitempty"`
	// RepoSetup controls whether [[repos.setup]] commands run inside each
	// inner repo of a new worktree.
	RepoSetup *bool `toml:"repo_setup,omitempty"`
	// Setup lists commands run in the worktree root after creation.
	Setup []SetupCommand `toml:"setup,omitempty"`
}

// WorktreePolicy is the resolved worktree behaviour, with defaults applied.
type WorktreePolicy struct {
	Dir          string
	BranchPrefix string
	RepoSetup    bool
	Setup        []SetupCommand
}

// WorktreePolicy resolves the effective worktree policy for this config,
// applying the defaults for anything the config leaves unset.
func (c *Config) WorktreePolicy() WorktreePolicy {
	p := WorktreePolicy{Dir: DefaultWorktreesDir, BranchPrefix: DefaultWorktreeBranchPrefix, RepoSetup: true}
	w := c.Worktrees
	if w == nil {
		return p
	}
	if w.Dir != nil {
		p.Dir = *w.Dir
	}
	if w.BranchPrefix != nil {
		p.BranchPrefix = *w.BranchPrefix
	}
	if w.RepoSetup != nil {
		p.RepoSetup = *w.RepoSetup
	}
	p.Setup = w.Setup
	return p
}

// validateWorktreesDir checks that dir is one path segment made of a single
// leading '.' followed by letters, digits, '-', '_' or '.'. The leading dot
// keeps the directory out of working-copy enumeration and default searches.
func validateWorktreesDir(dir string) error {
	rest, ok := strings.CutPrefix(dir, ".")
	if !ok || rest == "" || strings.HasPrefix(rest, ".") {
		return fmt.Errorf("worktrees.dir is %q; must be a single path segment starting with one '.' (e.g. %q)", dir, DefaultWorktreesDir)
	}
	for _, r := range rest {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("worktrees.dir is %q; invalid character %q (allowed after the leading '.': letters, digits, '-', '_', '.')", dir, r)
		}
	}
	return nil
}

// DefaultTrashRetention is how long a trashed working copy survives before an
// opportunistic sweep purges it, when retention_days is not set.
const DefaultTrashRetention = 7 * 24 * time.Hour

// Trash is the raw on-disk `[trash]` table. Its zero value is not the default
// policy, and neither is a zero RetentionDays -- absence is. Callers use
// Config.TrashPolicy instead of reading these fields.
type Trash struct {
	// RetentionDays is how many days a trashed working copy is kept. It is a
	// pointer because "absent" and "explicitly 0" are different facts: absent
	// means the default retention, 0 means keep forever. Writing `[trash]` for
	// any other key must not silently change how long copies are kept.
	RetentionDays *int `toml:"retention_days,omitempty"`
	// Disabled makes `mws rm` delete a working copy outright instead of trashing it.
	Disabled bool `toml:"disabled,omitempty"`
}

// TrashPolicy is the resolved trash behaviour, with the absent-table defaults
// already applied so no caller has to reason about nil-vs-zero.
type TrashPolicy struct {
	// Enabled reports whether `mws rm` moves a working copy to the trash. When
	// false, rm deletes outright; `mws restore` and `mws trash` still operate
	// on entries trashed earlier.
	Enabled bool
	// Retention is how long an entry survives. Zero means never auto-purge.
	Retention time.Duration
}

// TrashPolicy resolves the effective trash policy for this config, applying
// the defaults for anything the config leaves unset.
func (c *Config) TrashPolicy() TrashPolicy {
	p := TrashPolicy{Enabled: true, Retention: DefaultTrashRetention}
	if c.Trash == nil {
		return p
	}
	p.Enabled = !c.Trash.Disabled
	if c.Trash.RetentionDays != nil {
		p.Retention = time.Duration(*c.Trash.RetentionDays) * 24 * time.Hour
	}
	return p
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
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return &c, nil
}

// Validate checks config values that must be rejected at load time rather than
// silently coerced. Load calls it, so a bad value surfaces at the point of
// reading the file instead of at the point of acting on it.
func (c *Config) Validate() error {
	if c.Trash != nil && c.Trash.RetentionDays != nil && *c.Trash.RetentionDays < 0 {
		return fmt.Errorf("trash.retention_days is %d; must be 0 (keep forever) or a positive number of days", *c.Trash.RetentionDays)
	}
	if c.Worktrees != nil && c.Worktrees.Dir != nil {
		if err := validateWorktreesDir(*c.Worktrees.Dir); err != nil {
			return err
		}
	}
	return nil
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

// RepoList names the list a repo is registered in.
type RepoList int

const (
	// ListDefault is [[repos]], cloned into every working copy.
	ListDefault RepoList = iota
	// ListOptional is [[optional_repos]], cloned on demand.
	ListOptional
)

// Registration is the outcome of Register.
type Registration int

const (
	// Registered means the repo was appended.
	Registered Registration = iota
	// AlreadyRegistered means the same folder and URL are already in the
	// same list; nothing changed.
	AlreadyRegistered
	// FolderTaken means the folder is registered with another URL or in the
	// other list; nothing changed.
	FolderTaken
)

// Register appends r to list unless its folder is already registered in
// either list; folders are unique across both.
func (c *Config) Register(r Repo, list RepoList) Registration {
	target := &c.Repos
	if list == ListOptional {
		target = &c.OptionalRepos
	}
	for _, x := range *target {
		if x.Folder == r.Folder && x.URL == r.URL {
			return AlreadyRegistered
		}
	}
	if c.folderRegistered(r.Folder) {
		return FolderTaken
	}
	*target = append(*target, r)
	return Registered
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

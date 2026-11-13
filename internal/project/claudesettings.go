package project

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/sustinbebustin/mws/internal/atomicfile"
)

// claudeLocalSettings is the subset of Claude Code's settings.local.json that
// mws writes.
type claudeLocalSettings struct {
	Permissions struct {
		AdditionalDirectories []string `json:"additionalDirectories"`
	} `json:"permissions"`
}

// WriteClaudeLocalSettings writes <metaRoot>/.claude/settings.local.json
// granting Claude Code access to the harness dir. Working copies reach the
// harness through symlinks that resolve outside the copy, so without the grant
// every edit to a harness file prompts for permission. The file holds an
// absolute path, which is why it is a local (untracked) settings file.
func WriteClaudeLocalSettings(metaRoot string) error {
	var s claudeLocalSettings
	s.Permissions.AdditionalDirectories = []string{filepath.Join(metaRoot, HarnessDirName)}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Claude settings: %w", err)
	}
	dst := filepath.Join(metaRoot, ".claude", "settings.local.json")
	if err := atomicfile.Write(dst, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write Claude settings %s: %w", dst, err)
	}
	return nil
}

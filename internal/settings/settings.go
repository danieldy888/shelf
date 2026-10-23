// Package settings keeps the user's preferences in settings.json inside the
// data directory, next to the shelf's own state.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FileName is the preferences file inside the data directory.
const FileName = "settings.json"

// Settings are the user-adjustable preferences. Every field must have its
// default in Default: a key missing from the file keeps that default.
type Settings struct {
	// RetentionDays removes items older than this many days; 0 or less
	// keeps everything.
	RetentionDays int `json:"retentionDays"`
	// ClipboardLimit caps how many unpinned clipboard texts are kept;
	// values below 1 load as the default.
	ClipboardLimit int `json:"clipboardLimit"`
	// ExcludedApps lists bundle identifiers whose copies are never
	// recorded into the shelf.
	ExcludedApps []string `json:"excludedApps"`
	// IslandNowPlaying shows the now-playing peek player and panel card.
	IslandNowPlaying bool `json:"islandNowPlaying"`
	// AutoCheckUpdates checks the GitHub release feed for a newer version
	// in the background; it is the app's only network access.
	AutoCheckUpdates bool `json:"autoCheckUpdates"`
	// RemoveAfterDrag takes an item off the tray once it was dragged out
	// and dropped; pinned items stay.
	RemoveAfterDrag bool `json:"removeAfterDrag"`
	// AIUsage is the AI 用量 page: off by default; each source (quota and
	// local usage) can be switched separately.
	AIUsage AIUsageSettings `json:"aiUsage"`
}

// AIUsageSettings is the aiUsage object in settings.json. Enabled turns
// the AI 用量 page on; the rest pick which data the page reads: the Codex
// and Claude Code subscription quotas, and the local token usage of omp,
// Claude Code and Codex.
type AIUsageSettings struct {
	Enabled     bool `json:"enabled"`
	QuotaCodex  bool `json:"quotaCodex"`
	QuotaClaude bool `json:"quotaClaude"`
	LocalOmp    bool `json:"localOmp"`
	LocalClaude bool `json:"localClaude"`
	LocalCodex  bool `json:"localCodex"`
}

// defaultClipboardLimit is the clipboard text history cap a fresh
// install uses.
const defaultClipboardLimit = 30

// Default is what a fresh install uses.
func Default() Settings {
	return Settings{
		ClipboardLimit:   defaultClipboardLimit,
		IslandNowPlaying: true,
		AutoCheckUpdates: true,
		RemoveAfterDrag:  false,
		AIUsage: AIUsageSettings{
			Enabled:     false,
			QuotaCodex:  true,
			QuotaClaude: true,
			LocalOmp:    true,
			LocalClaude: true,
			LocalCodex:  true,
		},
	}
}

// Save replaces dir/settings.json with s, atomically.
func Save(dir string, s Settings) error {
	if err := write(dir, s); err != nil {
		return fmt.Errorf("settings: saving %s: %w", FileName, err)
	}
	return nil
}

// Load reads dir/settings.json over the defaults.
//
// A missing file is created with the defaults so the user can find and edit
// it. A file that cannot be read or parsed is never rewritten: Load returns
// the defaults together with the error, and the caller decides how to tell
// the user.
func Load(dir string) (Settings, error) {
	s := Default()
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if werr := write(dir, s); werr != nil {
			return s, fmt.Errorf("settings: creating %s: %w", FileName, werr)
		}
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("settings: reading %s: %w", FileName, err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return Default(), fmt.Errorf("settings: %s is not valid JSON, using defaults: %w", FileName, err)
	}
	if s.ClipboardLimit < 1 {
		s.ClipboardLimit = defaultClipboardLimit
	}
	return s, nil
}

// write saves s atomically: a temporary file in dir renamed over the target,
// so a crash never leaves a half-written settings.json.
func write(dir string, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, "."+FileName+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, FileName))
}

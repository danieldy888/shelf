package aiusage

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClaudeUsedSince reports whether Claude Code was used after t: one of its
// sessions (a .jsonl under projects/) was written to since then. An
// allowance nobody used has not moved, so an automatic /usage read is not
// worth its process.
func ClaudeUsedSince(configDir, home string, since time.Time) bool {
	if configDir == "" {
		configDir = filepath.Join(home, ".claude")
	}
	projects := filepath.Join(configDir, "projects")
	dirs, _ := os.ReadDir(projects)
	for _, d := range dirs {
		root := filepath.Join(projects, d.Name())
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			continue
		}
		files, _ := os.ReadDir(root)
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			if fi, err := f.Info(); err == nil && fi.ModTime().After(since) {
				return true
			}
		}
	}
	return false
}

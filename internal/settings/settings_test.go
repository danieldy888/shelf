package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingFileIsCreatedWithDefaults(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s != Default() {
		t.Fatalf("fresh load = %+v, want defaults %+v", s, Default())
	}
	again, err := Load(dir)
	if err != nil || again != Default() {
		t.Fatalf("reloading the created file = %+v, %v", again, err)
	}
}

func TestMissingKeyKeepsDefault(t *testing.T) {
	// A file from an older version (or hand-edited) without the key must not
	// silently turn a feature off through Go's zero value.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"somethingElse": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.ClipboardHistory {
		t.Fatal("missing clipboardHistory turned clipboard history off")
	}
}

func TestExplicitValueIsHonoured(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"clipboardHistory": false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.ClipboardHistory {
		t.Fatal("clipboardHistory: false was ignored")
	}
}

func TestInvalidFileIsNeverRewritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	bad := []byte(`{"clipboardHistory": fals`)
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err == nil {
		t.Fatal("invalid JSON must be reported")
	}
	if s != Default() {
		t.Fatalf("invalid file gave %+v, want defaults", s)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(bad) {
		t.Fatalf("invalid settings.json was overwritten: %q", got)
	}
}

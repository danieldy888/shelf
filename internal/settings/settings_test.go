package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMissingFileIsCreatedWithDefaults(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, Default()) {
		t.Fatalf("fresh load = %+v, want defaults %+v", s, Default())
	}
	again, err := Load(dir)
	if err != nil || !reflect.DeepEqual(again, Default()) {
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
	if s.ClipboardLimit != 30 {
		t.Fatalf("missing clipboardLimit = %d, want 30", s.ClipboardLimit)
	}
	if len(s.ExcludedApps) != 0 {
		t.Fatalf("missing excludedApps = %v, want empty", s.ExcludedApps)
	}
	if !s.IslandNowPlaying {
		t.Fatalf("missing islandNowPlaying must stay on: %+v", s)
	}
}

// The now-playing toggle still loads from the file, and the removed
// battery/volume island keys in a legacy settings.json are ignored
// (unknown keys never fail a load) while the rest keep their defaults.
func TestNowPlayingToggleAndLegacyIslandKeysLoad(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"islandNowPlaying": false, "islandBattery": false, "islandVolume": false}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.IslandNowPlaying {
		t.Fatal("islandNowPlaying: false was ignored")
	}
	if !s.ClipboardHistory || !s.AutoCheckUpdates {
		t.Fatalf("unrelated keys must keep their defaults: %+v", s)
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

// autoCheckUpdates defaults on so updates reach users; a file without the
// key (older versions, hand edits) must not silently disable it, and an
// explicit false is honoured.
func TestAutoCheckUpdatesLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"clipboardHistory": false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil || !s.AutoCheckUpdates {
		t.Fatalf("missing autoCheckUpdates = %v, %v; want true", s.AutoCheckUpdates, err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"autoCheckUpdates": false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err = Load(dir)
	if err != nil || s.AutoCheckUpdates {
		t.Fatalf("autoCheckUpdates: false ignored: %v, %v", s.AutoCheckUpdates, err)
	}
}

// aiUsage is a nested object: a file without the key (every install before
// the AI 用量 page) must load the page's defaults — the feature off, every
// source on — never Go's zero value. Partial objects keep defaults for
// their missing keys too, and an explicit enabled is honoured.
func TestAIUsageLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"clipboardHistory": false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.AIUsage.Enabled {
		t.Fatal("missing aiUsage turned the page on")
	}
	for name, on := range map[string]bool{
		"quotaCodex": s.AIUsage.QuotaCodex, "quotaClaude": s.AIUsage.QuotaClaude,
		"localOmp": s.AIUsage.LocalOmp, "localClaude": s.AIUsage.LocalClaude, "localCodex": s.AIUsage.LocalCodex,
	} {
		if !on {
			t.Fatalf("missing aiUsage.%s loaded as off", name)
		}
	}
	// A partial object fills only the keys it sets.
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"aiUsage": {"enabled": true, "localCodex": false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err = Load(dir)
	if err != nil || !s.AIUsage.Enabled || s.AIUsage.LocalCodex || !s.AIUsage.QuotaCodex || !s.AIUsage.LocalOmp {
		t.Fatalf("partial aiUsage = %+v, %v; want enabled, localCodex off, the rest on", s.AIUsage, err)
	}
}

func TestSaveRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := Settings{ClipboardHistory: false, RetentionDays: 7, ClipboardLimit: 12, ExcludedApps: []string{"com.example.one", "com.example.two"}}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load after Save = %+v, %v; want %+v", got, err, want)
	}
}

// A stored clipboardLimit below 1 means "use the default", so the file
// can never disable or blow up the history cap by hand-editing.
func TestNonPositiveClipboardLimitLoadsAsDefault(t *testing.T) {
	for _, jsonLimit := range []string{"0", "-5"} {
		dir := t.TempDir()
		data := []byte(`{"clipboardLimit": ` + jsonLimit + `}`)
		if err := os.WriteFile(filepath.Join(dir, FileName), data, 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if s.ClipboardLimit != 30 {
			t.Fatalf("clipboardLimit %s loaded as %d, want 30", jsonLimit, s.ClipboardLimit)
		}
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
	if !reflect.DeepEqual(s, Default()) {
		t.Fatalf("invalid file gave %+v, want defaults", s)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(bad) {
		t.Fatalf("invalid settings.json was overwritten: %q", got)
	}
}

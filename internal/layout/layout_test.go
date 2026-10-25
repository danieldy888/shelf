package layout

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func cardIDs(l Layout) []string {
	out := make([]string, len(l.Cards))
	for i, c := range l.Cards {
		out[i] = c.ID
	}
	return out
}

func pinIDs(l Layout) []string {
	out := make([]string, len(l.Pins))
	for i, p := range l.Pins {
		out[i] = p.ID
	}
	return out
}

func homeIDs(l Layout) []string {
	out := make([]string, len(l.Home))
	for i, w := range l.Home {
		out[i] = w.ID
	}
	return out
}

// writeFile stores v as JSON in dir/layout.json and returns the bytes.
func writeFile(t *testing.T, dir string, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return data
}

func readFile(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertNoOverlap(t *testing.T, l Layout) {
	t.Helper()
	for i := range l.Home {
		for j := i + 1; j < len(l.Home); j++ {
			a, b := l.Home[i], l.Home[j]
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				t.Fatalf("home widgets %s and %s overlap: %+v", a.ID, b.ID, l.Home)
			}
		}
	}
}

func TestDefault(t *testing.T) {
	l := Default()
	if l.Version != 2 || Version != 2 {
		t.Fatalf("version = %d, want 2", l.Version)
	}
	wantCards := []string{"ocr", "fit1m", "topdf", "zip", "tojpeg", "toword", "extract", "resize"}
	if !slices.Equal(cardIDs(l), wantCards) {
		t.Fatalf("cards = %v, want %v", cardIDs(l), wantCards)
	}
	for _, c := range l.Cards {
		if c.Span != 1 {
			t.Fatalf("card %s span = %d, want 1", c.ID, c.Span)
		}
	}
	wantHome := []Widget{
		{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		{ID: "usage", X: 3, Y: 0, W: 1, H: 1},
		{ID: "recent", X: 2, Y: 1, W: 2, H: 1},
	}
	if !slices.Equal(l.Home, wantHome) {
		t.Fatalf("home = %+v, want %+v", l.Home, wantHome)
	}
	if len(l.Pins) != 0 {
		t.Fatalf("default has pins: %+v", l.Pins)
	}
}

func TestDefaultWith(t *testing.T) {
	withAI := DefaultWith(true)
	if !slices.Equal(withAI.Cards, Default().Cards) || !slices.Equal(withAI.Home, Default().Home) {
		t.Fatalf("DefaultWith(true) differs from Default: %+v", withAI)
	}
	noAI := DefaultWith(false)
	if noAI.Version != Version {
		t.Fatalf("version = %d", noAI.Version)
	}
	if !slices.Equal(cardIDs(noAI), cardIDs(Default())) {
		t.Fatalf("cards = %v", cardIDs(noAI))
	}
	wantHome := []Widget{
		{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		{ID: "clip", X: 2, Y: 1, W: 2, H: 1},
	}
	if !slices.Equal(noAI.Home, wantHome) {
		t.Fatalf("home = %+v, want %+v", noAI.Home, wantHome)
	}
}

func TestLoadCreatesDefault(t *testing.T) {
	dir := t.TempDir()
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cardIDs(l), cardIDs(Default())) {
		t.Fatalf("cards = %v, want default %v", cardIDs(l), cardIDs(Default()))
	}
	if !slices.Equal(l.Home, Default().Home) {
		t.Fatalf("home = %+v, want default %+v", l.Home, Default().Home)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("layout.json was not written: %v", err)
	}
}

func TestLoadWithCreatesAvailabilityDefault(t *testing.T) {
	for _, aiUsage := range []bool{true, false} {
		dir := t.TempDir()
		l, err := LoadWith(dir, aiUsage)
		if err != nil {
			t.Fatalf("aiUsage %v: %v", aiUsage, err)
		}
		if !slices.Equal(l.Home, DefaultWith(aiUsage).Home) {
			t.Fatalf("aiUsage %v: home = %+v, want %+v", aiUsage, l.Home, DefaultWith(aiUsage).Home)
		}
		var onDisk Layout
		if err := json.Unmarshal(readFile(t, dir), &onDisk); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(onDisk.Home, DefaultWith(aiUsage).Home) {
			t.Fatalf("aiUsage %v: file holds %+v, want %+v", aiUsage, onDisk.Home, DefaultWith(aiUsage).Home)
		}
	}
}

func TestLoadWithBrokenFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := LoadWith(dir, false)
	if err == nil {
		t.Fatal("expected an error for corrupt JSON")
	}
	if !slices.Equal(l.Home, DefaultWith(false).Home) {
		t.Fatalf("home = %+v, want %+v", l.Home, DefaultWith(false).Home)
	}
}

func TestWriteThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	l := Default()
	l.MoveCard("resize", 0)
	l.SetSpan("ocr", 2)
	l.AddPin(Pin{ID: "clip", Span: 2, Edge: EdgeRight, X: 4, Y: 120, AlwaysOpen: true})
	if err := Write(dir, l); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cardIDs(got), cardIDs(l)) {
		t.Fatalf("cards = %v, want %v", cardIDs(got), cardIDs(l))
	}
	if got.Cards[slices.Index(cardIDs(got), "ocr")].Span != 2 {
		t.Fatal("ocr span did not persist")
	}
	if !slices.Equal(got.Home, l.Home) {
		t.Fatalf("home = %+v, want %+v", got.Home, l.Home)
	}
	if len(got.Pins) != 1 || got.Pins[0] != l.Pins[0] {
		t.Fatalf("pins = %+v", got.Pins)
	}
}

func TestLoadBadJSONKeepsFileAndDefaults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	if err := os.WriteFile(p, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Load(dir)
	if err == nil {
		t.Fatal("expected an error for corrupt JSON")
	}
	if !slices.Equal(cardIDs(l), cardIDs(Default())) || !slices.Equal(l.Home, Default().Home) {
		t.Fatalf("got %+v, want defaults", l)
	}
	if data, _ := os.ReadFile(p); string(data) != "{nope" {
		t.Fatal("corrupt file was overwritten")
	}
}

func TestLoadFutureVersionDefaultsAndKeepsFile(t *testing.T) {
	dir := t.TempDir()
	raw := writeFile(t, dir, map[string]any{
		"version": 3,
		"cards":   []Card{{ID: "zip"}},
		"home":    []Widget{{ID: "playing", X: 0, Y: 0, W: 2, H: 2}},
	})
	l, err := Load(dir)
	if err == nil {
		t.Fatal("expected a version error")
	}
	if !slices.Equal(cardIDs(l), cardIDs(Default())) || !slices.Equal(l.Home, Default().Home) {
		t.Fatalf("got %+v, want defaults", l)
	}
	if got := readFile(t, dir); string(got) != string(raw) {
		t.Fatalf("future-version file changed: %s", got)
	}
	noAI, err := LoadWith(dir, false)
	if err == nil || !slices.Equal(noAI.Home, DefaultWith(false).Home) {
		t.Fatalf("LoadWith future version: home = %+v, err = %v", noAI.Home, err)
	}
}

// A file written before version stamps existed (version 0) is migrated
// like a version 1 file: known widgets move to the home canvas, actions
// stay on the tool page, and Load does not rewrite the file.
func TestLoadUnversionedFileMigrates(t *testing.T) {
	dir := t.TempDir()
	raw := writeFile(t, dir, map[string]any{
		"cards": []Card{{ID: "zip", Span: 7}, {ID: "clip"}},
	})
	l, err := Load(dir)
	if err != nil {
		t.Fatalf("unversioned file rejected: %v", err)
	}
	if !slices.Equal(cardIDs(l), []string{"zip"}) || l.Cards[0].Span != 1 {
		t.Fatalf("unversioned cards not migrated: %+v", l.Cards)
	}
	if !slices.Equal(l.Home, []Widget{{ID: "clip", X: 2, Y: 1, W: 2, H: 1}}) {
		t.Fatalf("home = %+v, want clip at its default", l.Home)
	}
	if l.Version != Version {
		t.Fatalf("version = %d, want %d", l.Version, Version)
	}
	if got := readFile(t, dir); string(got) != string(raw) {
		t.Fatal("Load rewrote the migrated file")
	}
}

func TestMigrateV1ActionsWidgetsPins(t *testing.T) {
	dir := t.TempDir()
	pins := []Pin{{ID: "fit1m", Span: 2, Edge: EdgeLeft, X: 12, Y: 40, AlwaysOpen: true}}
	raw := writeFile(t, dir, Layout{
		Version: 1,
		Cards: []Card{
			{ID: "ocr", Span: 1},
			{ID: "playing", Span: 2},
			{ID: "zip", Span: 0},
			{ID: "awake", Span: 1},
			{ID: "tojpeg", Span: 1},
			{ID: "clip", Span: 2},
		},
		Pins: pins,
	})
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Version != 2 {
		t.Fatalf("version = %d, want 2", l.Version)
	}
	if got, want := cardIDs(l), []string{"ocr", "zip", "tojpeg"}; !slices.Equal(got, want) {
		t.Fatalf("cards = %v, want %v", got, want)
	}
	for _, c := range l.Cards {
		if c.Span != 1 {
			t.Fatalf("card %s span = %d, want clamped 1", c.ID, c.Span)
		}
	}
	wantHome := []Widget{
		{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		{ID: "clip", X: 2, Y: 1, W: 2, H: 1},
	}
	if !slices.Equal(l.Home, wantHome) {
		t.Fatalf("home = %+v, want %+v", l.Home, wantHome)
	}
	assertNoOverlap(t, l)
	if !slices.Equal(l.Pins, pins) {
		t.Fatalf("pins = %+v, want %+v", l.Pins, pins)
	}
	if got := readFile(t, dir); string(got) != string(raw) {
		t.Fatal("Load rewrote the migrated file")
	}
}

func TestMigrateV1AllWidgetsDropsLaterOverlap(t *testing.T) {
	orders := map[string]struct {
		cards []Card
		drops string
	}{
		"recent before clip": {
			cards: []Card{
				{ID: "playing"}, {ID: "awake"}, {ID: "usage"}, {ID: "recent"}, {ID: "clip"},
			},
			drops: "clip",
		},
		"clip before recent": {
			cards: []Card{
				{ID: "playing"}, {ID: "awake"}, {ID: "usage"}, {ID: "clip"}, {ID: "recent"},
			},
			drops: "recent",
		},
	}
	for name, tc := range orders {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, Layout{Version: 1, Cards: tc.cards})
			l, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(homeIDs(l), tc.drops) {
				t.Fatalf("%s was placed: %v", tc.drops, homeIDs(l))
			}
			if len(l.Home) != 4 {
				t.Fatalf("home = %+v, want four widgets", l.Home)
			}
			if slices.Contains(cardIDs(l), tc.drops) {
				t.Fatalf("%s fell through to the tool page", tc.drops)
			}
			if !slices.Contains(homeIDs(l), "playing") {
				t.Fatalf("playing missing: %v", homeIDs(l))
			}
			assertNoOverlap(t, l)
		})
	}
}

func TestMigrateV1KeepsUnknownAndActionCards(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, Layout{Version: 1, Cards: []Card{
		{ID: "mystery", Span: 2},
		{ID: "playing", Span: 2},
		{ID: "ocr", Span: 1},
		{ID: "second-unknown", Span: 5},
	}})
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cardIDs(l), []string{"mystery", "ocr", "second-unknown"}; !slices.Equal(got, want) {
		t.Fatalf("cards = %v, want %v", got, want)
	}
	if l.Cards[0].Span != 2 || l.Cards[1].Span != 1 || l.Cards[2].Span != 1 {
		t.Fatalf("spans = %+v, want 2, 1, 1", l.Cards)
	}
	if !slices.Equal(homeIDs(l), []string{"playing"}) {
		t.Fatalf("home = %v", homeIDs(l))
	}
}

func TestNormalizeHomeRepairs(t *testing.T) {
	l := Layout{
		Version: Version,
		Home: []Widget{
			{ID: "a", X: 0, Y: 0, W: 3, H: 0},  // width clamps to 2, height to 1
			{ID: "b", X: 1, Y: 0, W: 1, H: 1},  // overlaps a: dropped
			{ID: "", X: 2, Y: 0, W: 1, H: 1},   // empty id: dropped
			{ID: "c", X: 3, Y: 1, W: 2, H: 1},  // x+w = 5: outside the grid
			{ID: "d", X: -1, Y: 1, W: 1, H: 1}, // negative x: outside the grid
			{ID: "e", X: 0, Y: 1, W: 1, H: 0},  // height clamps to 1
			{ID: "a", X: 2, Y: 1, W: 2, H: 1},  // duplicate id: dropped
		},
	}
	l.normalize()
	want := []Widget{
		{ID: "a", X: 0, Y: 0, W: 2, H: 1},
		{ID: "e", X: 0, Y: 1, W: 1, H: 1},
	}
	if !slices.Equal(l.Home, want) {
		t.Fatalf("home = %+v, want %+v", l.Home, want)
	}
}

func TestNormalizeOnePlaceAcrossHomeCardsPins(t *testing.T) {
	l := Layout{
		Version: Version,
		Home:    []Widget{{ID: "awake", X: 0, Y: 0, W: 1, H: 1}},
		Cards:   []Card{{ID: "awake"}, {ID: "zip", Span: 0}, {ID: "zip", Span: 2}},
		Pins:    []Pin{{ID: "zip"}, {ID: "clip", Edge: "diagonal"}, {ID: "awake"}},
	}
	l.normalize()
	if !slices.Equal(homeIDs(l), []string{"awake"}) {
		t.Fatalf("home = %v: home occurrence should win", homeIDs(l))
	}
	if got, want := cardIDs(l), []string{"zip"}; !slices.Equal(got, want) {
		t.Fatalf("cards = %v, want %v", got, want)
	}
	if l.Cards[0].Span != 1 {
		t.Fatalf("zip span = %d, want clamped 1", l.Cards[0].Span)
	}
	if !slices.Equal(pinIDs(l), []string{"clip"}) {
		t.Fatalf("pins = %v, want clip only", pinIDs(l))
	}
	if l.Pins[0].Edge != "" {
		t.Fatalf("unknown edge kept: %q", l.Pins[0].Edge)
	}
}

func TestNormalizeDedupesCardsAndPins(t *testing.T) {
	l := Layout{
		Version: Version,
		Cards:   []Card{{ID: "zip"}, {ID: "zip", Span: 2}, {ID: "fit1m"}},
		Pins:    []Pin{{ID: "fit1m"}, {ID: "topdf", Edge: "diagonal"}},
	}
	l.normalize()
	if !slices.Equal(cardIDs(l), []string{"zip", "fit1m"}) {
		t.Fatalf("cards = %v", cardIDs(l))
	}
	if l.Cards[0].Span != 1 {
		t.Fatal("first duplicate should win")
	}
	if !slices.Equal(pinIDs(l), []string{"topdf"}) {
		t.Fatalf("pins = %v: fit1m's pin should have dropped (grid wins)", pinIDs(l))
	}
	if l.Pins[0].Edge != "" {
		t.Fatalf("unknown edge kept: %q", l.Pins[0].Edge)
	}
}

func TestNormalizeClampsSpans(t *testing.T) {
	l := Layout{Version: Version, Cards: []Card{{ID: "awake", Span: 0}, {ID: "zip", Span: 3}}}
	l.normalize()
	for _, c := range l.Cards {
		if c.Span != 1 {
			t.Fatalf("span %d was not clamped", c.Span)
		}
	}
}

func TestHomeLookups(t *testing.T) {
	l := Default()
	if got := l.HomeIndex("playing"); got != 0 {
		t.Fatalf("HomeIndex(playing) = %d, want 0", got)
	}
	if got := l.HomeIndex("clip"); got != -1 {
		t.Fatalf("HomeIndex(clip) = %d, want -1", got)
	}
	if w, ok := l.HomeAt(0, 0); !ok || w.ID != "playing" {
		t.Fatalf("HomeAt(0,0) = %+v, %v", w, ok)
	}
	if w, ok := l.HomeAt(2, 0); !ok || w.ID != "awake" {
		t.Fatalf("HomeAt(2,0) = %+v, %v", w, ok)
	}
	// (1,1) lies inside playing but is not its top-left cell.
	if w, ok := l.HomeAt(1, 1); ok {
		t.Fatalf("HomeAt(1,1) = %+v, want no top-left match", w)
	}
	if _, ok := l.HomeAt(9, 9); ok {
		t.Fatal("HomeAt(9,9) found a widget")
	}
	if l.OverlapsHome("playing", 0, 0, 2, 2) {
		t.Fatal("OverlapsHome did not ignore the widget itself")
	}
	if !l.OverlapsHome("", 0, 0, 2, 2) {
		t.Fatal("OverlapsHome missed playing")
	}
	if !l.OverlapsHome("awake", 3, 1, 1, 1) {
		t.Fatal("OverlapsHome missed recent at (3,1)")
	}
	if l.OverlapsHome("awake", 2, 0, 1, 1) {
		t.Fatal("OverlapsHome false positive on awake's own cell")
	}
}

func TestAddWidgetRefusals(t *testing.T) {
	l := Default()
	l.AddPin(Pin{ID: "clip"})
	homeBefore := slices.Clone(l.Home)
	cardsBefore := slices.Clone(l.Cards)
	pinsBefore := slices.Clone(l.Pins)

	bad := []Widget{
		{ID: "playing", X: 2, Y: 1, W: 2, H: 1}, // on the home canvas already
		{ID: "ocr", X: 2, Y: 1, W: 2, H: 1},     // on the tool page
		{ID: "clip", X: 2, Y: 1, W: 2, H: 1},    // pinned
		{ID: "", X: 2, Y: 1, W: 1, H: 1},        // empty id
		{ID: "new", X: 3, Y: 0, W: 2, H: 1},     // leaves the grid
		{ID: "new", X: 0, Y: 1, W: 1, H: 2},     // leaves the grid
		{ID: "new", X: 0, Y: 0, W: 3, H: 1},     // W outside 1..2
		{ID: "new", X: 0, Y: 0, W: 1, H: 3},     // H outside 1..2
		{ID: "new", X: 2, Y: 0, W: 2, H: 1},     // overlaps awake and usage
	}
	for _, w := range bad {
		if l.AddWidget(w) {
			t.Fatalf("AddWidget(%+v) succeeded, want refusal", w)
		}
	}
	if !slices.Equal(l.Home, homeBefore) || !slices.Equal(l.Cards, cardsBefore) || !slices.Equal(l.Pins, pinsBefore) {
		t.Fatalf("failed AddWidget mutated the layout: %+v", l)
	}
	l.RemoveWidget("recent")
	if !l.AddWidget(Widget{ID: "new", X: 3, Y: 1, W: 1, H: 1}) {
		t.Fatal("AddWidget refused the sole free cell")
	}
	if l.HomeIndex("new") != len(l.Home)-1 {
		t.Fatalf("new widget not appended: %+v", l.Home)
	}
}

func TestAddWidgetBest(t *testing.T) {
	// Default rectangle, when free.
	l := Default()
	l.RemoveWidget("recent")
	if !l.AddWidgetBest("clip", 2, 1) {
		t.Fatal("AddWidgetBest refused clip")
	}
	if w, ok := l.HomeAt(2, 1); !ok || w.ID != "clip" {
		t.Fatalf("clip placed at %+v, %v; want (2,1)", w, ok)
	}

	// Fallback: the default rectangle is blocked, so the first free cell
	// in row-major order wins.
	l = Layout{Version: Version, Home: []Widget{
		{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		{ID: "block", X: 2, Y: 1, W: 2, H: 1},
	}}
	if !l.AddWidgetBest("clip", 2, 1) {
		t.Fatal("AddWidgetBest did not fall back")
	}
	if w, ok := l.HomeAt(0, 0); !ok || w.ID != "clip" {
		t.Fatalf("clip placed at %+v, %v; want fallback (0,0)", w, ok)
	}

	// Already placed anywhere: refused.
	if l.AddWidgetBest("clip", 1, 1) {
		t.Fatal("AddWidgetBest placed a duplicate")
	}
	l.AddPin(Pin{ID: "ocr"})
	if l.AddWidgetBest("ocr", 1, 1) {
		t.Fatal("AddWidgetBest placed a pinned card")
	}

	// No free cell: refused.
	full := Layout{Version: Version}
	for y := range GridRows {
		for x := range GridCols {
			if !full.AddWidget(Widget{ID: string(rune('a' + y*GridCols + x)), X: x, Y: y, W: 1, H: 1}) {
				t.Fatalf("could not fill cell %d,%d", x, y)
			}
		}
	}
	if full.AddWidgetBest("z", 1, 1) {
		t.Fatal("AddWidgetBest found a spot on a full canvas")
	}
	if full.AddWidgetBest("z", 2, 2) {
		t.Fatal("AddWidgetBest found a 2x2 spot on a full canvas")
	}
}

func TestMoveWidget(t *testing.T) {
	l := Default()
	if l.MoveWidget("missing", 0, 0) {
		t.Fatal("MoveWidget moved an absent widget")
	}
	before := slices.Clone(l.Home)
	// Playing covers awake's target and recent covers usage's.
	if l.MoveWidget("awake", 0, 1) {
		t.Fatal("MoveWidget allowed an overlap")
	}
	if l.MoveWidget("awake", 2, 1) {
		t.Fatal("MoveWidget allowed an overlap with recent")
	}
	if l.MoveWidget("awake", 2, 2) {
		t.Fatal("MoveWidget allowed a move outside the grid")
	}
	if !slices.Equal(l.Home, before) {
		t.Fatalf("failed moves mutated the layout: %+v", l.Home)
	}
	l.RemoveWidget("recent")
	if !l.MoveWidget("awake", 2, 1) {
		t.Fatal("MoveWidget refused a free cell")
	}
	if w, ok := l.HomeAt(2, 1); !ok || w.ID != "awake" {
		t.Fatalf("awake not at (2,1): %+v", l.Home)
	}
	if _, ok := l.HomeAt(2, 0); ok {
		t.Fatal("awake's old cell still reports a widget")
	}
}

func TestSetWidgetSize(t *testing.T) {
	l := Default()
	if l.SetWidgetSize("missing", 1, 1) {
		t.Fatal("SetWidgetSize resized an absent widget")
	}
	if l.SetWidgetSize("usage", 2, 1) {
		t.Fatal("SetWidgetSize allowed a size outside the grid")
	}
	if l.SetWidgetSize("awake", 2, 1) {
		t.Fatal("SetWidgetSize allowed an overlap with usage")
	}
	if l.SetWidgetSize("awake", 1, 2) {
		t.Fatal("SetWidgetSize allowed an overlap with recent")
	}
	if l.SetWidgetSize("awake", 3, 1) || l.SetWidgetSize("awake", 0, 1) {
		t.Fatal("SetWidgetSize accepted a size outside 1..2")
	}
	if w := l.Home[l.HomeIndex("awake")]; w != (Widget{ID: "awake", X: 2, Y: 0, W: 1, H: 1}) {
		t.Fatalf("failed resizes mutated awake: %+v", w)
	}
	l.RemoveWidget("recent")
	if !l.SetWidgetSize("awake", 1, 2) {
		t.Fatal("SetWidgetSize refused a fitting size")
	}
	if w := l.Home[l.HomeIndex("awake")]; w != (Widget{ID: "awake", X: 2, Y: 0, W: 1, H: 2}) {
		t.Fatalf("awake = %+v, want 1x2 anchored at (2,0)", w)
	}
}

func TestRemoveWidget(t *testing.T) {
	l := Default()
	if !l.RemoveWidget("awake") {
		t.Fatal("RemoveWidget failed")
	}
	if l.HomeIndex("awake") != -1 {
		t.Fatal("awake still on the home canvas")
	}
	if l.RemoveWidget("awake") {
		t.Fatal("RemoveWidget reported a second removal")
	}
	if l.RemoveWidget("ocr") {
		t.Fatal("RemoveWidget removed a tool page card")
	}
	if len(l.Cards) != len(Default().Cards) {
		t.Fatalf("cards changed: %v", cardIDs(l))
	}
}

func TestAddCardKeepsOnePlace(t *testing.T) {
	l := Default()
	before := len(l.Cards)
	l.AddCard("ocr", 2) // already placed: no-op
	if got := cardIDs(l); len(got) != before {
		t.Fatalf("duplicated card added: %v", got)
	}
	l.AddCard("mystery", 2)
	if got := cardIDs(l); got[len(got)-1] != "mystery" || l.Cards[len(l.Cards)-1].Span != 2 {
		t.Fatalf("mystery not appended with span 2: %+v", l.Cards)
	}
	l.AddPin(Pin{ID: "widgetx"})
	l.AddCard("widgetx", 2) // pinned already: still no-op
	l.AddCard("playing", 1) // on the home canvas: no-op
	if slices.Contains(cardIDs(l), "widgetx") || slices.Contains(cardIDs(l), "playing") {
		t.Fatalf("placed card joined the grid: %v", cardIDs(l))
	}
}

func TestMoveCard(t *testing.T) {
	l := Default()
	l.MoveCard("zip", 0)
	if got := cardIDs(l); got[0] != "zip" {
		t.Fatalf("zip did not move first: %v", got)
	}
	before := cardIDs(l)
	l.MoveCard("zip", 0) // moving before itself is a no-op
	if !slices.Equal(cardIDs(l), before) {
		t.Fatalf("self-move changed order: %v", cardIDs(l))
	}
	l.MoveCard("zip", len(l.Cards))
	if got := cardIDs(l); got[len(got)-1] != "zip" {
		t.Fatalf("zip did not move last: %v", got)
	}
	l.MoveCard("missing", 0) // absent: no-op, no panic
}

func TestSetSpan(t *testing.T) {
	l := Default()
	l.SetSpan("ocr", 2)
	if l.Cards[l.index("ocr")].Span != 2 {
		t.Fatal("span not set")
	}
	l.SetSpan("ocr", 9) // out of range: ignored
	if l.Cards[l.index("ocr")].Span != 2 {
		t.Fatal("out-of-range span applied")
	}
	l.SetSpan("missing", 2) // absent: no panic
}

func TestAddPinRemovesFromGridAndHome(t *testing.T) {
	l := Default()
	l.AddPin(Pin{ID: "zip", Span: 1, Edge: EdgeLeft})
	if slices.Contains(cardIDs(l), "zip") {
		t.Fatal("pinned card stayed on the grid")
	}
	if !slices.Equal(pinIDs(l), []string{"zip"}) {
		t.Fatalf("pins = %v", pinIDs(l))
	}
	l.AddPin(Pin{ID: "zip", Edge: EdgeRight}) // already pinned: keeps the first
	if len(l.Pins) != 1 || l.Pins[0].Edge != EdgeLeft {
		t.Fatalf("second pin mutated the first: %+v", l.Pins)
	}

	l.AddPin(Pin{ID: "awake", Span: 2, Edge: EdgeRight})
	if l.HomeIndex("awake") != -1 {
		t.Fatalf("pinned widget stayed on the home canvas: %+v", l.Home)
	}
	if !slices.Contains(pinIDs(l), "awake") {
		t.Fatalf("awake not pinned: %v", pinIDs(l))
	}
	if slices.Contains(cardIDs(l), "awake") {
		t.Fatal("pinned home widget joined the tool page")
	}
}

func TestRemovePinPutsBack(t *testing.T) {
	l := Default()
	l.AddPin(Pin{ID: "awake", Span: 2, Edge: EdgeRight})
	l.RemovePin("awake", true, 1)
	if len(l.Pins) != 0 {
		t.Fatal("pin was not removed")
	}
	if got := cardIDs(l); got[1] != "awake" {
		t.Fatalf("awake did not return at index 1: %v", got)
	}
	if l.Cards[1].Span != 2 {
		t.Fatal("returned card lost its span")
	}
	// Out-of-range position appends.
	l.AddPin(Pin{ID: "awake", Span: 1})
	l.RemovePin("awake", true, 99)
	if got := cardIDs(l); got[len(got)-1] != "awake" {
		t.Fatalf("awake did not append: %v", got)
	}
	// Without putBack the card is just gone.
	l.AddPin(Pin{ID: "zip"})
	l.RemovePin("zip", false, 0)
	if slices.Contains(cardIDs(l), "zip") || len(l.Pins) != 0 {
		t.Fatal("removed card came back")
	}
}

func TestLoadNormalizesWithoutFailing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, Layout{Version: Version,
		Cards: []Card{{ID: "zip"}, {ID: "zip"}, {ID: "awake", Span: 7}}})
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cardIDs(l), []string{"zip", "awake"}) || l.Cards[1].Span != 1 {
		t.Fatalf("normalize did not repair: %+v", l.Cards)
	}
}

func TestHomePersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l := Default()
	l.RemoveWidget("recent")
	if !l.AddWidgetBest("clip", 2, 1) {
		t.Fatal("could not place clip")
	}
	if !l.RemoveWidget("usage") {
		t.Fatal("could not remove usage")
	}
	if !l.MoveWidget("awake", 3, 0) {
		t.Fatal("could not move awake")
	}
	if !l.AddWidgetBest("usage", 1, 1) {
		t.Fatal("could not place usage")
	}
	if !l.SetWidgetSize("playing", 2, 2) {
		t.Fatal("could not keep playing at 2x2")
	}
	if err := Write(dir, l); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Home, l.Home) {
		t.Fatalf("home = %+v, want %+v", got.Home, l.Home)
	}
	if got.Version != 2 {
		t.Fatalf("version = %d", got.Version)
	}
	assertNoOverlap(t, got)
}

func TestLoadQuietWith(t *testing.T) {
	prev := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(prev) })

	missing := t.TempDir()
	if got := LoadQuietWith(missing, false); !slices.Equal(got.Home, DefaultWith(false).Home) {
		t.Fatalf("missing file home = %+v", got.Home)
	}
	if _, err := os.Stat(filepath.Join(missing, FileName)); err != nil {
		t.Fatalf("LoadQuietWith did not create the file: %v", err)
	}

	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, FileName), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadQuietWith(broken, true); !slices.Equal(got.Home, Default().Home) {
		t.Fatalf("broken file home = %+v", got.Home)
	}
	if got := LoadQuiet(broken); !slices.Equal(got.Home, Default().Home) {
		t.Fatalf("LoadQuiet home = %+v", got.Home)
	}
}

func TestWriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	l := Default()
	if err := Write(dir, l); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "."+FileName+".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

// TestInactiveWidgetLeavesCellsFree is review #6 at the layout level: a
// widget Inactive names takes no space, so moves, additions and resizes
// treat its cells as free, while the entry stays in the layout.
func TestInactiveWidgetLeavesCellsFree(t *testing.T) {
	l := Layout{Version: Version,
		Home: []Widget{
			{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
			{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
			{ID: "usage", X: 3, Y: 0, W: 1, H: 1},
			{ID: "recent", X: 2, Y: 1, W: 2, H: 1},
		},
		Inactive: func(id string) bool { return id == "usage" },
	}
	if l.OverlapsHome("awake", 3, 0, 1, 1) {
		t.Fatal("a switched-off widget still blocks its cell")
	}
	if !l.MoveWidget("awake", 3, 0) {
		t.Fatal("MoveWidget refused a cell under a switched-off widget")
	}
	if !l.AddWidget(Widget{ID: "clip", X: 2, Y: 0, W: 1, H: 1}) {
		t.Fatal("AddWidget refused a cell under a switched-off widget")
	}
	if got := l.HomeIndex("usage"); got < 0 {
		t.Fatal("the switched-off widget left the layout")
	}

	// Active again: the same geometry is refused.
	l.Inactive = nil
	if !l.OverlapsHome("awake", 3, 0, 1, 1) {
		t.Fatal("an active widget does not occupy its cell")
	}
	if l.MoveWidget("recent", 3, 0) {
		t.Fatal("an active widget's cell took a move")
	}
}

// TestPinHomeSlotRoundTrip is review #3's file half: the pin records the
// canvas rect a widget came from, an old file without it still loads, and
// an off-canvas rect is dropped rather than believed.
func TestPinHomeSlotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l := Default()
	slot := Widget{ID: "recent", X: 2, Y: 1, W: 2, H: 1}
	var pin Pin
	pin.ID = "recent"
	pin.Span = 2
	pin.SetHome(slot)
	l.AddPin(pin)
	if err := Write(dir, l); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pins) != 1 || got.Pins[0].HomeSlot() != slot {
		t.Fatalf("pin home slot = %+v, want %+v", got.Pins[0].HomeSlot(), slot)
	}
	// An action card has none.
	if (Pin{ID: "zip"}).HomeSlot() != (Widget{}) {
		t.Fatal("a pin without a recorded rect reports one")
	}
	// Off the canvas: dropped by normalize, so 放回刘海 falls back.
	off := Widget{ID: "recent", X: 3, Y: 0, W: 2, H: 1}
	pin.SetHome(off)
	if pin.HomeSlot() != (Widget{}) {
		t.Fatalf("an off-canvas rect was kept: %+v", pin.HomeSlot())
	}
}

// TestPinHomeSlotOmittedInOldFiles pins the version compatibility review
// #3 asks for: a layout.json written before the fields existed loads
// normally and is not rewritten.
func TestPinHomeSlotOmittedInOldFiles(t *testing.T) {
	dir := t.TempDir()
	old := `{
  "version": 2,
  "home": [{"id": "playing", "x": 0, "y": 0, "w": 2, "h": 2}],
  "pins": [{"id": "recent", "span": 2, "display": "1", "x": 10, "y": 20}]
}`
	file := filepath.Join(dir, FileName)
	if err := os.WriteFile(file, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Load(dir)
	if err != nil {
		t.Fatalf("an old file failed to load: %v", err)
	}
	if len(l.Pins) != 1 || l.Pins[0].HomeSlot() != (Widget{}) {
		t.Fatalf("pins = %+v", l.Pins)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != old {
		t.Fatalf("loading an old file rewrote it:\n%s", data)
	}
}

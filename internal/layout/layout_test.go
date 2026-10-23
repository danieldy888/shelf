package layout

import (
	"encoding/json"
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

func TestLoadCreatesDefault(t *testing.T) {
	dir := t.TempDir()
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cardIDs(l), cardIDs(Default())) {
		t.Fatalf("cards = %v, want default %v", cardIDs(l), cardIDs(Default()))
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("layout.json was not written: %v", err)
	}
}

func TestWriteThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	l := Default()
	l.MoveCard("zip", 0)
	l.SetSpan("awake", 2)
	l.Pins = append(l.Pins, Pin{ID: "clip", Span: 2, Edge: EdgeRight, X: 4, Y: 120, AlwaysOpen: true})
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
	if got.Cards[slices.Index(cardIDs(got), "awake")].Span != 2 {
		t.Fatal("awake span did not persist")
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
	if !slices.Equal(cardIDs(l), cardIDs(Default())) {
		t.Fatalf("cards = %v, want defaults", cardIDs(l))
	}
	data, _ := os.ReadFile(p)
	if string(data) != "{nope" {
		t.Fatal("corrupt file was overwritten")
	}
}

func TestLoadUnknownVersionDefaults(t *testing.T) {
	dir := t.TempDir()
	data, _ := json.Marshal(map[string]any{"version": 99, "cards": []Card{{ID: "zip"}}})
	if err := os.WriteFile(filepath.Join(dir, FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Load(dir)
	if err == nil {
		t.Fatal("expected a version error")
	}
	if !slices.Equal(cardIDs(l), cardIDs(Default())) {
		t.Fatalf("cards = %v, want defaults", cardIDs(l))
	}
}

func TestNormalizeDedupesCardsAndPins(t *testing.T) {
	l := Layout{
		Version: Version,
		Cards:   []Card{{ID: "zip"}, {ID: "zip", Span: 2}, {ID: "fit1m"}},
		Pins:    []Pin{{ID: "fit1m"}, {ID: "clip", Edge: "diagonal"}},
	}
	l.normalize()
	if !slices.Equal(cardIDs(l), []string{"zip", "fit1m"}) {
		t.Fatalf("cards = %v", cardIDs(l))
	}
	if l.Cards[0].Span != 1 {
		t.Fatal("first duplicate should win")
	}
	if !slices.Equal(pinIDs(l), []string{"clip"}) {
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

func TestAddCardKeepsOnePlace(t *testing.T) {
	l := Default()
	l.AddCard("fit1m", 2) // already placed: no-op
	if got := cardIDs(l); len(got) != len(cardIDs(Default())) {
		t.Fatalf("duplicated card added: %v", got)
	}
	l.AddCard("clip", 2)
	if got := cardIDs(l); got[len(got)-1] != "clip" || l.Cards[len(l.Cards)-1].Span != 2 {
		t.Fatalf("clip not appended with span 2: %+v", l.Cards)
	}
	l.Pins = append(l.Pins, Pin{ID: "usage"})
	l.AddCard("usage", 2) // pinned already: still no-op
	if slices.Contains(cardIDs(l), "usage") {
		t.Fatal("pinned card joined the grid")
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
	l.SetSpan("awake", 2)
	if l.Cards[l.index("awake")].Span != 2 {
		t.Fatal("span not set")
	}
	l.SetSpan("awake", 9) // out of range: ignored
	if l.Cards[l.index("awake")].Span != 2 {
		t.Fatal("out-of-range span applied")
	}
	l.SetSpan("missing", 2) // absent: no panic
}

func TestAddPinRemovesFromGrid(t *testing.T) {
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
	data, _ := json.Marshal(Layout{Version: Version,
		Cards: []Card{{ID: "zip"}, {ID: "zip"}, {ID: "awake", Span: 7}}})
	if err := os.WriteFile(filepath.Join(dir, FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cardIDs(l), []string{"zip", "awake"}) || l.Cards[1].Span != 1 {
		t.Fatalf("normalize did not repair: %+v", l.Cards)
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

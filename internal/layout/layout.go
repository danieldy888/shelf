// Package layout persists the tool page's card arrangement and the
// desktop pins in layout.json under the data directory. The file is
// versioned, written atomically (temporary file + rename, like
// settings.json), and a corrupt or unknown-version file falls back to the
// default layout without being overwritten, so a damaged file never eats
// the user's arrangement. A card id appears at most once in the whole
// layout — on the tool page or pinned to the desktop, never both
// (design draft §6「固定项数量」).
package layout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
)

// FileName is the file's name inside the data directory.
const FileName = "layout.json"

// Version is the file schema version Load accepts.
const Version = 1

// Edge is the screen edge a pinned card sticks to; EdgeNone floats free
// and is drawn always expanded.
type Edge string

const (
	EdgeNone  Edge = "none"
	EdgeLeft  Edge = "left"
	EdgeRight Edge = "right"
)

// Card is one cell of the tool page grid: a catalog card id and its
// column span (1 or 2).
type Card struct {
	ID   string `json:"id"`
	Span int    `json:"span,omitempty"` // omitted means 1
}

// Pin is one card floating on the desktop (task B): the card, the
// column span it keeps on return to the grid, the display it lives
// on, the screen edge it sticks to (empty edge floats free), its
// position in the display's visible frame, and whether it stays
// expanded instead of folding into the edge strip.
type Pin struct {
	ID         string `json:"id"`
	Span       int    `json:"span,omitempty"`       // column span it keeps when put back on the tool page
	Display    string `json:"display,omitempty"`    // display identifier the pin was last seen on; empty = main
	Edge       Edge   `json:"edge,omitempty"`       // EdgeLeft / EdgeRight collapse at that edge
	X          int    `json:"x,omitempty"`          // left offset inside the display's visible frame
	Y          int    `json:"y,omitempty"`          // top offset inside the display's visible frame
	AlwaysOpen bool   `json:"alwaysOpen,omitempty"` // never folds into the edge strip
}

// Layout is the persisted arrangement: the tool page's card list and the
// pinned desktop cards.
type Layout struct {
	Version int    `json:"version"`
	Cards   []Card `json:"cards,omitempty"`
	Pins    []Pin  `json:"pins,omitempty"`
}

// Default returns the arrangement a missing or broken file starts from:
// the design's default grid. The 框选文字 card joins the front of row one
// when its entry lands (PLAN.md 任务 A 第 3 条: the card stays out of the
// catalog until task C merges).
func Default() Layout {
	return Layout{
		Version: Version,
		Cards: []Card{
			{ID: "fit1m", Span: 1},
			{ID: "topdf", Span: 1},
			{ID: "zip", Span: 1},
			{ID: "playing", Span: 2},
			{ID: "awake", Span: 1},
			{ID: "airdrop", Span: 1},
		},
	}
}

// Load reads layout.json under dir, creating it with the default layout
// when missing. A corrupt file (bad JSON, an unknown version, or shape
// violations that validation cannot repair) yields the default layout and
// an error; the file is left alone for the user to inspect.
func Load(dir string) (Layout, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	var l Layout
	switch {
	case errors.Is(err, fs.ErrNotExist):
		l = Default()
		if werr := Write(dir, l); werr != nil {
			return l, fmt.Errorf("layout: creating %s: %w", FileName, werr)
		}
		return l, nil
	case err != nil:
		return Default(), fmt.Errorf("layout: reading %s: %w", FileName, err)
	}
	if err := json.Unmarshal(data, &l); err != nil {
		return Default(), fmt.Errorf("layout: %s is not valid JSON, using defaults: %w", FileName, err)
	}
	if l.Version != Version {
		return Default(), fmt.Errorf("layout: %s version %d unknown, using defaults", FileName, l.Version)
	}
	l.normalize()
	return l, nil
}

// Write saves l atomically: a temporary file in dir renamed over the
// target, so a crash never leaves a half-written layout.json.
func Write(dir string, l Layout) error {
	data, err := json.MarshalIndent(l, "", "  ")
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

// normalize repairs the recoverable parts of a loaded file: spans clamp
// to 1 or 2, empty card ids drop, and a card id that shows up more than
// once — in the grid, among the pins, or both — keeps only its first
// place, in grid-then-pin order (design §6: one card, one place).
// Pins for unknown edges reset to the main display floating free.
func (l *Layout) normalize() {
	seen := map[string]bool{}
	cards := l.Cards[:0]
	for _, card := range l.Cards {
		if card.ID == "" || seen[card.ID] {
			continue
		}
		if card.Span < 1 || card.Span > 2 {
			card.Span = 1
		}
		seen[card.ID] = true
		cards = append(cards, card)
	}
	l.Cards = cards
	pins := l.Pins[:0]
	for _, pin := range l.Pins {
		if pin.ID == "" || seen[pin.ID] {
			continue
		}
		switch pin.Edge {
		case "", EdgeLeft, EdgeRight:
		default:
			pin.Edge = ""
		}
		seen[pin.ID] = true
		pins = append(pins, pin)
	}
	l.Pins = pins
	if l.Version == 0 {
		l.Version = Version
	}
}

// index is the grid position of card id, or -1.
func (l *Layout) index(id string) int {
	return slices.IndexFunc(l.Cards, func(c Card) bool { return c.ID == id })
}

// hasCard reports whether id is on the tool page or pinned.
func (l *Layout) hasCard(id string) bool {
	if l.index(id) >= 0 {
		return true
	}
	return slices.ContainsFunc(l.Pins, func(p Pin) bool { return p.ID == id })
}

// AddCard appends id to the end of the tool page grid with span clamped
// to 1 or 2; it is a no-op when the card is already placed anywhere
// (grid or desktop), the one-card-one-place rule.
func (l *Layout) AddCard(id string, span int) {
	if id == "" || l.hasCard(id) {
		return
	}
	l.Cards = append(l.Cards, Card{ID: id, Span: clampSpan(span)})
}

// RemoveCard takes id off the tool page grid; a desktop pin is not
// touched — use RemovePin for it.
func (l *Layout) RemoveCard(id string) {
	if i := l.index(id); i >= 0 {
		l.Cards = slices.Delete(l.Cards, i, i+1)
	}
}

// SetSpan switches id's column span between 1 and 2; absent cards and
// out-of-range spans do nothing.
func (l *Layout) SetSpan(id string, span int) {
	i := l.index(id)
	if i < 0 || span < 1 || span > 2 {
		return
	}
	l.Cards[i].Span = span
}

// MoveCard reorders id to before position to: the index it inserts at
// counting the list without it (n cards means to == n appends). A
// negative to clamps to 0, an over-the-end one to len.
func (l *Layout) MoveCard(id string, to int) {
	i := l.index(id)
	if i < 0 {
		return
	}
	card := l.Cards[i]
	l.Cards = slices.Delete(l.Cards, i, i+1)
	to = max(0, min(to, len(l.Cards)))
	l.Cards = slices.Insert(l.Cards, to, card)
}

// AddPin places id on the desktop as pin; it is removed from the tool
// page grid first (the card lives in exactly one place). A card already
// pinned keeps its placement.
func (l *Layout) AddPin(pin Pin) {
	if pin.ID == "" {
		return
	}
	if slices.ContainsFunc(l.Pins, func(p Pin) bool { return p.ID == pin.ID }) {
		return
	}
	l.RemoveCard(pin.ID)
	switch pin.Edge {
	case "", EdgeLeft, EdgeRight:
	default:
		pin.Edge = ""
	}
	l.Pins = append(l.Pins, pin)
}

// RemovePin takes id's pin off the desktop; when putBack is true the
// card goes back to the tool page grid — at before, or appended when
// before is out of range (design §4: 「放回刘海」回到原位置，被占就放末尾).
func (l *Layout) RemovePin(id string, putBack bool, before int) {
	i := slices.IndexFunc(l.Pins, func(p Pin) bool { return p.ID == id })
	if i < 0 {
		return
	}
	pin := l.Pins[i]
	l.Pins = slices.Delete(l.Pins, i, i+1)
	if !putBack || l.hasCard(pin.ID) {
		return
	}
	card := Card{ID: pin.ID, Span: clampSpan(pin.Span)}
	before = max(0, min(before, len(l.Cards)))
	l.Cards = slices.Insert(l.Cards, before, card)
}

func clampSpan(span int) int {
	if span < 1 || span > 2 {
		return 1
	}
	return span
}

// LoadQuiet is Load with the error logged instead of returned, for the
// app shell's startup path: a broken file falls back to defaults and the
// user still gets a working page.
func LoadQuiet(dir string) Layout {
	l, err := Load(dir)
	if err != nil {
		log.Print(err)
	}
	return l
}

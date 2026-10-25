// Package layout persists the tool page's card arrangement, the home
// canvas' placed widgets, and the desktop pins in layout.json under the
// data directory. The file is versioned, written atomically (temporary
// file + rename, like settings.json), and a corrupt or unknown-version
// file falls back to the default layout without being overwritten, so a
// damaged file never eats the user's arrangement. A card id appears at
// most once in the whole layout — on the home canvas, the tool page, or
// pinned to the desktop, never twice (design draft §6「固定项数量」).
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

// Version is the file schema version Load writes and accepts; files
// stamped with an older version migrate in memory.
const Version = 2

const (
	// GridCols is the home canvas' width in cells.
	GridCols = 4
	// GridRows is the home canvas' height in cells.
	GridRows = 2
)

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

// Widget is one widget placed on the home canvas: the widget id and the
// rectangle it covers, in cells. X and Y address the top-left cell; W
// and H are the cell counts, each 1 or 2.
type Widget struct {
	ID string `json:"id"`
	X  int    `json:"x"`
	Y  int    `json:"y"`
	W  int    `json:"w"`
	H  int    `json:"h"`
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

// Layout is the persisted arrangement: the tool page's card list, the
// widgets placed on the home canvas, and the pinned desktop cards.
type Layout struct {
	Version int      `json:"version"`
	Cards   []Card   `json:"cards,omitempty"`
	Home    []Widget `json:"home,omitempty"`
	Pins    []Pin    `json:"pins,omitempty"`
}

// widgetCatalog lists the home widgets and the rectangle each starts at.
// Migration reads it to place a version 1 widget card, AddWidgetBest to
// try the natural spot first.
var widgetCatalog = []Widget{
	{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
	{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
	{ID: "usage", X: 3, Y: 0, W: 1, H: 1},
	{ID: "recent", X: 2, Y: 1, W: 2, H: 1},
	{ID: "clip", X: 2, Y: 1, W: 2, H: 1},
}

// defaultWidget returns id's catalogue rectangle.
func defaultWidget(id string) (Widget, bool) {
	for _, w := range widgetCatalog {
		if w.ID == id {
			return w, true
		}
	}
	return Widget{}, false
}

// defaultCards is the tool page's action cards, in the design's order.
func defaultCards() []Card {
	return []Card{
		{ID: "ocr", Span: 1},
		{ID: "fit1m", Span: 1},
		{ID: "topdf", Span: 1},
		{ID: "zip", Span: 1},
		{ID: "tojpeg", Span: 1},
		{ID: "toword", Span: 1},
		{ID: "extract", Span: 1},
		{ID: "resize", Span: 1},
	}
}

// Default returns the arrangement a missing or broken file starts from:
// the actions on the tool page and the play widget beside the awake,
// usage and recent widgets on the home canvas.
func Default() Layout {
	return DefaultWith(true)
}

// DefaultWith is Default for a build whose AI usage widget is available
// or not: without it the clipboard widget takes the second home row
// instead of the usage and recent widgets.
func DefaultWith(aiUsage bool) Layout {
	l := Layout{
		Version: Version,
		Cards:   defaultCards(),
		Home: []Widget{
			{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
			{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		},
	}
	if aiUsage {
		l.Home = append(l.Home,
			Widget{ID: "usage", X: 3, Y: 0, W: 1, H: 1},
			Widget{ID: "recent", X: 2, Y: 1, W: 2, H: 1},
		)
		return l
	}
	l.Home = append(l.Home, Widget{ID: "clip", X: 2, Y: 1, W: 2, H: 1})
	return l
}

// Load reads layout.json under dir, creating it with the default layout
// when missing. A corrupt file (bad JSON, an unknown version, or shape
// violations that validation cannot repair) yields the default layout and
// an error; the file is left alone for the user to inspect. A version 0
// or 1 file migrates in memory and is not written back.
func Load(dir string) (Layout, error) {
	return load(dir, Default)
}

// LoadWith is Load with DefaultWith's layout as the fallback: a missing
// file is created with it, a broken one falls back to it.
func LoadWith(dir string, aiUsage bool) (Layout, error) {
	return load(dir, func() Layout { return DefaultWith(aiUsage) })
}

func load(dir string, def func() Layout) (Layout, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		l := def()
		if werr := Write(dir, l); werr != nil {
			return l, fmt.Errorf("layout: creating %s: %w", FileName, werr)
		}
		return l, nil
	case err != nil:
		return def(), fmt.Errorf("layout: reading %s: %w", FileName, err)
	}
	var l Layout
	if err := json.Unmarshal(data, &l); err != nil {
		return def(), fmt.Errorf("layout: %s is not valid JSON, using defaults: %w", FileName, err)
	}
	if l.Version > Version || l.Version < 0 {
		// A future schema is rejected before any repair: the defaults
		// must not overwrite a file this build cannot read.
		return def(), fmt.Errorf("layout: %s version %d unknown, using defaults", FileName, l.Version)
	}
	if l.Version < Version {
		l.migrate()
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

// normalize repairs the recoverable parts of a loaded file (version 2):
// home widgets clamp to 1 or 2 cells and to the 4x2 canvas, empty ids
// drop, later overlapping widgets drop so the first placement wins, and
// a card id that shows up more than once — on the home canvas, in the
// tool page grid, or among the pins — keeps only its first place, in
// home-then-grid-then-pin order (design §6: one card, one place). Card
// spans clamp to 1 or 2; pins for unknown edges reset to the main display
// floating free.
func (l *Layout) normalize() {
	seen := map[string]bool{}
	home := l.Home[:0]
	for _, w := range l.Home {
		if w.ID == "" || seen[w.ID] {
			continue
		}
		w.W = clampCells(w.W)
		w.H = clampCells(w.H)
		if !fitsGrid(w.X, w.Y, w.W, w.H) {
			continue
		}
		if overlapsAny(home, w.ID, w.X, w.Y, w.W, w.H) {
			continue
		}
		seen[w.ID] = true
		home = append(home, w)
	}
	l.Home = home
	cards := l.Cards[:0]
	for _, card := range l.Cards {
		if card.ID == "" || seen[card.ID] {
			continue
		}
		card.Span = clampSpan(card.Span)
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
}

// migrate upgrades a version 0 (pre-stamp) or 1 file in memory: a card
// whose id is a known widget moves to the home canvas at the widget's
// default rectangle, but only when it fits the grid and does not overlap
// an already placed widget; otherwise it drops from the file. Every other
// card stays on the tool page in its original order with its span clamped
// to 1 or 2. Pins are untouched here — normalize then applies the
// one-card-one-place rule across the three places. The file itself is not
// rewritten.
func (l *Layout) migrate() {
	home := make([]Widget, 0, len(widgetCatalog))
	cards := make([]Card, 0, len(l.Cards))
	for _, card := range l.Cards {
		if card.ID == "" {
			continue
		}
		w, ok := defaultWidget(card.ID)
		if !ok {
			cards = append(cards, Card{ID: card.ID, Span: clampSpan(card.Span)})
			continue
		}
		if slices.ContainsFunc(home, func(o Widget) bool { return o.ID == w.ID }) {
			continue
		}
		if !fitsGrid(w.X, w.Y, w.W, w.H) || overlapsAny(home, w.ID, w.X, w.Y, w.W, w.H) {
			continue
		}
		home = append(home, w)
	}
	l.Home = home
	l.Cards = cards
	l.Version = Version
}

// index is the grid position of card id, or -1.
func (l *Layout) index(id string) int {
	return slices.IndexFunc(l.Cards, func(c Card) bool { return c.ID == id })
}

// hasCard reports whether id is placed anywhere: on the home canvas, on
// the tool page, or pinned to the desktop.
func (l *Layout) hasCard(id string) bool {
	if id == "" {
		return false
	}
	if l.HomeIndex(id) >= 0 || l.index(id) >= 0 {
		return true
	}
	return slices.ContainsFunc(l.Pins, func(p Pin) bool { return p.ID == id })
}

// HomeIndex is id's position in Home, or -1 when absent.
func (l *Layout) HomeIndex(id string) int {
	return slices.IndexFunc(l.Home, func(w Widget) bool { return w.ID == id })
}

// HomeAt returns the widget whose top-left cell is x, y.
func (l *Layout) HomeAt(x, y int) (Widget, bool) {
	for _, w := range l.Home {
		if w.X == x && w.Y == y {
			return w, true
		}
	}
	return Widget{}, false
}

// OverlapsHome reports whether the rectangle x, y, w, h shares a cell
// with a home widget. The widget with id is ignored, so a widget can ask
// about its own new rectangle.
func (l *Layout) OverlapsHome(id string, x, y, w, h int) bool {
	return overlapsAny(l.Home, id, x, y, w, h)
}

// AddWidget places w on the home canvas. It refuses — leaving the layout
// untouched — when the id is already placed anywhere (home, tool page, or
// desktop), the rectangle leaves the 4x2 grid, W or H is outside 1..2, or
// the rectangle overlaps an existing widget.
func (l *Layout) AddWidget(w Widget) bool {
	if w.ID == "" || l.hasCard(w.ID) {
		return false
	}
	if w.W < 1 || w.W > 2 || w.H < 1 || w.H > 2 || !fitsGrid(w.X, w.Y, w.W, w.H) {
		return false
	}
	if l.OverlapsHome(w.ID, w.X, w.Y, w.W, w.H) {
		return false
	}
	l.Home = append(l.Home, w)
	return true
}

// AddWidgetBest places id on the home canvas at the widget's default
// rectangle when it fits there, and otherwise at the first free cell in
// row-major order where a w by h widget fits. It reports false when no
// spot is free or id is already placed.
func (l *Layout) AddWidgetBest(id string, w, h int) bool {
	if id == "" || l.hasCard(id) {
		return false
	}
	if d, ok := defaultWidget(id); ok {
		if l.AddWidget(Widget{ID: id, X: d.X, Y: d.Y, W: w, H: h}) {
			return true
		}
	}
	for y := range GridRows {
		for x := range GridCols {
			if l.AddWidget(Widget{ID: id, X: x, Y: y, W: w, H: h}) {
				return true
			}
		}
	}
	return false
}

// RemoveWidget lifts id off the home canvas; other places are not
// touched. It reports whether a widget was removed.
func (l *Layout) RemoveWidget(id string) bool {
	i := l.HomeIndex(id)
	if i < 0 {
		return false
	}
	l.Home = slices.Delete(l.Home, i, i+1)
	return true
}

// MoveWidget moves id to x, y keeping its size. It reports false — the
// layout untouched — when id is not on the home canvas, the rectangle
// would leave the grid, or it would overlap another widget.
func (l *Layout) MoveWidget(id string, x, y int) bool {
	i := l.HomeIndex(id)
	if i < 0 {
		return false
	}
	w := l.Home[i]
	if !fitsGrid(x, y, w.W, w.H) || l.OverlapsHome(id, x, y, w.W, w.H) {
		return false
	}
	l.Home[i].X = x
	l.Home[i].Y = y
	return true
}

// SetWidgetSize resizes id to w by h cells, keeping its top-left anchor.
// It reports false — the layout untouched — when id is not on the home
// canvas, w or h is outside 1..2, or the rectangle would leave the grid
// or overlap another widget.
func (l *Layout) SetWidgetSize(id string, w, h int) bool {
	i := l.HomeIndex(id)
	if i < 0 || w < 1 || w > 2 || h < 1 || h > 2 {
		return false
	}
	cur := l.Home[i]
	if !fitsGrid(cur.X, cur.Y, w, h) || l.OverlapsHome(id, cur.X, cur.Y, w, h) {
		return false
	}
	l.Home[i].W = w
	l.Home[i].H = h
	return true
}

// AddCard appends id to the end of the tool page grid with span clamped
// to 1 or 2; it is a no-op when the card is already placed anywhere
// (home canvas, grid, or desktop), the one-card-one-place rule.
func (l *Layout) AddCard(id string, span int) {
	if id == "" || l.hasCard(id) {
		return
	}
	l.Cards = append(l.Cards, Card{ID: id, Span: clampSpan(span)})
}

// RemoveCard takes id off the tool page grid; a home widget or desktop
// pin is not touched — use RemoveWidget or RemovePin for those.
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

// AddPin places id on the desktop as pin; it is removed from the home
// canvas and the tool page grid first (the card lives in exactly one
// place). A card already pinned keeps its placement.
func (l *Layout) AddPin(pin Pin) {
	if pin.ID == "" {
		return
	}
	if slices.ContainsFunc(l.Pins, func(p Pin) bool { return p.ID == pin.ID }) {
		return
	}
	l.RemoveWidget(pin.ID)
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

// clampCells confines a widget side to the 1..2 cells a widget may span.
func clampCells(n int) int {
	return max(1, min(n, 2))
}

// fitsGrid reports whether the rectangle x, y, w, h lies inside the
// GridCols by GridRows canvas and has a positive cell size.
func fitsGrid(x, y, w, h int) bool {
	return x >= 0 && y >= 0 && w >= 1 && h >= 1 &&
		x+w <= GridCols && y+h <= GridRows
}

// overlapsAny reports whether the rectangle x, y, w, h shares a cell with
// a widget in home, ignoring the widget with id.
func overlapsAny(home []Widget, id string, x, y, w, h int) bool {
	for _, o := range home {
		if o.ID == id {
			continue
		}
		if x < o.X+o.W && o.X < x+w && y < o.Y+o.H && o.Y < y+h {
			return true
		}
	}
	return false
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

// LoadQuietWith is LoadWith with the error logged instead of returned.
func LoadQuietWith(dir string, aiUsage bool) Layout {
	l, err := LoadWith(dir, aiUsage)
	if err != nil {
		log.Print(err)
	}
	return l
}

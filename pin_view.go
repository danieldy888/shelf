//go:build darwin

package main

import (
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
)

// The pinned card's look (design/tools-v1.html §4): pure black, radius 18,
// a 0.5pt white 14% edge so it reads on any wallpaper — the one difference
// from the same card inside the notch panel. The card itself draws as
// toolCard draws it on the tools page (cardDesktop surface).
const (
	pinCardW  = 136 // one-column card; two-column cards are 278
	pinCardW2 = 278
)

var (
	pinEdgeColor  = ui.RGBA(255, 255, 255, 0.14)
	pinStripMark  = ui.RGBA(255, 255, 255, 0.40)
	pinEdgeStrip  = ui.RGBA(255, 255, 255, 0.22)
	pinHotFill    = ui.Hex("#06192E")
	pinExpandAnim = ui.Animation{Spring: ui.SpringOptions{Duration: pinExpand, Bounce: 0.12}}
)

// view draws the pinned window: the tool card while free or expanded,
// the narrow edge strip while collapsed. The window frame matches the
// content exactly; the shadow is the window's own.
func (it *pinItem) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	a := it.owner
	if !it.expanded {
		it.viewStrip(c)
		return
	}
	// Desktop drops belong to this zone, not the tools-page snapshot.
	opts := transfer.DropOptions{Formats: []transfer.Format{transfer.FileList, transfer.URIList, transfer.PNG, transfer.Text}, Operations: transfer.Copy}
	zone := ui.Column(c.Key("pin")).Fill().Radius(18).Clip()
	zone.Background(ui.Hex("#000000")).Border(0.5, pinEdgeColor)
	slide := zone.AnimateTo(pinRevealKey{it}, 1, pinExpandAnim)
	_, over := ui.DataDragOver(zone, opts)
	zone.Children(func() {
		card := a.toolCard(c, layout.Card{ID: it.cardID, Span: it.span}, cardDesktop).Fill()
		switch it.edge {
		case pinLeft:
			card.Translate(-float32(it.cardW-pinStripW)*(1-slide), 0)
		case pinRight:
			card.Translate(float32(it.cardW-pinStripW)*(1-slide), 0)
		}
	})
	if over || time.Now().Before(it.dropAt) {
		zone.Border(1.5, c.Theme().Accent)
	}
	it.trackPress(zone)
	if it.pressWas && !it.didMove && zone.Clicked() {
		// A press that never went far is a tap on the card; a press that
		// moved the window or only woke the strip is not.
		a.runToolCard(it.cardID)
	}
	it.pinMenu(zone)
	if drop, ok := ui.DropData(zone, opts); ok {
		a.pinReceive(it, drop.Data)
	}
}

// viewStrip draws the collapsed edge strip: an 8×56 black pill flush to
// the screen edge with the 2×16 grab mark the mock gives it.
func (it *pinItem) viewStrip(c *ui.Context) {
	root := ui.Column(c.Key("pin")).Fill()
	it.trackPress(root)
	it.pinMenu(root)
	root.Children(func() {
		mark := ui.Column(c.Key("strip")).Fill().Radius(4).Center()
		mark.Background(ui.Hex("#000000")).Border(0.5, pinEdgeStrip)
		mark.Children(func() {
			ui.Box(c.Key("mark")).Size(2, 16).Radius(1).Background(pinStripMark)
		})
	})
}

// trackPress records a press on the window so pinTick can tell a click
// from a window move and follow the cursor with the card. The grab point
// is where the press landed inside the window, so a re-drag starts
// without a jump; a press that only woke the strip re-anchors in pinTick
// once it goes far.
func (it *pinItem) trackPress(e ui.Element) {
	px, py, _ := e.PointerPosition()
	if e.Pressed() && !it.pressed {
		it.pressAt(float64(px), float64(py))
	}
}

// pressAt marks the press pinTick decides click-or-move from: the grab
// point is where it landed, and didMove clears — the previous press's
// move must not poison this tap, nor may this tap see its own drag.
func (it *pinItem) pressAt(px, py float64) {
	it.grabX, it.grabY = int(px+0.5), int(py+0.5)
	it.pressWas = it.expanded
	it.didMove = false
	it.pressed = true
}

// pinMenu is the right-click menu (design §4): 始终展开 toggles the
// auto-collapse, 放回刘海 hands the card back to the tools page, 从桌面移除
// takes it off the desktop without putting it back on the page.
func (it *pinItem) pinMenu(e ui.Element) {
	a := it.owner
	e.ContextMenu(func(m *ui.Menu) {
		if it.edge != pinFree {
			if m.Item("始终展开").Checked(it.always).Chosen() {
				it.always = !it.always
				a.savePins()
			}
			m.Separator()
		}
		if m.Item("放回刘海").Chosen() {
			it.wantKill, it.putBack = true, true
		}
		if m.Item("从桌面移除").Chosen() {
			it.wantKill, it.putBack = true, false
		}
	})
}

// pinRevealKey keys the strip-to-card slide to this pin's window.
type pinRevealKey struct{ it *pinItem }

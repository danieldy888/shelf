//go:build darwin

package main

// The 主页 page: a fixed 4×2 canvas of widgets (music, keep-awake, AI
// usage, recent files, clipboard). Widgets come from layout.json's Home
// list with their own rect, so they can sit in any free cells and leave
// gaps. Editing reuses the tools page's gestures — long press or pencil,
// wobble, ⊖ to remove, the corner handle to change size, drag to another
// cell, the catalogue to add, drag out of the panel to pin to the
// desktop.

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
	"shelf/internal/mac"
)

const (
	pageHome pageID = "home"

	homeGridCols = layout.GridCols
	homeGridRows = layout.GridRows
	homeGridGap  = 6

	homeLongPress    = 500 * time.Millisecond
	homeDragSlop     = 6
	homeSettleAnim   = 240 * time.Millisecond
	homeWobblePeriod = 520 * time.Millisecond
	homeWobbleDeg    = 0.7
)

// homeCellW/homeCellH are one grid cell; spans add the 6pt gap between
// cells so a 2×1 widget is 195 × 84 in the 396 × 174 area.
func homeCellW() float32 { return (contentW - (homeGridCols-1)*homeGridGap) / homeGridCols }
func homeCellH() float32 { return (contentH - (homeGridRows-1)*homeGridGap) / homeGridRows }

// homeSpanW/homeSpanH are a widget's size for n cells.
func homeSpanW(n int) float32 { return float32(n)*homeCellW() + float32(n-1)*homeGridGap }
func homeSpanH(n int) float32 { return float32(n)*homeCellH() + float32(n-1)*homeGridGap }

// homeX/homeY are a cell's top-left in content coordinates.
func homeX(col int) float32 { return float32(col) * (homeCellW() + homeGridGap) }
func homeY(row int) float32 { return float32(row) * (homeCellH() + homeGridGap) }

// homePress is one pointer session on a widget: the down, a possible
// long press or drag, and the settle flight after the release. All
// coordinates are canvas content space.
type homePress struct {
	id       string
	downAt   time.Time
	downX    float32
	downY    float32
	px, py   float32
	grabDX   float32
	grabDY   float32
	cardX    float32
	cardY    float32
	orig     layout.Widget
	dragging bool
	suppress bool
	settling bool

	settleJump bool
	settleX    float32
	settleY    float32
}

// homePage is the canvas page.
type homePage struct {
	host *shelf

	editing   bool
	picking   bool
	editAt    time.Time
	press     *homePress
	skipClick bool // the last release was a suppressed press, not a tap
	jiggle    string
	jiggleAt  time.Time

	// active is the set of widgets that took cells the last time the page
	// drew. A widget that comes back after its feature was off is compared
	// against it: one whose stored cells were taken in the meantime goes
	// back to the catalogue instead of overlapping.
	active map[string]bool

	// System output volume, re-read on the monitor tick's pace while the
	// page draws. The row greys out when the device has no settable
	// volume.
	volLoaded bool
	volOK     bool
	vol       float32
	volAt     time.Time

	// Progress/volume drag state: while dragging, the bar shows the
	// pointer's value; the command (seek / volume write) goes out once,
	// on release.
	dragPos bool
	posFrac float32
	dragVol bool
	volFrac float32
}

func (p *homePage) id() pageID    { return pageHome }
func (p *homePage) label() string { return "主页" }
func (p *homePage) icon(c *ui.Context, _ bool) ui.Element {
	return ui.Icon(c, homeNavSymbol)
}
func (p *homePage) enabled() bool { return true }

// title is the back row's page name/count (the P interface).
func (p *homePage) title() (name, count string) {
	if p.picking {
		return "小组件目录", ""
	}
	return "主页", ""
}

// actions is the back row's right side: the pencil, or 完成 while
// editing/picking. A full canvas has no cell for the dashed catalogue
// entry, so a + button takes its place beside 完成.
func (p *homePage) actions(c *ui.Context) []ui.Element {
	a := p.host
	live := a.tab == pageHome
	switch {
	case p.picking, p.editing:
		var out []ui.Element
		if p.editing && !p.picking && p.homePickable() {
			if _, _, free := p.homeFreeSpot(); !free {
				b := a.iconButton(c, "home-add", plusSymbol, "添加小组件")
				if live && b.Clicked() {
					p.picking, p.press = true, nil
				}
				out = append(out, b)
			}
		}
		return append(out, a.homeDoneButton(c, live))
	default:
		b := a.iconButton(c, "home-edit", pencilSymbol, "编辑主页")
		if live && b.Clicked() {
			p.editing, p.editAt = true, time.Now()
		}
		return []ui.Element{b}
	}
}

// homeDoneButton is the 完成 pill that leaves the canvas's edit mode (or
// backs out of the picker first).
func (a *shelf) homeDoneButton(c *ui.Context, live bool) ui.Element {
	b := ui.Row(c.Key("home-done")).Height(24).Padding(0, 12, 0, 12).Radius(12).
		Background(accent).Center().Label("完成")
	if live && b.Clicked() {
		if p := a.homePageOf(); p != nil {
			if p.picking {
				p.picking = false
			} else {
				p.editing, p.press = false, nil
			}
		}
	}
	b.Children(func() {
		panelLabel(c, nil, "完成", 12, 600).TextColor(ink)
	})
	return b
}

// homePageOf is the shelf's home page, or nil for test shelves that never
// registered one.
func (a *shelf) homePageOf() *homePage {
	if p, ok := a.page(pageHome).(*homePage); ok {
		return p
	}
	return nil
}

// resetHomeEdit ends editing and the picker; called on hide and on
// leaving the page so re-entry starts clean.
func (a *shelf) resetHomeEdit() {
	if p := a.homePageOf(); p != nil && (p.editing || p.picking || p.press != nil) {
		p.editing, p.picking, p.press = false, false, nil
		p.jiggle = ""
		if a.win != nil {
			a.win.Invalidate()
		}
	}
}

// homeWidgetDef is a widget's catalogue entry, nil when the widget is off
// or unknown.
func (p *homePage) homeWidgetDef(id string) *toolCardDef {
	def := toolCardDefFor(id)
	if def == nil || def.kind != cardWidget || !def.available(p.host) {
		return nil
	}
	return def
}

// homeDefaultSize is a widget's catalogue size.
func homeDefaultSize(id string) (w, h int) {
	switch id {
	case "playing":
		return 2, 2
	case "recent", "clip":
		return 2, 1
	default:
		return 1, 1
	}
}

// homeSizes are the sizes the corner handle can cycle through, the
// widget's current size first.
func homeSizes(id string) [][2]int {
	switch id {
	case "playing":
		return [][2]int{{2, 2}, {2, 1}, {1, 1}}
	case "awake":
		return [][2]int{{1, 1}, {2, 1}, {2, 2}}
	case "usage":
		return [][2]int{{1, 1}, {2, 1}}
	case "recent", "clip":
		return [][2]int{{2, 1}, {2, 2}, {1, 1}}
	}
	return [][2]int{{1, 1}}
}

// homePinWide reports the widgets whose card body needs a two-column pin
// window (278pt): the music player's cover, title and transport, the
// recent files' three thumbnails and the clipboard's icon plus two text
// lines all draw wider than a 136pt card's 112pt content. 保持唤醒 and
// AI 用量 fit one column.
func homePinWide(id string) bool {
	switch id {
	case "playing", "recent", "clip":
		return true
	}
	return false
}

// homePinSpan is the column span a widget keeps when it is pinned to the
// desktop: the canvas width it had, raised to two columns for the widgets
// that need it, so a pinned card never clips its content.
func homePinSpan(id string, w int) int {
	if w >= 2 || homePinWide(id) {
		return 2
	}
	return 1
}

// settleWidgets resolves a widget that comes back after its feature was
// off: while off its stored cells were free, so another widget may sit
// there now. The returning widget goes back to the 小组件目录 (its stored
// entry drops, the catalogue lists it again) instead of overlapping.
// Nothing to resolve means no write.
func (p *homePage) settleWidgets() {
	l := p.host.toolsLayout()
	active := make(map[string]bool, len(l.Home))
	var back []string
	for _, w := range l.Home {
		if p.host.inactiveWidget(w.ID) {
			continue
		}
		if !p.active[w.ID] && l.OverlapsHome(w.ID, w.X, w.Y, w.W, w.H) {
			back = append(back, w.ID)
			continue
		}
		active[w.ID] = true
	}
	p.active = active
	if len(back) == 0 {
		return
	}
	p.host.mutateToolsChanged(func(l *layout.Layout) bool {
		for _, id := range back {
			l.RemoveWidget(id)
		}
		return true
	})
}

// homeWidgets is the canvas's widget list in draw order, skipping widgets
// that are switched off (the AI usage page closed) so a disabled feature
// never draws a stale card.
func (p *homePage) homeWidgets() []layout.Widget {
	var out []layout.Widget
	for _, w := range p.host.toolsLayout().Home {
		if p.homeWidgetDef(w.ID) != nil {
			out = append(out, w)
		}
	}
	return out
}

// body draws the canvas into the sheet's fixed 396 × 174 content box.
func (p *homePage) body(c *ui.Context, _ bool, live bool) ui.Element {
	p.settleWidgets()
	if p.picking {
		return p.pickerBody(c, live)
	}
	canvas := ui.Column(c.Key("home-canvas")).Width(contentW).Height(contentH)
	canvas.Children(func() {
		for _, w := range p.homeWidgets() {
			p.homeWidget(c, w, live)
		}
		if p.editing {
			p.homeAddCell(c, live)
		}
		if d := p.press; d != nil && d.dragging {
			p.homeLifted(c, *d)
		}
	})
	return canvas
}

// homeAddCell is edit mode's way into the catalogue: a dashed 1×1 cell in
// the first free spot of the grid, the tools page's + slot in the canvas.
// It is drawn under the dragged copy and only while a widget is left to
// add; a canvas with no free cell falls back to the back row's + button.
func (p *homePage) homeAddCell(c *ui.Context, live bool) {
	if !p.homePickable() {
		return
	}
	col, row, ok := p.homeFreeSpot()
	if !ok {
		return
	}
	plus := ui.Column(c.Key("home-plus")).Absolute().Left(homeX(col)).Top(homeY(row)).
		Width(homeCellW()).Height(homeCellH()).Radius(14).Center().
		BorderWidth(1.5).BorderColor(ui.RGBA(255, 255, 255, 0.25)).
		BorderStyle(ui.BorderDashed).Label("添加小组件")
	if live {
		plus.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputPointerUp {
				p.picking, p.press = true, nil
				return true
			}
			return false
		})
		plus.PointerPosition()
	}
	plus.Children(func() {
		ui.Icon(c, plusSymbol).Size(22, 22).TextColor(ink3)
	})
}

// homeFreeSpot is the first free 1×1 cell in row-major order: the cell
// the dashed entry takes, and the same scan the catalogue places with.
func (p *homePage) homeFreeSpot() (col, row int, ok bool) {
	l := p.host.toolsLayout()
	for y := range homeGridRows {
		for x := range homeGridCols {
			if !l.OverlapsHome("", x, y, 1, 1) {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// homePlaced reports whether id already sits on the canvas, the tools
// page or a desktop pin. AddWidget refuses a duplicate, so the catalogue
// leaves such a widget out instead of offering a row that cannot work.
func homePlaced(l *layout.Layout, id string) bool {
	if l.HomeIndex(id) >= 0 {
		return true
	}
	if slices.ContainsFunc(l.Cards, func(c layout.Card) bool { return c.ID == id }) {
		return true
	}
	return slices.ContainsFunc(l.Pins, func(p layout.Pin) bool { return p.ID == id })
}

// homeCandidates are the catalogue's rows: available widgets that are not
// placed yet.
func (p *homePage) homeCandidates() []toolCardDef {
	l := p.host.toolsLayout()
	var defs []toolCardDef
	for _, def := range toolCatalog() {
		if def.kind == cardWidget && def.available(p.host) && !homePlaced(&l, def.id) {
			defs = append(defs, def)
		}
	}
	return defs
}

// homePickable reports whether the catalogue has anything to offer; with
// every widget placed, edit mode draws no entry at all.
func (p *homePage) homePickable() bool {
	l := p.host.toolsLayout()
	for _, def := range toolCatalog() {
		if def.kind == cardWidget && def.available(p.host) && !homePlaced(&l, def.id) {
			return true
		}
	}
	return false
}

// homeAddSizes are the sizes the catalogue tries for a widget: its
// default size first, then the smaller sizes the corner handle offers.
func homeAddSizes(id string) [][2]int {
	w, h := homeDefaultSize(id)
	out := [][2]int{{w, h}}
	for _, s := range homeSizes(id) {
		if s[0] <= w && s[1] <= h && (s[0] != w || s[1] != h) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// homeAddWidget places id at the first free cell in row-major order
// where one of its sizes fits, the default size first, and returns
// whether the layout changed.
func (p *homePage) homeAddWidget(id string) bool {
	for _, s := range homeAddSizes(id) {
		added := p.host.mutateToolsChanged(func(l *layout.Layout) bool {
			for y := range homeGridRows {
				for x := range homeGridCols {
					if x+s[0] > homeGridCols || y+s[1] > homeGridRows {
						continue
					}
					if l.OverlapsHome(id, x, y, s[0], s[1]) {
						continue
					}
					return l.AddWidget(layout.Widget{ID: id, X: x, Y: y, W: s[0], H: s[1]})
				}
			}
			return false
		})
		if added {
			return true
		}
	}
	return false
}

// homeWidget draws one widget at its cell and wires its pointer session.
func (p *homePage) homeWidget(c *ui.Context, w layout.Widget, live bool) {
	def := p.homeWidgetDef(w.ID)
	if def == nil {
		return
	}
	card := ui.Column(c.Key("hw-" + w.ID)).Radius(14).Background(fill1).MinWidth(0).MinHeight(0).
		Label(def.title).Clip()
	p.homeWidgetBody(c, card, w, p.editing)
	d := p.press
	parked := d != nil && d.id == w.ID && (d.dragging || d.settling)
	switch {
	case parked && d.settling:
		p.homeSettle(card, d)
		card.Absolute().Width(homeSpanW(w.W)).Height(homeSpanH(w.H))
	case parked:
		cx := card.AnimateTo("hx", homeX(w.X), ui.Snappy)
		cy := card.AnimateTo("hy", homeY(w.Y), ui.Snappy)
		d.cardX, d.cardY = cx, cy
		card.Absolute().Left(cx).Top(cy).Width(homeSpanW(w.W)).Height(homeSpanH(w.H)).Opacity(0)
	default:
		card.Absolute().Left(homeX(w.X)).Top(homeY(w.Y)).
			Width(homeSpanW(w.W)).Height(homeSpanH(w.H))
	}
	if live {
		p.homeInput(c, card, w, p.editing, live)
	}
	if !parked {
		p.homeWobble(c, card, w, p.editing)
	}
	if p.editing && !parked {
		p.homeEditChrome(c, card, w, live)
	}
}

// homeLifted draws the dragged copy at the pointer.
func (p *homePage) homeLifted(c *ui.Context, d homePress) {
	def := p.homeWidgetDef(d.id)
	if def == nil {
		return
	}
	card := ui.Column(c.Key("hw-lift-" + d.id)).Radius(14).Background(fill1).MinWidth(0).MinHeight(0).
		Label(def.title).Clip()
	p.homeWidgetBody(c, card, d.orig, true)
	card.Absolute().Left(d.px-d.grabDX).Top(d.py-d.grabDY).
		Width(homeSpanW(d.orig.W)).Height(homeSpanH(d.orig.H)).
		Scale(1.06, 1.06).TransformOrigin(0.5, 0.5).PassThrough()
}

// homeInput wires the widget's press: long press to edit, drag to a free
// cell, release outside the panel to pin, tap to run.
func (p *homePage) homeInput(c *ui.Context, card ui.Element, w layout.Widget, editing, live bool) {
	card.PointerPosition()
	card.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerDown:
			if p.press != nil {
				return false
			}
			p.skipClick = false // a fresh press owns the next release
			p.press = &homePress{
				id: w.ID, downAt: ev.Time,
				downX: homeX(w.X) + ev.X, downY: homeY(w.Y) + ev.Y,
				px: homeX(w.X) + ev.X, py: homeY(w.Y) + ev.Y,
				grabDX: ev.X, grabDY: ev.Y,
				cardX: homeX(w.X), cardY: homeY(w.Y),
				orig: w,
			}
			c.AnimationFrame()
			return true
		case ui.InputPointerMove:
			d := p.press
			if d == nil || d.id != w.ID || d.settling {
				return d != nil && d.id == w.ID && d.settling
			}
			d.px, d.py = d.cardX+ev.X, d.cardY+ev.Y
			if d.dragging {
				return true
			}
			if hypot(d.px-d.downX, d.py-d.downY) <= homeDragSlop {
				return true
			}
			if p.editing {
				d.dragging = true
			} else {
				d.suppress = true
			}
			return true
		case ui.InputPointerUp:
			d := p.press
			if d == nil || d.id != w.ID {
				return false
			}
			if d.settling {
				return true
			}
			if d.dragging {
				d.px, d.py = d.cardX+ev.X, d.cardY+ev.Y
				p.homeDrop(d)
				return true
			}
			// A press that moved past the slop is not a tap: the release
			// still reaches Clicked's check below, so remember it.
			p.skipClick = d.suppress
			p.press = nil
			return true
		}
		return false
	})
	// The long-press deadline runs on the frame clock.
	if d := p.press; d != nil && d.id == w.ID && !d.dragging && !d.settling && !d.suppress && !p.editing {
		c.AnimationFrame()
		if c.Now().Sub(d.downAt) >= homeLongPress {
			p.editing, p.editAt = true, c.Now()
			d.suppress = true
		}
	}
	if !editing && live && card.Clicked() {
		if p.skipClick {
			p.skipClick = false // a moved press never becomes a tap
			return
		}
		p.homeTapped(w)
	}
}

// homeTapped runs a widget's tap action. Music toggles playback; the
// other widgets defer to the card action.
func (p *homePage) homeTapped(w layout.Widget) {
	a := p.host
	if w.ID == "playing" {
		if a.np != nil {
			a.np.send(mac.NPPlayPause)
		}
		return
	}
	if !a.runToolCard(w.ID) {
		a.toolCardTapFeedback()
	}
}

// homeDrop commits a move inside the canvas, pins the widget when the
// release is outside the panel, or flies it back.
func (p *homePage) homeDrop(d *homePress) {
	a := p.host
	inside := d.px >= 0 && d.px <= contentW && d.py >= 0 && d.py <= contentH
	target := layout.Widget{X: d.orig.X, Y: d.orig.Y, W: d.orig.W, H: d.orig.H}
	if inside {
		col := int(math.Round(float64((d.px - d.grabDX) / (homeCellW() + homeGridGap))))
		row := int(math.Round(float64((d.py - d.grabDY) / (homeCellH() + homeGridGap))))
		col = max(0, min(col, homeGridCols-d.orig.W))
		row = max(0, min(row, homeGridRows-d.orig.H))
		if a.mutateToolsChanged(func(l *layout.Layout) bool { return l.MoveWidget(d.id, col, row) }) {
			target.X, target.Y = col, row
			for _, w := range a.toolsLayout().Home {
				if w.ID == d.id {
					target = w
				}
			}
		}
	} else {
		// The screen cursor is only needed for a drop outside the canvas;
		// asking the window server on every grid drop would block a
		// headless build.
		cursor := cursorPoint()
		panel := mygo.Rectangle{X: a.x, Y: a.y, Width: sheetW, Height: a.panelH()}
		if !insideRect(cursor, panel) {
			span := homePinSpan(d.id, d.orig.W)
			w := pinCardW
			if span == 2 {
				w = pinCardW2
			}
			disp := displayIDAt(cursor, 0)
			area := pinArea(disp)
			edge := pinReleaseEdge(cursor, area)
			frame := mygo.Rectangle{X: cursor.X - int(d.grabDX), Y: cursor.Y - int(d.grabDY), Width: w, Height: pinCardH}
			if edge != pinFree {
				frame = pinStripFrame(edge, area, cursor.Y)
			}
			if a.addPin(d.id, span, disp, edge, false, frame) != nil {
				p.press = nil
				return
			}
		}
	}
	d.settling, d.dragging = true, false
	d.settleX, d.settleY = homeX(target.X), homeY(target.Y)
}

// homeSettle animates the released widget back to its committed cell.
func (p *homePage) homeSettle(card ui.Element, d *homePress) {
	if !d.settleJump {
		card.Jump("hx", d.px-d.grabDX)
		card.Jump("hy", d.py-d.grabDY)
		card.Jump("hscale", 1.06)
		d.settleJump = true
	}
	cx := card.AnimateTo("hx", d.settleX, ui.Animation{Spring: ui.SpringOptions{Duration: homeSettleAnim, Bounce: 0.12}})
	cy := card.AnimateTo("hy", d.settleY, ui.Animation{Spring: ui.SpringOptions{Duration: homeSettleAnim, Bounce: 0.12}})
	sc := card.AnimateTo("hscale", 1, ui.Animation{Spring: ui.SpringOptions{Duration: homeSettleAnim, Bounce: 0.12}})
	card.Left(cx).Top(cy).Scale(sc, sc).TransformOrigin(0.5, 0.5)
	d.cardX, d.cardY = cx, cy
	if card.Settled("hx") && card.Settled("hy") && card.Settled("hscale") {
		p.press = nil
	}
}

// homeEditChrome draws the ⊖ badge and the size handle.
func (p *homePage) homeEditChrome(c *ui.Context, card ui.Element, w layout.Widget, live bool) {
	a := p.host
	card.Children(func() {
		badge := ui.Row(c.Key("home-minus")).Absolute().Top(2).Left(2).Size(18, 18).Radius(9).
			Background(ui.Hex("#3A3A3C")).Border(1.5, ui.Hex("#000000")).
			Center().Label("移除")
		if live {
			badge.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputPointerUp {
					a.mutateToolsChanged(func(l *layout.Layout) bool { return l.RemoveWidget(w.ID) })
					return true
				}
				return true
			})
			badge.PointerPosition()
		}
		badge.Children(func() {
			ui.Box(c).Size(8, 1.6).Radius(1).Background(ink)
		})

		handle := ui.Box(c.Key("home-size")).Absolute().Bottom(5).Right(5).Size(12, 12).
			BorderWidth(0, 1.6, 1.6, 0).BorderColor(ink2).Label("改尺寸")
		if live {
			handle.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputPointerUp {
					p.cycleSize(w)
					return true
				}
				return true
			})
			handle.PointerPosition()
		}
	})
}

// cycleSize switches a widget to the next size in its list that fits its
// anchor without overlapping another active widget. Sizes that do not fit
// (past the grid's edge, or on a neighbour) are skipped, so 2×1 on the
// bottom row still reaches 1×1 behind a refused 2×2; when no size applies
// the layout is left — and not rewritten.
func (p *homePage) cycleSize(w layout.Widget) {
	sizes := homeSizes(w.ID)
	start := 0
	for i, s := range sizes {
		if s[0] == w.W && s[1] == w.H {
			start = i
			break
		}
	}
	for i := 1; i < len(sizes); i++ {
		s := sizes[(start+i)%len(sizes)]
		if s[0] == w.W && s[1] == w.H {
			continue
		}
		applied := false
		p.host.mutateToolsChanged(func(l *layout.Layout) bool {
			applied = l.SetWidgetSize(w.ID, s[0], s[1])
			return applied
		})
		if applied {
			// Stop even when the write failed: the size did take, and a
			// second attempt would only report the same write error.
			return
		}
	}
}

// homeWobble is the edit-mode ±0.7° sway, phased by column.
func (p *homePage) homeWobble(c *ui.Context, card ui.Element, w layout.Widget, editing bool) {
	if c.Preferences().ReduceMotion {
		return
	}
	if p.jiggle == w.ID {
		t := float32(time.Since(p.jiggleAt)) / float32(toolJiggleSpan)
		if t >= 1 {
			p.jiggle = ""
		} else {
			card.Rotate(float32(math.Sin(float64(t*3*math.Pi)) * 3.0 * float64(1-t)))
			card.TransformOrigin(0.5, 0.5)
		}
		return
	}
	if !editing {
		return
	}
	phase := float64(time.Since(p.editAt)) / float64(homeWobblePeriod) * 2 * math.Pi
	if w.X%2 == 1 {
		phase += math.Pi
	}
	card.Rotate(float32(math.Sin(phase) * homeWobbleDeg))
	card.TransformOrigin(0.5, 0.5)
	c.AnimationFrame()
}

// pickerBody is the widget catalogue: one sheet of 30pt rows for the
// widgets that are on none of the three places yet. The entry that opens
// it is hidden when nothing is left, so an empty catalogue is only ever a
// picture of that last moment.
func (p *homePage) pickerBody(c *ui.Context, live bool) ui.Element {
	defs := p.homeCandidates()
	return ui.Column(c.Key("home-pick")).Grow(1).MinHeight(0).Children(func() {
		sheet := ui.Column(c.Key("home-picker")).FillWidth().Radius(14).
			Background(ui.Hex("#1C1C1E")).Padding(10, 12, 12, 12)
		sheet.Children(func() {
			panelLabel(c, nil, "小组件", 11, 600).TextColor(ink3).Margin(2, 0, 6, 0)
			if len(defs) == 0 {
				panelLabel(c, nil, "全部已在主页", 11, 0).TextColor(ink3)
				return
			}
			ui.Grid(c.Key("home-plist")).ColumnTracks(ui.Fr(1), ui.Fr(1)).
				GapY(4).GapX(10).FillWidth().Children(func() {
				for _, def := range defs {
					p.pickRow(c, def, live)
				}
			})
		})
	})
}

// pickRow is one catalogue row; a tap places the widget at the first free
// cell that fits and returns to the edit grid. A full canvas keeps the
// catalogue open and says why nothing happened.
func (p *homePage) pickRow(c *ui.Context, def toolCardDef, live bool) ui.Element {
	a := p.host
	row := ui.Row(c.Key("hpi-"+def.id)).Height(toolPickRowH).Radius(8).
		AlignItems(ui.Center).Gap(8).Padding(0, 8, 0, 8).Label(def.title)
	if live {
		if row.Hovered() {
			row.Background(fill2)
		}
		row.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputPointerUp {
				return false
			}
			if p.homeAddWidget(def.id) {
				p.picking = false
				return true
			}
			a.toastStatus("主页没有空位，先移除一个小组件", time.Now().Add(2*time.Second), statusInfo)
			return true
		})
		row.PointerPosition()
	}
	row.Children(func() {
		ui.Icon(c, def.icon).Size(15, 15).TextColor(ink)
		panelLabel(c, nil, def.title, 12, 500).TextColor(ink)
		ui.Box(c).Grow(1)
		w, h := homeDefaultSize(def.id)
		panelLabel(c, nil, fmt.Sprintf("%d × %d", w, h), 10, 0).TextColor(ink3)
	})
	return row
}

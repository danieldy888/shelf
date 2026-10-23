//go:build darwin

package main

import (
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
	"shelf/internal/mac"
)

// Desktop pins: a tool card dragged out of the tools page becomes a small
// floating window on the desktop. Releasing it within pinSnapDist of a
// screen's left or right edge snaps it there as a pinStripW×pinStripH strip
// that expands on hover (or while a file drag comes within pinWakeDist),
// collapses pinHideDelay after the pointer leaves, and can be pinned open.
// Releasing the card top-centre on the notch hands it back to the tools
// page.
//
// The card the window shows comes from the tool catalog (tools_cards.go):
// a.toolCard(c, layout.Card{...}, cardDesktop) draws it, a.runToolCard(id)
// runs its tap and a.runToolCardDrop(id, data) takes its file drop. The
// placement lives in layout.json's pins list (internal/layout), written
// through the tools page's mutateTools so Cards and Pins stay one file.
const (
	pinSnapDist  = 24 // release this close to a screen edge and it docks
	pinWakeDist  = 40 // a file drag this close to a strip wakes it
	pinStripW    = 8  // collapsed strip width, flush to the edge
	pinStripH    = 56 // collapsed strip height
	pinStripGap  = 8  // stacked strips on one edge stay this far apart
	pinHideDelay = 600 * time.Millisecond
	pinExpand    = 240 * time.Millisecond
	pinCardH     = 84
	pinReturnH   = 120                    // release this high, centred on the notch, returns the card
	pinHoldAfter = 900 * time.Millisecond // drop confirmation on the card
	pinDragStart = 4                      // a press that goes this far is a window move
)

// pinEdge is the screen side a pin docks to.
type pinEdge int

const (
	pinFree pinEdge = iota
	pinLeft
	pinRight
)

func (e pinEdge) layout() layout.Edge {
	switch e {
	case pinLeft:
		return layout.EdgeLeft
	case pinRight:
		return layout.EdgeRight
	}
	return ""
}

func pinEdgeOf(e layout.Edge) pinEdge {
	switch e {
	case layout.EdgeLeft:
		return pinLeft
	case layout.EdgeRight:
		return pinRight
	}
	return pinFree
}

// pinItem is one pinned window: the layout.Pin's placement plus its
// runtime state. frame is the applied screen rect (card size while free
// or expanded, strip size while collapsed).
type pinItem struct {
	owner     *shelf
	cardID    string
	span      int
	cardW     int // 136 or 278 from span; height is always pinCardH
	homeIndex int // grid slot it came from; -1 pins it back at the end
	display   int64
	edge      pinEdge
	always    bool // always expanded; the hide delay never collapses it
	expanded  bool // valid only while edge != pinFree
	frame     mygo.Rectangle
	leftAt    time.Time
	dropAt    time.Time // accent border lingers this long after a drop
	pressed   bool      // a press on the card is being held
	pressWas  bool      // it was already expanded when that press began
	moving    bool      // the press went far: it is a window move, not a click
	grabX     int
	grabY     int
	wantKill  bool // a menu asked to remove it; pinTick does the destroy
	putBack   bool // …and whether the card returns to the tools page
	win       *mygo.Window
}

// pinDisplays is the display list the geometry sees; tests replace it.
var pinDisplays = func() []mygo.Display { return mygo.Screen.Displays() }

// pinArea returns the work area of a display, or of the primary when the
// ID is gone (unplugged, or a test list that lacks it): the remembered
// edge survives the move.
func pinArea(display int64) mygo.Rectangle {
	var primary mygo.Rectangle
	for _, d := range pinDisplays() {
		if d.ID == display {
			return d.WorkArea
		}
		if d.Primary || primary == (mygo.Rectangle{}) {
			primary = d.WorkArea
		}
	}
	return primary
}

func (it *pinItem) displayRect() mygo.Rectangle { return pinArea(it.display) }

func pinDisplayID(display int64) string {
	if display == 0 {
		return ""
	}
	return strconv.FormatInt(display, 10)
}

func pinParseDisplay(s string) int64 {
	id, _ := strconv.ParseInt(s, 10, 64)
	return id
}

// pinClamp holds r inside area: a pinned card never sits under the menu
// bar or off its display.
func pinClamp(r mygo.Rectangle, area mygo.Rectangle) mygo.Rectangle {
	r.X = min(max(r.X, area.X), max(area.X, area.X+area.Width-r.Width))
	r.Y = min(max(r.Y, area.Y), max(area.Y, area.Y+area.Height-r.Height))
	return r
}

// pinExpandFrame is the card's frame while docked: flush to the docked
// edge, vertically centred on the strip it opened from, inside the work
// area.
func pinExpandFrame(edge pinEdge, area mygo.Rectangle, w, cy int) mygo.Rectangle {
	f := mygo.Rectangle{Y: cy - pinCardH/2, Width: w, Height: pinCardH}
	if edge == pinRight {
		f.X = area.X + area.Width - w
	} else {
		f.X = area.X
	}
	return pinClamp(f, area)
}

// pinStripFrame is the collapsed strip's frame: flush to the edge, kept
// inside the work area.
func pinStripFrame(edge pinEdge, area mygo.Rectangle, cy int) mygo.Rectangle {
	f := mygo.Rectangle{Y: cy - pinStripH/2, Width: pinStripW, Height: pinStripH}
	if edge == pinRight {
		f.X = area.X + area.Width - pinStripW
	} else {
		f.X = area.X
	}
	return pinClamp(f, area)
}

// pinReleaseEdge reports which edge a released card docks to: the release
// point within pinSnapDist of its display's left or right work-area edge.
func pinReleaseEdge(p mygo.Point, area mygo.Rectangle) pinEdge {
	switch {
	case p.X >= area.X-pinSnapDist && p.X <= area.X+pinSnapDist:
		return pinLeft
	case p.X >= area.X+area.Width-pinSnapDist && p.X <= area.X+area.Width+pinSnapDist:
		return pinRight
	}
	return pinFree
}

// pinReturnZone reports whether the dragged pin's centre lands on the
// notch: the hardware notch when the shelf tracks this display, else a
// release-band the same shape at the top of the display the centre is
// on. The same zone drives the 松开放回 bulge and the put-back release.
func (a *shelf) pinReturnZone(centre mygo.Point) bool {
	if centre.Y >= a.notchY+notchHoverPadY+pinReturnH {
		return false
	}
	// The shelf follows the cursor across displays (monitorDrag places
	// it on the cursor's): a pin dragged at the notch is by definition
	// on the shelf's display. The zone centres on the hardware notch,
	// widening like overNotch does.
	nx, nw := a.notchX, a.notchW
	if nw <= 0 {
		nw = 200 // no measured notch: the strip's default centre
	}
	half := float64(nw)/2 + notchHoverPadX
	return abs(centre.X-(nx+nw/2)) <= int(half)
}

// pinStagger spaces collapsed strips on one edge of one display
// pinStripGap apart, top to bottom. Expanded cards keep their frame: the
// overlap is theirs while the pointer is on them.
func pinStagger(pins []*pinItem) {
	type edgeKey struct {
		disp int64
		edge pinEdge
	}
	groups := map[edgeKey][]*pinItem{}
	for _, it := range pins {
		if it.edge == pinFree || it.expanded {
			continue
		}
		k := edgeKey{it.display, it.edge}
		groups[k] = append(groups[k], it)
	}
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		sort.SliceStable(g, func(i, j int) bool { return g[i].frame.Y < g[j].frame.Y })
		area := pinArea(g[0].display)
		y := g[0].frame.Y
		for _, it := range g {
			it.frame.Y = max(it.frame.Y, y)
			y = it.frame.Y + pinStripH + pinStripGap
		}
		if y-pinStripGap > area.Y+area.Height {
			// Overflowing the bottom: pull the column back up from the last.
			y = g[len(g)-1].frame.Y
			for i := len(g) - 2; i >= 0; i-- {
				g[i].frame.Y = min(g[i].frame.Y, y-pinStripH-pinStripGap)
				y = g[i].frame.Y
			}
		}
	}
}

// pinApplyFrame pushes it.frame to the native window.
func (it *pinItem) applyFrame() {
	if it.win != nil {
		mac.SetWindowFrame(it.win.NativeHandle(), it.frame.X, it.frame.Y, it.frame.Width, it.frame.Height)
	}
}

// pinMoveFrame is the card frame while it is dragged: card-sized, top-left
// under the grab point, clamped into the work area so it can never be
// lost off-screen or parked under the menu bar.
func (it *pinItem) pinMoveFrame(p mygo.Point, area mygo.Rectangle) mygo.Rectangle {
	return pinClamp(mygo.Rectangle{X: p.X - it.grabX, Y: p.Y - it.grabY, Width: it.cardW, Height: pinCardH}, area)
}

// pinNear reports whether p is within dist of r, on any side.
func pinNear(p mygo.Point, r mygo.Rectangle, dist int) bool {
	return p.X >= r.X-dist && p.X < r.X+r.Width+dist && p.Y >= r.Y-dist && p.Y < r.Y+r.Height+dist
}

// addPin pins a card to the desktop: the card leaves the tools page grid
// (layout.AddPin enforces the one-card-one-place rule), its window opens
// at frame, and the placement lands in layout.json. cardID is a tool
// catalog id; frame is the release rect (a strip frame when docked,
// card-sized elsewhere). The tools page's drag-out calls this.
func (a *shelf) addPin(cardID string, span int, display int64, edge pinEdge, always bool, frame mygo.Rectangle) *pinItem {
	homeIndex := slices.IndexFunc(a.toolsLayout().Cards, func(c layout.Card) bool { return c.ID == cardID })
	area := pinArea(display)
	a.mutateTools(func(l *layout.Layout) {
		l.AddPin(layout.Pin{
			ID: cardID, Span: span, Display: pinDisplayID(display), Edge: edge.layout(),
			X: frame.X - area.X, Y: frame.Y - area.Y, AlwaysOpen: always,
		})
	})
	w := pinCardW
	if span == 2 {
		w = pinCardW2
	}
	it := &pinItem{
		owner: a, cardID: cardID, span: span, cardW: w,
		homeIndex: homeIndex, display: display, edge: edge,
		always: always, expanded: edge == pinFree,
		// A display that shrank since the file was written still gets a
		// visible pin: the frame clamps back into its work area.
		frame: pinClamp(frame, pinArea(display)),
	}
	a.pins = append(a.pins, it)
	pinStagger(a.pins)
	for _, other := range a.pins {
		other.applyFrame() // restagger existing strips before the new window shows
	}
	pinOpenWindow(it)
	a.savePins()
	return it
}

// pinOpenWindow opens a pin's native window; a seam so the drag-out and
// restore paths can be tested without a window server.
var pinOpenWindow = func(it *pinItem) { it.show() }

// show opens the pin's window: a borderless non-activating panel at the
// floating level, visible on every Space, movable only through pinTick so
// snap-on-release stays under the app's control.
func (it *pinItem) show() {
	f := it.frame
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: appName + " 固定项", Width: f.Width, Height: f.Height,
		X: f.X, Y: f.Y,
		Hidden: true, Frameless: true, Transparent: true, DisableShadow: true,
		AlwaysOnTop: true, SkipTaskbar: true, Panel: true,
		DisableMove: true, DisableResize: true, DisableMinimize: true, DisableMaximize: true, DisableFullScreen: true,
		Content: ui.View(func(c *ui.Context) { it.view(c) }),
	})
	it.win = w
	w.SetVisibleOnAllWorkspaces(true)
	mac.ConfigurePinWindow(w.NativeHandle())
	mac.SetWindowFrame(w.NativeHandle(), f.X, f.Y, f.Width, f.Height)
	w.OnClose(func(e *mygo.CloseEvent) {
		// A system close (Space switch, Exposé tidy-up) removes the pin:
		// the card returns to the tools page, same as 放回刘海. The window
		// is already going away; unpin must not Destroy it again.
		it.win = nil
		it.owner.unpin(it, true)
	})
	w.ShowInactive()
}

// unpin removes it from the desktop and its pin from the layout.
// putBack returns the card to the tools page grid — its home slot, or
// the end when that is gone (放回刘海); otherwise the card leaves the
// desktop without rejoining the page and can be re-added from the "+"
// catalog later (从桌面移除, design §4).
func (a *shelf) unpin(it *pinItem, putBack bool) {
	for i, p := range a.pins {
		if p == it {
			a.pins = append(a.pins[:i], a.pins[i+1:]...)
			break
		}
	}
	a.mutateTools(func(l *layout.Layout) {
		// A pin restored from layout.json has no remembered slot (-1):
		// 放回刘海 appends it to the page's end rather than the front.
		before := it.homeIndex
		if before < 0 {
			before = len(l.Cards)
		}
		l.RemovePin(it.cardID, putBack, before)
	})
	w := it.win
	it.win = nil
	if w != nil {
		w.Destroy()
	}
	a.savePins()
}

// pinDisplaysChanged re-anchors pins whose display went away: docked pins
// keep their edge on the primary display, floating ones land inside it.
func (a *shelf) pinDisplaysChanged() {
	var primary int64
	seen := map[int64]bool{}
	for _, d := range pinDisplays() {
		seen[d.ID] = true
		if d.Primary || primary == 0 {
			primary = d.ID
		}
	}
	changed := false
	for _, it := range a.pins {
		if seen[it.display] {
			continue
		}
		it.display = primary
		area := it.displayRect()
		if it.edge != pinFree {
			it.frame = pinStripFrame(it.edge, area, it.frame.Y+it.frame.Height/2)
		} else {
			it.frame = pinClamp(it.frame, area)
		}
		it.applyFrame()
		changed = true
	}
	if changed {
		pinStagger(a.pins)
		for _, it := range a.pins {
			it.applyFrame()
		}
		a.savePins()
	}
}

// pinTick drives the pinned windows from the monitor: window moves while
// the press is a drag, hover expand, the delayed collapse and the
// always-open state. p is the cursor, down the left button, fileDrag a
// system drag the cards can receive.
func (a *shelf) pinTick(now time.Time, p mygo.Point, down, fileDrag bool) {
	returning := false
	for _, it := range append([]*pinItem{}, a.pins...) {
		if it.wantKill {
			a.unpin(it, it.putBack)
			continue
		}
		area := it.displayRect()
		if it.pressed {
			if down {
				dx, dy := p.X-(it.frame.X+it.grabX), p.Y-(it.frame.Y+it.grabY)
				if !it.moving && (abs(dx) >= pinDragStart || abs(dy) >= pinDragStart) {
					// The press went far: it is a window move. Re-anchor
					// the grab to the top-left so a press that only woke
					// the strip does not jump, and show the card the
					// dragged frame is sized for.
					it.moving, it.expanded = true, true
					it.grabX, it.grabY = 0, 0
				}
				if it.moving {
					it.frame = it.pinMoveFrame(p, area)
					it.applyFrame()
					centre := mygo.Point{X: it.frame.X + it.frame.Width/2, Y: it.frame.Y + it.frame.Height/2}
					returning = returning || a.pinReturnZone(centre)
				}
			} else {
				moved := it.moving
				it.pressed, it.moving = false, false
				if moved {
					a.pinRelease(it, p, area)
				}
			}
			continue
		}
		if it.edge == pinFree {
			continue
		}
		if it.always {
			if !it.expanded {
				it.expanded = true
				it.frame = pinExpandFrame(it.edge, area, it.cardW, it.frame.Y+pinStripH/2)
				it.applyFrame()
				it.invalidate()
			}
			continue // always expanded: the hide delay never runs
		}
		over := insideRect(p, it.frame) || (fileDrag && pinNear(p, it.frame, pinWakeDist))
		if it.expanded {
			if over {
				it.leftAt = time.Time{}
			} else if it.leftAt.IsZero() {
				it.leftAt = now
			} else if now.Sub(it.leftAt) >= pinHideDelay {
				it.expanded = false
				it.leftAt = time.Time{}
				it.frame = pinStripFrame(it.edge, area, it.frame.Y+pinCardH/2)
				it.applyFrame()
				it.invalidate()
			}
		} else if over {
			it.expanded = true
			it.leftAt = time.Time{}
			it.frame = pinExpandFrame(it.edge, area, it.cardW, it.frame.Y+pinStripH/2)
			it.applyFrame()
			it.invalidate()
		}
	}
	// A pin held over the notch bulges 松开放回: peekView reads
	// a.pinReturning for the shape, the release in pinRelease reads the
	// same zone. The flag clears the tick the drag ends; monitorDrag's
	// own fold takes the shell down on the next pass.
	if a.pinReturning != returning {
		a.pinReturning = returning
		if returning && !a.visible {
			a.setPeek(true)
		}
	}
}

func (it *pinItem) invalidate() {
	if it.win != nil {
		it.win.Invalidate()
	}
}

// pinRelease settles a moved card: back to the tools page on the notch,
// docked to an edge, or floating where it landed.
func (a *shelf) pinRelease(it *pinItem, p mygo.Point, area mygo.Rectangle) {
	centre := mygo.Point{X: it.frame.X + it.frame.Width/2, Y: it.frame.Y + it.frame.Height/2}
	if a.pinReturnZone(centre) {
		a.unpin(it, true)
		return
	}
	it.display = displayIDAt(centre, it.display)
	area = it.displayRect()
	if edge := pinReleaseEdge(p, area); edge != pinFree {
		it.edge, it.expanded = edge, false
		it.frame = pinStripFrame(edge, area, p.Y)
		pinStagger(a.pins)
		for _, other := range a.pins {
			other.applyFrame() // the stagger may move strips it did not release
		}
	} else {
		it.edge, it.expanded = pinFree, true
		it.frame = pinClamp(it.frame, area)
	}
	it.applyFrame()
	a.savePins()
}

// displayIDAt resolves the display a released card belongs to: the one
// containing its centre, else the nearest, else the remembered one.
func displayIDAt(p mygo.Point, keep int64) int64 {
	var nearest int64
	best := -1
	for _, d := range pinDisplays() {
		if insideRect(p, d.Bounds) {
			return d.ID
		}
		dx := max(0, max(d.Bounds.X-p.X, p.X-d.Bounds.X-d.Bounds.Width))
		dy := max(0, max(d.Bounds.Y-p.Y, p.Y-d.Bounds.Y-d.Bounds.Height))
		if best < 0 || dx*dx+dy*dy < best {
			best, nearest = dx*dx+dy*dy, d.ID
		}
	}
	if nearest != 0 {
		return nearest
	}
	return keep
}

// pinReceive is the drop on a pinned card: files the card accepts run
// its action, anything else lands in the tray like a drop on the notch —
// a release on a pin never loses the files. dropAt keeps the accent
// border up a moment as the confirmation the notch capsule gives
// elsewhere.
func (a *shelf) pinReceive(it *pinItem, data transfer.Data) {
	def := toolCardDefFor(it.cardID)
	if def != nil && def.kind == cardAction {
		if files, err := dropFilesData(data); err == nil && len(files) > 0 && def.accepts(classifyDrop(files)) {
			a.runToolCardDrop(it.cardID, data)
			it.dropAt = time.Now().Add(pinHoldAfter)
			it.invalidate()
			return
		}
	}
	if err := a.receive(data); err != nil {
		a.toastStatus("放入失败："+err.Error(), time.Time{}, statusErr)
	}
	it.dropAt = time.Now().Add(pinHoldAfter)
	it.invalidate()
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// savePins mirrors the live pins into layout.json's pin list: positions
// are stored inside each display's visible frame, as layout.Pin wants.
// mutateTools keeps the tools page's Cards untouched and writes the file.
func (a *shelf) savePins() {
	pins := make([]layout.Pin, 0, len(a.pins))
	for _, it := range a.pins {
		area := it.displayRect()
		pins = append(pins, layout.Pin{
			ID: it.cardID, Span: it.span, Display: pinDisplayID(it.display),
			Edge: it.edge.layout(), X: it.frame.X - area.X, Y: it.frame.Y - area.Y,
			AlwaysOpen: it.always,
		})
	}
	a.mutateTools(func(l *layout.Layout) { l.Pins = pins })
}

// pinRestore opens the pinned cards' windows from the shared layout (the
// tools page reads the same toolsLayout copy). A card the catalog does
// not know yet still pins — an unknown id draws the empty cell toolCard
// reserves.
func (a *shelf) pinRestore() {
	for _, p := range a.toolsLayout().Pins {
		disp := pinParseDisplay(p.Display)
		edge := pinEdgeOf(p.Edge)
		w := pinCardW
		if p.Span == 2 {
			w = pinCardW2
		}
		area := pinArea(disp)
		var frame mygo.Rectangle
		if edge == pinFree {
			frame = mygo.Rectangle{X: area.X + p.X, Y: area.Y + p.Y, Width: w, Height: pinCardH}
		} else {
			frame = pinClamp(mygo.Rectangle{X: area.X + p.X, Y: area.Y + p.Y, Width: pinStripW, Height: pinStripH}, area)
		}
		it := &pinItem{
			owner: a, cardID: p.ID, span: p.Span, cardW: w, homeIndex: -1,
			display: disp, edge: edge, always: p.AlwaysOpen,
			expanded: edge == pinFree, frame: frame,
		}
		a.pins = append(a.pins, it)
		pinOpenWindow(it)
	}
}

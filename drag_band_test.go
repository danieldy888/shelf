//go:build darwin

package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/settings"
)

// dragShelf is a closed shelf on a notched display, mid incoming drag.
func dragShelf() *shelf {
	return &shelf{
		notchX: 600, notchY: 0, notchW: 180, notchH: 32,
		x: 410, y: 0, h: 560,
		dispBounds:   mygo.Rectangle{X: 0, Y: 0, Width: 1470, Height: 956},
		dragging:     true,
		settings:     settings.Default(),
		thumbnails:   map[string]*ui.Bitmap{},
		thumbPending: map[string]bool{},
	}
}

// driveMonitor stubs the drag/cursor seams and runs monitorDrag with p.
func driveMonitor(t *testing.T, a *shelf, p mygo.Point, active bool, serial int64, types []string) {
	t.Helper()
	old := cursorPoint
	oldS, oldT, oldD := dragState, dragTypes, dragLeftDown
	cursorPoint = func() mygo.Point { return p }
	dragState = func() (bool, int64) { return active, serial }
	dragTypes = func() []string { return types }
	dragLeftDown = func() bool { return active }
	a.monitorDrag(time.Now())
	cursorPoint, dragState, dragTypes, dragLeftDown = old, oldS, oldT, oldD
}

// monitorDrag must walk the band in and out: outside the band the drag
// gets only the hint frame, inside it the panel opens edgeOpened, and
// leaving both band and panel past edgeLeaveDelay collapses it again.
func TestMonitorDragBandTransitions(t *testing.T) {
	a := dragShelf()
	a.dragging = false // the monitor owns the flag; start idle
	cx := a.stripCenter()
	// Outside the band: hint only, no panel, frame inside the menu bar.
	driveMonitor(t, a, mygo.Point{X: 100, Y: 200}, true, 1, []string{"public.file-url"})
	if !a.dragging || a.inBand || a.dropHot || a.visible {
		t.Fatalf("outside band: dragging=%v inBand=%v dropHot=%v visible=%v", a.dragging, a.inBand, a.dropHot, a.visible)
	}
	if f := a.desiredFrame(); f.Height > max(32, a.notchH) {
		t.Fatalf("outside band the frame is %dpt tall, above the menu bar", f.Height)
	}
	// Inside the band: dropHot, the panel opens edgeOpened.
	driveMonitor(t, a, mygo.Point{X: cx, Y: a.notchY + a.notchH - 1}, true, 1, []string{"public.file-url"})
	if !a.inBand || !a.dropHot || !a.visible || !a.edgeOpened {
		t.Fatalf("inside band: inBand=%v dropHot=%v visible=%v edgeOpened=%v", a.inBand, a.dropHot, a.visible, a.edgeOpened)
	}
	a.animationStart = time.Time{} // the open animation is done
	// Leaving band and panel past edgeLeaveDelay collapses it.
	old := cursorPoint
	cursorPoint = func() mygo.Point { return mygo.Point{X: 100, Y: 800} }
	oldS, oldT, oldD := dragState, dragTypes, dragLeftDown
	dragState = func() (bool, int64) { return true, 1 }
	dragTypes = func() []string { return []string{"public.file-url"} }
	dragLeftDown = func() bool { return true }
	a.monitorDrag(time.Now())
	a.monitorDrag(time.Now().Add(edgeLeaveDelay + time.Millisecond))
	cursorPoint, dragState, dragTypes, dragLeftDown = old, oldS, oldT, oldD
	if a.visible {
		t.Fatal("leaving the band did not collapse the edge-opened panel")
	}
	if a.dropHot || a.inBand {
		t.Fatalf("after leaving: dropHot=%v inBand=%v", a.dropHot, a.inBand)
	}
	// Drag ends: the strip comes back to the no-drag resident frame.
	idle := (&shelf{notchX: 600, notchY: 0, notchW: 180, notchH: 32,
		x: 410, y: 0, h: 560, settings: settings.Default()}).desiredFrame()
	driveMonitor(t, a, mygo.Point{X: 100, Y: 800}, false, 1, nil)
	if a.dragging || a.dropHot || a.inBand {
		t.Fatalf("drag end: dragging=%v dropHot=%v inBand=%v", a.dragging, a.dropHot, a.inBand)
	}
	if got := a.desiredFrame(); got != idle {
		t.Fatalf("after the drag the frame %+v did not return to idle %+v", got, idle)
	}
}

// The drag guard pushes the cursor to its corridor's lower edge. That
// landing row must sit below the menu bar and inside the window the
// frame keeps for it — in every drag state, including suppressed.
func TestDragGuardLandsBelowMenuBarInsideTheWindow(t *testing.T) {
	a := dragShelf()
	g := a.dragGuardRegion()
	landing := g.Y + g.Height
	if landing <= a.notchH {
		t.Fatalf("guard landing %d is inside the %dpt menu bar", landing, a.notchH)
	}
	// In-band unsuppressed: the frame grew for the pill; landing inside.
	a.inBand, a.dropHot = true, true
	f := a.desiredFrame()
	if landing >= f.Height {
		t.Fatalf("dropHot: landing %d outside frame %+v", landing, f)
	}
	// In-band suppressed: the pill is off but the window still holds the
	// landing row — the clamped cursor can never escape onto the bar.
	a.suppressed, a.dropHot = true, false
	f = a.desiredFrame()
	if landing >= f.Height {
		t.Fatalf("suppressed in-band: landing %d outside frame %+v", landing, f)
	}
	// Out of band: no push is possible (the pin only acts inside its
	// corridor), so the frame is free to stay menu-bar high.
	a.suppressed, a.inBand = false, false
	f = a.desiredFrame()
	if f.Height > max(32, a.notchH) {
		t.Fatalf("out-of-band frame %dpt tall exceeds the menu bar", f.Height)
	}
}

// The incoming-drag frame must never exceed the menu bar's height while
// nothing taller is drawn: the widened hint stays band-high, and the
// player/capsule's tall frames are not retained through a drag.
func TestIncomingDragFrameStaysInMenuBand(t *testing.T) {
	a := dragShelf()
	a.settings.IslandNowPlaying = true
	a.np = &nowPlayingModule{host: a, have: true, last: mac.NowPlayingState{Title: "Track"}}
	f := a.desiredFrame()
	bandW := max(180, a.notchW) + 2*dropBandPadX
	hintW := int(180+dropHintExtraW) + 2*earR // band may widen ±ears to cover the hint's corners
	if f.Height > max(32, a.notchH) {
		t.Fatalf("incoming-drag frame %dpt tall left the menu bar", f.Height)
	}
	if f.Width != max(bandW, hintW) {
		t.Fatalf("incoming-drag frame %dpt wide, want %d (band or hint+ears)", f.Width, max(bandW, hintW))
	}
	if f.X != a.stripCenter()-f.Width/2 || f.Y != a.y {
		t.Fatalf("incoming-drag frame %+v lost the notch anchor", f)
	}
	// Outside the band the drawn hint is also band-high: frame and shell
	// agree that nothing reaches below the menu bar.
	if w, h := a.peekSize(); h > float32(max(32, a.notchH)) {
		t.Fatalf("drag hint %.0fx%.0f exceeds the menu bar", w, h)
	}
}

// Entering the band aims the frame at the drop pill; mid-morph it still
// covers the drawn shell plus ears, and a fold remnant taller than the
// pill keeps its own room.
func TestDropBandFrameFollowsTheDrawnPill(t *testing.T) {
	a := dragShelf()
	a.dropHot = true
	f := a.desiredFrame()
	pillW := 180 + dropPillExtraW
	if f.Height != 32+dropPillBody || f.Width != pillW+2*earR {
		t.Fatalf("in-band frame %+v must cover the %dx%d pill plus ears", f, pillW, 32+dropPillBody)
	}
	// The pill mid-grow: the frame tracks the drawn shell plus ears.
	a.fx.peek, a.fx.w, a.fx.h = true, 240, 48
	f = a.desiredFrame()
	if f.Height != 64 || f.Width != max(pillW, 240)+2*earR {
		t.Fatalf("frame %+v does not follow the drawn pill", f)
	}
	// A fold remnant taller than the pill keeps its own room.
	a.fx.w, a.fx.h = 300, 90
	f = a.desiredFrame()
	if f.Height != 91 || f.Width != 300+2*earR {
		t.Fatalf("frame %+v clipped the fold remnant 300x90", f)
	}
}

// Shelf's own drag-out keeps the resident strip — no band, no hint.
func TestOwnDragKeepsTheNormalStrip(t *testing.T) {
	a := dragShelf()
	a.ownDrag = true
	f := a.desiredFrame()
	bandW := max(180, a.notchW) + 2*dropBandPadX
	if f.Width == bandW && f.Height == max(32, a.notchH) {
		t.Fatalf("own drag-out shrank to the drop band %+v", f)
	}
	if w, h := a.peekSize(); w == 180+dropPillExtraW || h == 32+dropPillBody {
		t.Fatalf("own drag-out drew the drop pill %.0fx%.0f", w, h)
	}
}

// Shake-to-summon arms only for drags carrying files or image data.
func TestDragSummonsBasketOnlyForFilesAndImages(t *testing.T) {
	for _, types := range [][]string{
		{"public.file-url"},
		{"NSFilenamesPboardType", "com.apple.finder.node"},
		{"com.apple.pasteboard.promised-file-url"},
		{"com.apple.NSFilePromiseItemMetaData"},
		{"public.png"},
		{"public.tiff", "public.utf8-plain-text"},
		{"public.heic"},
		{"public.file-promise"},
	} {
		if !dragSummonsBasket(types) {
			t.Fatalf("%v must summon the basket", types)
		}
	}
	for _, types := range [][]string{
		nil,
		{"public.utf8-plain-text"},
		{"public.utf8-plain-text", "NSStringPboardType"}, // Tern tab drag
		{"public.url", "public.utf8-plain-text"},         // a dragged link
		{"public.data"},
	} {
		if dragSummonsBasket(types) {
			t.Fatalf("%v must not summon the basket", types)
		}
	}
}

// The mouse-down observer and the peek shape both see the same drag band:
// outside it a drag press belongs to the app underneath, inside it the
// band accepts.
func TestIncomingDragAcceptsOnlyInsideTheBand(t *testing.T) {
	a := dragShelf()
	bandW := max(180, a.notchW) + 2*dropBandPadX
	f := a.desiredFrame()
	hintW := int(180+dropHintExtraW) + 2*earR
	if f.Width != max(bandW, hintW) {
		t.Fatalf("drag frame %d wide, want %d (band or hint+ears)", f.Width, max(bandW, hintW))
	}
	// The band's vertical reach is the guard's landing row: anything
	// below it is neither in the band nor the frame, so the drop
	// belongs to whatever is underneath.
	below := mygo.Point{X: a.notchX + a.notchW/2, Y: a.notchY + dropBandH(a.notchH) + 1}
	if a.dropBand(below) {
		t.Fatalf("point %+v below the band hit it", below)
	}
	if below.Y < f.Y+f.Height && below.X >= f.X && below.X < f.X+f.Width {
		t.Fatalf("point %+v below the band is inside the drag frame %+v", below, f)
	}
}

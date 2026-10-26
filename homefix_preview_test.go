//go:build darwin

package main

// Regression test for the final review: a widget dragged out of the 主页
// canvas followed the pointer in a preview window of its own, because the
// page clips its own drawing at the 396 × 174 content box.

import (
	"testing"

	"github.com/egoist/mygo"

	"shelf/internal/layout"
)

// TestHomeDragOutUsesDragPreview covers the three moments of that window
// headlessly: it appears once the copy would leave the content box, it
// closes when the drag comes back or is cancelled, and a release beyond the
// panel hands the very same window to the committed pin.
func TestHomeDragOutUsesDragPreview(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	var opened []*pinItem
	oldPreview, oldOpen, oldCursor := pinOpenPreview, pinOpenWindow, cursorPoint
	pinOpenPreview = func(it *pinItem) { opened = append(opened, it) }
	pinOpenWindow = func(it *pinItem) { opened = append(opened, it) }
	cursorPoint = func() mygo.Point { return mygo.Point{X: 1000, Y: 500} }
	t.Cleanup(func() { pinOpenPreview, pinOpenWindow, cursorPoint = oldPreview, oldOpen, oldCursor })

	a, p := musicShelf(t)
	a.dataDir = t.TempDir()
	a.frame = mygo.Rectangle{X: 300, Y: 40}
	a.x, a.y = 300, 40
	a.winH = 1 // a native window exists; the seams keep it in-process
	p.editing = true
	homeReviewLayout(a, layout.Widget{ID: "playing", X: 0, Y: 0, W: 2, H: 2})
	view := pageTester(p, true)
	view.Frame()

	card, ok := view.Find("音乐")
	if !ok {
		t.Fatalf("music card missing: %v", view.Texts())
	}
	// Inside the box the page draws the lifted copy (its edit form: title
	// and size); the preview must not exist yet.
	x, y := card.X+40, card.Y+30
	view.Press(x, y)
	view.Move(x+20, y+10)
	if p.preview.window() != nil {
		t.Fatal("a drag inside the content box opened a preview window")
	}
	if n := countText(view.Texts(), "2 × 2"); n != 2 {
		t.Fatalf("the lifted copy is not drawn inside the box (%d edit bodies), texts=%v", n, view.Texts())
	}

	// Past the box's right edge the clip would cut the copy: the preview
	// takes over and follows the pointer, at homePinSpan's width.
	out := x + contentW
	view.Move(out, y)
	it := p.preview.window()
	if it == nil {
		t.Fatal("dragging past the content box left the copy clipped in the page")
	}
	if it.cardID != "playing" || it.span != 2 || it.cardW != pinCardW2 {
		t.Fatalf("preview = %s span %d width %d, want playing at 2 columns / %d",
			it.cardID, it.span, it.cardW, pinCardW2)
	}
	if len(opened) != 1 || opened[0] != it {
		t.Fatalf("the preview window was opened %d times", len(opened))
	}
	if n := countText(view.Texts(), "2 × 2"); n != 1 {
		t.Fatalf("the page still draws the copy outside the box (%d edit bodies), texts=%v", n, view.Texts())
	}
	// A move inside the preview keeps that one window, it does not stack.
	view.Move(out+40, y+20)
	if p.preview.window() != it || len(opened) != 1 {
		t.Fatalf("the preview followed the pointer by reopening: %d windows", len(opened))
	}
	// The window keeps the grab point under the pointer: cursor
	// (300+476, 40+50) less the 40/30 grabbed inside the card.
	if it.frame.X != 736 || it.frame.Y != 60 {
		t.Fatalf("preview frame %+v, want the card grabbed at 40,30 under (776,90)", it.frame)
	}

	// Back inside the box: the preview goes away at once.
	view.Move(x+30, y+10)
	if p.preview.window() != nil {
		t.Fatal("dragging back into the content box left the preview on screen")
	}
	if n := countText(view.Texts(), "2 × 2"); n != 2 {
		t.Fatalf("the copy did not return to the page (%d edit bodies), texts=%v", n, view.Texts())
	}

	// Out again, then a release on the desktop: the committed pin is the
	// very item the preview was showing, so the window is handed over.
	view.Move(out, y)
	preview := p.preview.window()
	if preview == nil {
		t.Fatal("the second drag out has no preview")
	}
	view.Release(out, y)
	if p.preview.window() != nil {
		t.Fatal("the released drag still owns its preview")
	}
	if len(a.pins) != 1 || a.pins[0] != preview {
		t.Fatalf("the pin is not the preview's card: pins=%+v", a.pins)
	}
	if widgetByID(a.toolsLayout().Home, "playing").ID != "" {
		t.Fatal("the pinned widget stayed on the canvas")
	}
	if last := opened[len(opened)-1]; last != preview {
		t.Fatalf("the release opened a new window instead of reusing the preview")
	}
}

// TestHomeDragPreviewClosesOnCancel is the escape path: leaving edit mode
// mid-drag must not leave the copy on the desktop.
func TestHomeDragPreviewClosesOnCancel(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	oldPreview, oldCursor := pinOpenPreview, cursorPoint
	pinOpenPreview = func(*pinItem) {}
	cursorPoint = func() mygo.Point { return mygo.Point{X: 1000, Y: 500} }
	t.Cleanup(func() { pinOpenPreview, cursorPoint = oldPreview, oldCursor })

	a, p := musicShelf(t)
	a.dataDir = t.TempDir()
	a.frame = mygo.Rectangle{X: 300, Y: 40}
	a.x, a.y = 300, 40
	a.winH = 1
	p.editing = true
	homeReviewLayout(a, layout.Widget{ID: "awake", X: 2, Y: 0, W: 1, H: 1})
	view := pageTester(p, true)
	view.Frame()
	card, ok := view.Find("保持唤醒")
	if !ok {
		t.Fatalf("awake card missing: %v", view.Texts())
	}
	x, y := card.X+20, card.Y+20
	view.Press(x, y)
	view.Move(x+contentW, y)
	if p.preview.window() == nil {
		t.Fatal("no preview outside the content box")
	}
	a.resetHomeEdit()
	if p.preview.window() != nil {
		t.Fatal("leaving edit mode left the drag preview on the desktop")
	}
	if widgetByID(a.toolsLayout().Home, "awake").ID != "awake" {
		t.Fatalf("the cancelled drag left the canvas: %+v", a.toolsLayout().Home)
	}
}

// countText counts the labels showing s.
func countText(texts []string, s string) int {
	n := 0
	for _, text := range texts {
		if text == s {
			n++
		}
	}
	return n
}

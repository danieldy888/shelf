//go:build darwin

package main

// Regression tests for the shell motion fixes: the ear corners' room in
// the window, monotonic mid-open closes, the drawn-shell-driven window and
// click pass-through on close, the invisible rest snap, the linear content
// fade, and the drawn-shell height freeze. Plus the drop-zone isolation
// fixes: headless board reads, per-drag caches, headless thumbnails and
// the same-frame window sync on height changes.

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/store"
)

// #13: the shell's top edge reaches earR past its flanks on each side; the
// window must hold the ears on every frame of the open flight and at rest.
func TestFrameAlwaysHoldsTheEars(t *testing.T) {
	a := motionWindow()
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame()
	a.visible = true
	for i := range 120 {
		view.Frame()
		f := a.desiredFrame()
		if float64(f.Width) < float64(a.fx.w)+2*earR-0.5 {
			t.Fatalf("frame %d: window %d wide clips the ears of a %.1fpt shell", i, f.Width, a.fx.w)
		}
		view.Advance(16 * time.Millisecond)
	}
	if got, want := a.desiredFrame().Width, panelWidth+2*earR; got != want {
		t.Fatalf("the settled frame is %d wide, want the panel plus both ears (%d)", got, want)
	}
	// The idle strip holds the peek shape's ears too.
	b := motionShelf()
	w, _ := b.peekSize()
	if f := b.desiredFrame(); f.Width < int(w+0.5)+2*earR {
		t.Fatalf("the idle strip %d wide clips the peek shape's ears", f.Width)
	}
}

// #14: closing mid-open must never grow the shell again — the opening
// spring's outward velocity does not carry into the fold.
func TestMidOpenCloseNeverGrowsOutward(t *testing.T) {
	a := motionWindow()
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame()
	a.visible = true
	// Open partway: long enough to be fast-moving, not settled.
	for range 8 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	a.visible = false
	prevW, prevH := a.fx.w, a.fx.h
	for i := range 60 {
		view.Frame()
		if a.fx.h > prevH+2 || a.fx.w > prevW+2 {
			t.Fatalf("frame %d after the close: the shell grew to %.1fx%.1f from %.1fx%.1f",
				i, a.fx.w, a.fx.h, prevW, prevH)
		}
		prevW, prevH = a.fx.w, a.fx.h
		view.Advance(16 * time.Millisecond)
	}
	if a.fx.openK > 0.01 {
		t.Fatalf("the close never folded: openK %.3f", a.fx.openK)
	}
}

// Reopening mid-close must stay continuous: the shell turns around where
// it is — it may keep shrinking at the fold's speed for the frame already
// in flight, but it never snaps back to the notch.
func TestMidCloseReopenIsContinuous(t *testing.T) {
	a := motionWindow()
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame()
	a.visible = true
	for range 40 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	a.visible = false
	for range 8 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	a.visible = true // reopen mid-fold
	prevH := a.fx.h
	for i := range 60 {
		view.Frame()
		if a.fx.h < prevH-30 {
			t.Fatalf("frame %d after the reopen: the shell snapped down to %.1f from %.1f", i, a.fx.h, prevH)
		}
		prevH = a.fx.h
		view.Advance(16 * time.Millisecond)
	}
	if a.fx.openK < 0.99 {
		t.Fatalf("the reopen never finished: openK %.3f", a.fx.openK)
	}
}

// #15: once the drawn shell has folded back to the peek shape, the window
// shrinks to the strip and lets clicks through — even while the tick's
// eased progress still says "open".
func TestFoldedShellRestoresClicksAndTheStrip(t *testing.T) {
	a := motionWindow()
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame()
	a.visible = true
	for range 40 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	a.visible = false
	for range 60 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	if a.fx.openK > 0.01 {
		t.Fatalf("the fold never finished: openK %.3f", a.fx.openK)
	}
	a.progress = 0.2 // the 30ms tick's EaseOut still trails the drawn spring
	a.syncMousePassThrough()
	if !a.ignoringMouse {
		t.Fatal("the folded shell still eats clicks 110ms early")
	}
	if f := a.desiredFrame(); f.Height > 120 {
		t.Fatalf("the window still holds the panel's %dpt height after the fold", f.Height)
	}
}

// #16: the fold's last approach is smooth — the step that lands on the
// notch band is sub-point, not a visible snap.
func TestCloseTailHasNoVisibleSnap(t *testing.T) {
	a := motionWindow()
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame()
	a.visible = true
	for range 40 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	a.visible = false
	base := float32(max(32, a.notchH))
	prev := a.fx.h
	finalStep := float32(-1)
	for range 60 {
		view.Frame()
		if prev > base && a.fx.h <= base+0.001 {
			finalStep = prev - a.fx.h // the landing step
			break
		}
		prev = a.fx.h
		view.Advance(16 * time.Millisecond)
	}
	if finalStep < 0 {
		t.Fatal("the fold never reached the notch band")
	}
	if finalStep > 1 {
		t.Fatalf("the fold snapped the last %.1fpt into place", finalStep)
	}
}

// #17: the content fade is the design's linear ramp, not an eased one.
func TestContentFadeIsLinear(t *testing.T) {
	if got := contentFade(0.7); got < 0.49 || got > 0.51 {
		t.Fatalf("contentFade(0.7) = %.3f, want the linear 0.5", got)
	}
	if got := contentFade(0.4); got != 0 {
		t.Fatalf("contentFade(0.4) = %.3f, want 0", got)
	}
	if got := contentFade(1); got != 1 {
		t.Fatalf("contentFade(1) = %.3f, want 1", got)
	}
}

// tallPage is a registered test page taller than the tray, for the closing
// freeze test (#18).
type tallPage struct{ host *shelf }

func (p *tallPage) id() pageID         { return "tall" }
func (p *tallPage) label() string      { return "高页" }
func (p *tallPage) enabled() bool      { return true }
func (p *tallPage) stageH() int        { return 400 }
func (p *tallPage) sharesHeight() bool { return false }
func (p *tallPage) icon(c *ui.Context, active bool) ui.Element {
	return ui.Box(c).Size(16, 16)
}
func (p *tallPage) body(c *ui.Context, over bool, live bool) ui.Element {
	return ui.Box(c).Grow(1)
}
func (p *tallPage) moduleTitle() (string, string) { return "高页", "" }
func (p *tallPage) moduleTrailing(c *ui.Context, live bool) []ui.Element {
	return nil
}

// #18: the closing height freeze lasts while the drawn shell is still
// folding (openK), not just while the tick's progress says open — a page
// taller than the tray must not jump at the fold's tail.
func TestClosingFreezeFollowsTheDrawnShell(t *testing.T) {
	a := motionWindow()
	a.registerPage(&tallPage{host: a})
	a.tab = "tall"
	a.visible = true
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	for range 40 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	tallH := a.shownH
	if tallH < 350 {
		t.Fatalf("the tall page did not size the panel: %.0f", tallH)
	}
	a.hide(hideExplicit) // resets the tab to the tray: panelH drops at once
	a.progress = 0       // the tick's EaseOut already ended; the drawn spring has not
	for i := range 30 {
		view.Frame()
		if a.fx.openK <= 0.01 {
			break
		}
		if a.shownH != tallH {
			t.Fatalf("frame %d: the frozen height jumped to %.1f mid-fold (openK %.3f)",
				i, a.shownH, a.fx.openK)
		}
		view.Advance(16 * time.Millisecond)
	}
	if a.fx.openK > 0.01 {
		t.Fatal("the fold never finished")
	}
}

// #19: a headless shelf never reads the real drag pasteboard — tests get
// files only from a substituted board.
func TestHeadlessSnapNeverReadsTheRealBoard(t *testing.T) {
	a := &shelf{dragging: true, visible: true, notchW: 180, notchH: 32,
		thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	if got := a.dropZoneSnap(true); got != nil {
		t.Fatalf("a headless shelf read the real drag board: %v", got)
	}
}

// #20: a new drag session never inherits the previous one's files or
// classification, even when the board's changeCount coincides.
func TestNewDragDropsTheLastSessionsCache(t *testing.T) {
	dir := t.TempDir()
	img := writeFile(t, dir, "1.png", testPNGBytes(t))
	zip1 := writeFile(t, dir, "2.zip", []byte("PK"))
	files := []string{img}
	reads := new(int)
	oldF, oldC := dragFiles, dragFilesChange
	dragFiles = func(*shelf) []string { *reads++; return files }
	dragFilesChange = func(*shelf) int64 { return 7 } // the board serial coincides
	t.Cleanup(func() { dragFiles, dragFilesChange = oldF, oldC })
	a := &shelf{dragging: true, visible: true, notchW: 180, notchH: 32,
		thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	if got := a.dropZoneSnap(true); len(got) != 1 || a.drop.class != classImages {
		t.Fatalf("first drag: files %v class %v", got, a.drop.class)
	}
	a.dragSerial = 9 // the monitor saw a new drag session
	files = []string{zip1}
	got := a.dropZoneSnap(true)
	if *reads != 2 {
		t.Fatalf("a new drag reused the cached board read (reads=%d)", *reads)
	}
	if len(got) != 1 || got[0] != zip1 || a.drop.class != classArchives {
		t.Fatalf("the new drag inherited the old session: files %v class %v", got, a.drop.class)
	}
}

// A headless shelf does not start a decode with no window to receive it.
func TestHeadlessThumbnailNeverTouchesTheWindow(t *testing.T) {
	dir := t.TempDir()
	png := writeFile(t, dir, "p.png", testPNGBytes(t))
	a := &shelf{thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	item := store.Item{ID: "x", Path: png, Kind: store.KindImage}
	if got := a.thumbnail(item); got != nil {
		t.Fatal("a headless thumbnail decoded synchronously")
	}
	if a.thumbActive != 0 || len(a.thumbPending) != 0 {
		t.Fatal("a headless shelf started a thumbnail decode")
	}
}

// #22: the drop zone changing panelH re-syncs the window on the same
// frame, not at the next 30ms tick.
func TestDropZoneHeightSyncsTheWindowAtOnce(t *testing.T) {
	a := motionWindow()
	a.visible = true
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	for range 40 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	before := a.frame.Height
	dir := t.TempDir()
	img := writeFile(t, dir, "1.png", testPNGBytes(t))
	t.Setenv("SHELF_DEBUG_DROP_FILES", img)
	view.Frame()
	if a.frame.Height <= before {
		t.Fatalf("the window kept its %dpt height with the drop zone up", a.frame.Height)
	}
	if want := a.desiredFrame().Height; a.frame.Height != want {
		t.Fatalf("window height %d, want the synced %d", a.frame.Height, want)
	}
	t.Setenv("SHELF_DEBUG_DROP_FILES", "")
	view.Frame()
	view.Advance(300 * time.Millisecond)
	for range 40 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	if a.frame.Height != before {
		t.Fatalf("after the zone the window is %dpt tall, want the original %d", a.frame.Height, before)
	}
	// The frame's width still holds the ears after every sync.
	if a.frame.Width < int(a.fx.w+0.5)+2*earR-1 {
		t.Fatalf("the synced frame %d clips the ears of a %.0fpt shell", a.frame.Width, a.fx.w)
	}
}

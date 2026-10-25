//go:build darwin

package main

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
)

// The notch shell: geometry, the three states (idle, peek, expanded), their
// animations and mouse pass-through. Content lives in panel.go and items.go.

func (a *shelf) place(p mygo.Point) {
	a.placeAt(p)
	if a.win != nil {
		a.syncWindowFrame()
		a.win.SetOpacity(1)
	}
	a.syncDragGuard()
}

func (a *shelf) placeAt(p mygo.Point) {
	d := mygo.Screen.DisplayNearestPoint(p)
	a.dispBounds = d.Bounds
	nx, ny, nw, nh, hasNotch := mac.NotchRect(d.ID)
	a.notchX, a.notchY, a.notchW, a.notchH, a.hasNotch = nx, ny, nw, nh, hasNotch
	a.topInset = nh
	// The panel, the peek pill and the strip share one axis: the notch's
	// centre. A different centre made the expansion jump sideways.
	a.x = d.Bounds.X + (d.Bounds.Width-sheetW)/2
	if a.notchW > 0 {
		a.x = a.notchX + a.notchW/2 - sheetW/2
	}
	a.y = d.Bounds.Y
}

// overNotch 只认实测刘海几何：光标真正进入刘海热区（notchX/notchY 为原点，
// 四周留 hover 余量）才为真。显示器未匹配时 notchW 为 0，刘海交互整体禁用，
// 不会在屏幕左上角凭空出现一个悬停/触发区。
func (a *shelf) overNotch(p mygo.Point) bool {
	return a.notchW > 0 && a.notchH > 0 &&
		p.X >= a.notchX-notchHoverPadX && p.X <= a.notchX+a.notchW+notchHoverPadX &&
		p.Y >= a.notchY && p.Y <= a.notchY+a.notchH+notchHoverPadY
}

// Hover changes only the drawing, not the native window geometry. Keeping one
// stable transparent strip for idle/peek avoids resizing the backing surface
// every time the pointer crosses the notch. Only opening the panel changes it.
// The strip is always wide enough for the now-playing peek pill, so a track
// coming or going changes the drawing without a native resize. Every width
// includes the two ear corners (earR): the shell's top edge reaches past its
// flanks by that much on each side, and no frame may clip the arc.
func (a *shelf) desiredFrame() mygo.Rectangle {
	closing := !a.visible && a.fx.openK > 0
	if a.visible || closing {
		top := a.sheetTop()
		if closing {
			// Follow the drawn shell through its fold, with room for the
			// ears and two points below the bottom curve.
			w := int(math.Ceil(float64(a.fx.w))) + 2*earR
			h := int(a.fx.h+0.5) + top + 2
			if a.fb.up(time.Now()) {
				// Folding into the capsule: the shell may grow wider or
				// taller than the panel sliver it starts from. No glow
				// reaches past it anymore, so the room is the shell plus
				// its ears, shake and overshoot only.
				w = max(w, capsuleFrameW())
				h = max(h, int(float32(max(32, a.notchH))+fbBody+0.5)+2)
			}
			return mygo.Rectangle{X: a.stripCenter() - w/2, Y: a.y, Width: w, Height: h}
		}
		// The frame follows the level the panel is on, never the larger of
		// the two: an island opening on the sheet's box laid a 451pt
		// transparent frame — the shell is 420 — over the menu-bar icons
		// beside it. A level spring leaves the drawn shell wider or taller
		// than its target for a few frames and a spring on its way
		// overshoots, so the frame holds that shell and the level's
		// overshoot room. The width keeps the room at rest too: shrinking
		// it when the spring settles would shift the window under a shell
		// that has not moved, and the transparent columns beside the shell
		// no longer take clicks anyway (overlayAcceptsMouse).
		tw, th := float32(sheetW), float32(sheetH)
		if a.currentLevel() == 1 {
			tw, th = islandW, islandH
		}
		w, roomH := openFrameRoom(tw, float32(th+float32(top)))
		w = max(w, int(math.Ceil(float64(a.fx.w)))+2*earR)
		// Once settled the height is the shell's exact box, so its
		// transparent margin cannot swallow clicks below.
		h := max(int(math.Ceil(float64(th)))+top, int(math.Ceil(float64(a.fx.h)))+top)
		if a.fx.openMoving {
			h = max(h, roomH)
		}
		return mygo.Rectangle{X: a.stripCenter() - w/2, Y: a.y, Width: w, Height: h}
	}
	// An incoming system drag shrinks the resident strip to the notch's
	// menu-bar band: outside it Shelf only shows a widened band-height
	// hint, inside it the shell grows 「松开放入」. The frame always
	// covers the shape being aimed at (plus ears) and the shell actually
	// drawn, so the morph, a fold remnant and the Spaces edge guard stay
	// covered — and the wings/capsule's taller frames are never held
	// through a drag.
	if a.dragging && !a.ownDrag {
		bandH := max(32, a.notchH)
		w := max(180, a.notchW) + 2*dropBandPadX
		h := bandH
		pw, ph := a.peekSize()
		w = max(w, int(math.Ceil(float64(pw)))+2*earR)
		h = max(h, int(math.Ceil(float64(ph))))
		if a.fx.peek {
			w = max(w, int(math.Ceil(float64(a.fx.w)))+2*earR)
			h = max(h, int(math.Ceil(float64(a.fx.h)))+1)
		}
		if a.inBand {
			// The cursor is inside the band (entered through the menu
			// bar): the frame must hold the guard's landing row even
			// while the shell is still the small hint — a suppressed
			// drag grows the window too, so the clamped cursor never
			// leaves its surface. Before entry the window stays
			// menu-bar high; the guard's push lands on the bar's last
			// row, which is already inside this frame.
			h = max(h, dropBandH(a.notchH))
		}
		return mygo.Rectangle{X: a.stripCenter() - w/2, Y: a.y, Width: w, Height: h}
	}
	// The resident strip holds the largest peek shape for the current
	// state, so hovering only redraws; it resizes when a track comes or
	// goes, while nothing is drawn.
	pw, ph := a.peekSize()
	if a.np != nil && a.np.hasTrack() && a.settings.IslandNowPlaying {
		// The capsule and the wings can alternate on the same strip: keep
		// room for both so one hand-off never resizes the window.
		mw, mh := a.wingsSize()
		pw, ph = max(pw, mw), max(ph, mh)
	}
	if a.fx.peek {
		// The shell can still be the larger shape it is folding back from,
		// e.g. the capsule just expired or a track stopped playing.
		pw, ph = max(pw, a.fx.w), max(ph, a.fx.h)
	}
	if a.fx.sizeMoving {
		// A morph in flight overshoots its target by a couple of points
		// (Bounce 0.15); the strip keeps headroom so no frame is clipped.
		pw, ph = pw*(1+openOvershoot), ph*(1+openOvershoot)
	}
	w := int(math.Ceil(float64(pw))) + 2*earR
	h := int(math.Ceil(float64(ph)))
	if a.capsuleUp(time.Now()) || a.fb.drawn {
		// The completion capsule is another peek shape: while it shows, the
		// strip holds room for it at the widest the text can make it, plus
		// its ears, shake and grow-out overshoot. The shape grows out of
		// the notch in ~160ms, well inside the tick that resizes the strip,
		// so no frame is cut.
		w = max(w, capsuleFrameW())
		h = max(h, int(float32(max(32, a.notchH))+fbBody+0.5)+2)
	}
	return mygo.Rectangle{X: a.stripCenter() - w/2, Y: a.y, Width: w, Height: h}
}

// Use a single native setFrame plus a top-left pin so AppKit cannot leave
// the overlay a menu-bar-height below the hardware notch.
func (a *shelf) syncWindowFrame() {
	if a.win == nil || a.settingFrame {
		return
	}
	r := a.desiredFrame()
	if r == a.frame {
		return
	}
	if a.buildingView && a.winH != 0 {
		// MyGo captured the surface size before view() ran. Resizing here
		// would present this frame's old canvas in the new native frame.
		// Update posts to the next main-loop pass; resolve the latest target
		// there, including any close or reopen issued in the meantime.
		if !a.frameQueued {
			a.frameQueued = true
			a.win.Update(func() {
				a.frameQueued = false
				if a.win != nil && !a.win.IsDestroyed() {
					a.syncWindowFrame()
				}
			})
		}
		return
	}
	a.settingFrame = true
	defer func() { a.settingFrame = false }()
	mac.SetWindowFrame(a.winH, r.X, r.Y, r.Width, r.Height)
	a.frame = r
	// The shell moved with the window, so the pointer may now be on it (a
	// display change recentres the overlay under a still pointer) or off
	// it. Re-ask before the frame that will draw the new position.
	a.syncPointerGate()
	a.win.Invalidate()
}

// Native window movement can invalidate our cached frame independently of a
// state transition. Never let the cached target hide a displaced window.
func (a *shelf) windowGeometryChanged() {
	if a.settingFrame || a.win == nil {
		return
	}
	if actual := a.win.Bounds(); actual == a.desiredFrame() {
		a.frame = actual
		return
	}
	a.frame = mygo.Rectangle{}
	a.syncWindowFrame()
}

// peekSize is the hover shape: the status capsule while the closed panel
// has something to report, the now-playing wings while a track is loaded
// (the notch grows 52pt to each side, barely below), the drop pill while a
// drag is over the notch band, the narrow drag hint while one is in
// flight, otherwise a bulge carrying a chevron. The glyphs sit wholly
// below the hardware notch: nothing drawn inside it is visible.
func (a *shelf) peekSize() (w, h float32) {
	baseW, baseH := float32(max(180, a.notchW)), float32(max(32, a.notchH))
	if a.pinReturning {
		// A pinned card held over the notch wins over the capsule and
		// the wings: the release reads this same zone, so the affordance
		// must always be the drop-pill it acts on (design §4 松开放回).
		return baseW + dropPillExtraW, baseH + dropPillBody
	}
	if a.capsuleUp(time.Now()) {
		// The completion capsule takes the peek shape while the panel is
		// closed: it is what the shell grows into, whether or not a track
		// is loaded.
		return a.fb.capsule(baseW, baseH)
	}
	if a.np != nil && a.np.showsPlayer() {
		return a.wingsSize()
	}
	if a.dragging && !a.ownDrag {
		if a.dropHot {
			return baseW + dropPillExtraW, baseH + dropPillBody
		}
		// Drag hint: the notch's own height, a little wider — visible in
		// the menu bar, never over the windows below it.
		return baseW + dropHintExtraW, baseH
	}
	return baseW + 40, baseH + peekBody
}

// wingsSize is the hover wings' size: the notch plus npWingsW on each
// side, and npWingsBody below it. Wide notches keep their width and gain
// the same wings.
func (a *shelf) wingsSize() (w, h float32) {
	baseW := float32(max(180, a.notchW))
	return baseW + 2*npWingsW, float32(max(32, a.notchH)) + npWingsBody
}

// shellSize is the one interpolation the shell Path and the content layout
// share: hardware notch → peek shape peekW×peekH (peekK) → island →
// sheet (levelK, then openK). The caller passes the drawn values: view()
// animates them per display frame and substitutes final ones in headless
// tests.
func (a *shelf) shellSize(openK, levelK, peekK, peekW, peekH float32) (w, h float32) {
	baseW := float32(max(180, a.notchW))
	baseH := float32(max(32, a.notchH))
	lvlW := float32(islandW) + (sheetW-islandW)*levelK
	lvlH := float32(islandH) + (sheetH-islandH)*levelK
	w0 := baseW + (peekW-baseW)*peekK
	h0 := baseH + (peekH-baseH)*peekK
	return w0 + (lvlW-w0)*openK, h0 + (lvlH-h0)*openK
}

// capsuleFrameW is the window width the completion capsule needs once the
// glow margin is gone: the widest capsule, plus both ear corners, the
// failure shake's reach, and the grow-out spring's overshoot (Bounce 0.28
// widens the notch→capsule delta by about openOvershoot).
func capsuleFrameW() int {
	wide := int(math.Ceil(float64((fbMaxW - 180) * openOvershoot)))
	return fbMaxW + 2*(earR+fbShakeX+wide)
}

// peekPillRect is the on-screen box of the hovered notch capsule, matching
// the shell drawn in view(): the now-playing pill while a track is
// loaded, otherwise the chevron bulge. A press anywhere on it expands the
// shelf — the design has no in-pill controls. While the completion capsule
// or a fold in flight is on the shell the box is the shell actually drawn
// (a fold remnant still takes its own clicks), shifted by the error shake
// so the moving capsule keeps its own hit box.
func (a *shelf) peekPillRect() mygo.Rectangle {
	w, h := a.peekSize()
	if (a.peeking || a.fb.drawn || a.fb.retracting || a.peekProgress > 0.01) && a.fx.w > 0 {
		w, h = a.fx.w, a.fx.h
	}
	x := a.stripCenter() - int(w/2) + int(a.fx.shakeX)
	return mygo.Rectangle{X: x, Y: a.y, Width: int(w + 0.5), Height: int(h + 0.5)}
}

func (a *shelf) overPeekPill(p mygo.Point) bool {
	r := a.peekPillRect()
	return r.Width > 0 && r.Height > 0 &&
		p.X >= r.X && p.X < r.X+r.Width &&
		p.Y >= r.Y && p.Y < r.Y+r.Height
}

// mouseDown is the AppKit mouse-down observer. inside means the event was
// delivered to our window; otherwise it went to another app or the menu bar.
// Every left press also reaches the drag tracker: the 30 ms board poll can
// miss a release between two drags, and the press is the edge that splits
// their serials (see dragTracker.press).
func (a *shelf) mouseDown(inside bool, button int, p mygo.Point) {
	if button == 0 {
		mac.NoteLeftPress()
	}
	if a.quitting {
		return
	}
	// The capsule is drawn over the menu bar's empty centre. WindowServer
	// often gives that press to the menu bar or the app underneath, so the
	// overlay paints a "click to expand" pill that never receives the click.
	// A press on the visible pill expands either way — except a press the
	// hover wings received itself: they have no controls, and their own
	// handler opens the shelf for a press anywhere on them. Only a press
	// inside the drawn shape acts: transparent window margin neither opens
	// the panel nor claims the event for it.
	if (a.peeking || a.fb.drawn || a.fb.retracting || a.peekProgress > 0.01) && !a.visible && a.overPeekPill(p) {
		if inside && a.peeking && a.np != nil && a.np.showsPlayer() {
			return
		}
		a.show(false)
		return
	}
	if inside {
		// Esc and Space are taken only while the panel is open; a press
		// on the collapsed notch band or a closing panel must not hold them.
		if a.visible {
			a.holdKeys()
		}
		a.takeServiceOwnership()
		return
	}
	a.releaseKeys()
	inMenuBar := p.Y >= a.notchY && p.Y <= a.notchY+max(36, a.notchH+4)
	if a.visible && !a.dragging && !inMenuBar {
		a.hide(hidePassive)
	}
}

// windowLevel is the overlay's one window-level policy, applied by
// syncMousePassThrough on every state change: a file drag sits above the
// menu bar so its strip cannot steal the drop; keyboard focus (clipboard
// search) must drop below kCGDockWindowLevel or the input method's
// candidate panel — drawn at 20 and anchored inside the opaque panel —
// stays completely hidden behind it (marked text still composes inline,
// which is why the bug reads as "IME types but no candidates").
func (a *shelf) windowLevel() int {
	switch {
	case a.dragging || a.pointerDrag:
		return mac.PopUpMenuWindowLevel
	case a.focusMode:
		return mac.SearchKeyboardLevel
	default:
		return mac.StatusWindowLevel
	}
}

// While idle the overlay ignores the mouse altogether, so clicks under and
// beside the notch reach whatever is below. An in-flight drag takes the
// whole window: file drags to the top of the notch must hit a window, or
// WindowServer treats Y=0 as the Spaces edge (pointerDrag flips this on
// the drag's first movement, long before the top). Every other state takes
// the mouse only over the shell the last frame drew: the window is a
// rectangle (the shell plus its transparent ear margins) sitting above the
// menu bar, and WindowServer routes a press to a window before any view is
// hit-tested, so a window-wide press would eat the menu-bar icons beside
// the shell. A closing panel follows the drawn shell (fx.openK), not the
// tick's eased progress: the moment the shell has folded back to the peek
// shape the window lets clicks through again.
func (a *shelf) syncMousePassThrough() {
	a.syncWindowFrame()
	setGatePolling(anyOverlayWanted())
	accept := a.overlayAcceptsMouse()
	a.ignoringMouse = !accept
	if a.win == nil {
		return
	}
	h := a.winH
	mac.SetOverlayAcceptsMouse(h, accept)
	mac.SetWindowLevel(h, a.windowLevel())
}

// overlayWanted reports whether the current state needs the overlay to take
// the mouse at all: hovering, opening, open, folding, or a capture capsule.
func (a *shelf) overlayWanted() bool {
	return a.visible || a.fx.openK > 0.02 || a.dragging || a.pointerDrag ||
		a.peeking || a.peekProgress > 0.01 || a.fb.drawn || a.fb.retracting
}

// overlayAcceptsMouse answers whether the overlay takes the mouse at the
// current pointer, reading the pointer only when the answer depends on it:
// a headless surface has no window to gate, a drag owns the whole frame
// (the drop and the Spaces edge guard land on the window, not on the drawn
// shape), and an idle overlay ignores the mouse everywhere.
func (a *shelf) overlayAcceptsMouse() bool {
	wanted := a.overlayWanted()
	if !wanted || a.winH == 0 || a.dragging || a.pointerDrag {
		return wanted
	}
	p := cursorPoint()
	if a.overShell(p) {
		return true
	}
	// The drawn shell is not the only shape a press can belong to. An
	// opening island, the sheet springing out of the island and a capsule
	// growing out of the notch reach their box a frame or two later, and
	// the frame already covers it (desiredFrame follows the level), while a
	// still pointer produces no move event to re-ask with. So the shape the
	// state settles at counts too: a press that lands where the shell is
	// about to be must reach Shelf, not the window underneath.
	tw, th, tl := a.settledShell()
	return a.overShellShape(p, tw, th, tl, a.fx.shakeX)
}

// settledShell is the shell the current state is heading for: the level's
// box while the panel is open, the peek shape (wings, drop pill, completion
// capsule, chevron) while it is closed. levelK is the drawn corner's level
// progress at rest, so the hit shape rounds exactly like the settled frame.
func (a *shelf) settledShell() (w, h, levelK float32) {
	var openK, peekK float32
	if a.visible {
		openK = 1
	}
	if a.level >= 2 {
		levelK = 1
	}
	if a.peeking || a.peekProgress > 0.01 || a.fb.drawn || a.fb.retracting {
		peekK = 1
	}
	pw, ph := a.peekSize()
	w, h = a.shellSize(openK, levelK, peekK, pw, ph)
	return w, h, levelK
}

// The pointer gate's sources are installed once per process and fan out to
// every overlay that follows the pointer: Shelf owns one, and the smoke
// harness builds one per test. A watch bound to the first shelf left every
// other window's flag stale — its gate could only change on the next frame,
// never on a move.
var (
	pointerWatched bool
	pointerShelves []*shelf
)

// gatePollInterval is how often the gate re-reads the pointer while a shell
// is on screen. Move events alone are not enough: macOS coalesces
// mouse-moved events and an inactive overlay's event may be delivered tens
// of milliseconds late, so a press can arrive after the pointer reached the
// shell but before the monitor ran — WindowServer would hand that press to
// the window underneath. Polling bounds that window to one interval.
const gatePollInterval = 10 * time.Millisecond

// gatePolling mirrors "some overlay is on screen", set on the main thread so
// the poller never reads a shelf off it (the fields are main-thread only).
var gatePolling atomic.Bool

// gateWake carries "polling started" to the poller, which sleeps otherwise.
var gateWake = make(chan struct{}, 1)

// watchPointer registers the shelf with the pointer gate's sources. A
// settled panel draws no frames, so nothing else would notice the pointer
// leaving the drawn shell — and WindowServer decides which window a press
// reaches from ignoresMouseEvents, which only the pointer position keeps
// current. Both sources are mouse-only, so neither needs Accessibility.
// They live as long as the process.
func (a *shelf) watchPointer() {
	if a.watchingPointer {
		return
	}
	a.watchingPointer = true
	pointerShelves = append(pointerShelves, a)
	if pointerWatched {
		return
	}
	pointerWatched = true
	mac.ObserveMouseMoved(func() {
		for _, s := range pointerShelves {
			s.syncPointerGate()
		}
	})
	go pollPointerGate()
}

// pollPointerGate samples the pointer while a shell is on screen, so the
// flag is current without waiting for an event. The sample itself runs on
// the main thread: every field the gate reads belongs to it.
func pollPointerGate() {
	for range gateWake {
		for gatePolling.Load() {
			time.Sleep(gatePollInterval)
			mygo.RunOnMain(func() {
				for _, s := range pointerShelves {
					s.syncPointerGate()
				}
			})
		}
	}
}

// anyOverlayWanted reports whether any registered overlay is on screen, so
// one idle shelf cannot stop the poller another shelf relies on. Main
// thread only.
func anyOverlayWanted() bool {
	for _, s := range pointerShelves {
		if s.overlayWanted() {
			return true
		}
	}
	return false
}

// setGatePolling tells the poller that a shell is (or is no longer) on
// screen. On the main thread only.
func setGatePolling(on bool) {
	if gatePolling.Swap(on) == on {
		return
	}
	if on {
		select {
		case gateWake <- struct{}{}:
		default:
		}
	}
}

// syncPointerGate re-asks the shell-shaped hit test at the current pointer
// and applies the answer, touching the native flag only when it changed. It
// is both the pointer watch's entry and the refresh every change of the
// shell's shape or position goes through (window frame, show, level, page,
// fold, capsule, drag end), so no press waits for a move event.
func (a *shelf) syncPointerGate() {
	if a.win != nil && a.win.IsDestroyed() {
		return
	}
	accept := a.overlayAcceptsMouse()
	if accept == !a.ignoringMouse {
		return
	}
	a.ignoringMouse = !accept
	if a.win == nil {
		return // headless surface: the flag is all there is
	}
	mac.SetOverlayAcceptsMouse(a.winH, accept)
}

// overShell reports whether p is on the shell the last frame drew — the
// black body plus the two ear corners its top edge reaches past its
// flanks, and not the corners its bottom rounds off. Screen coordinates,
// top-left origin. The shell is centred on stripCenter and hangs from
// a.y + sheetTop() in the window, exactly as view() draws it.
func (a *shelf) overShell(p mygo.Point) bool {
	return a.overShellShape(p, a.fx.w, a.fx.h, a.fx.levelK, a.fx.shakeX)
}

// overShellShape is overShell for an explicit shell: w×h, its bottom
// corner rounded by the island/sheet radius at levelK, shifted by shakeX.
func (a *shelf) overShellShape(p mygo.Point, w, h, levelK, shakeX float32) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	top := float64(a.y + a.sheetTop())
	y := float64(p.Y) - top
	if y < 0 || y >= float64(h) {
		return false
	}
	cx := float64(a.stripCenter()) + float64(shakeX)
	x0, x1 := cx-float64(w)/2, cx+float64(w)/2
	left, right := x0, x1
	// inset is how far the path's corner cuts in d points from the edge it
	// starts on, for radius r: 0 at the flank, r at the outer edge. Both
	// top ears and bottom corners are quadratics with their control point
	// in the corner, which reduce to exactly this.
	inset := func(d, r float64) float64 {
		if r <= 0 || d >= r {
			return 0
		}
		return r * sq(1-math.Sqrt(d/r))
	}
	if ear := float64(earR); y < ear {
		// The ear: the top edge runs earR past the flank at y == 0 and
		// meets the flank at earR below.
		left, right = x0-inset(y, ear), x1+inset(y, ear)
	}
	corner := float64(islandRadius) + float64(sheetRadius-islandRadius)*float64(levelK)
	if botR := math.Min(float64(h)/2, corner); y > float64(h)-botR {
		cut := inset(float64(h)-y, botR)
		left, right = x0+cut, x1-cut
	}
	x := float64(p.X)
	return x >= left && x < right
}

// sq is the square of v.
func sq(v float64) float64 { return v * v }

// setPeek animates the notch shell between its idle hardware shape and the
// peeked "I'm here" pill, and updates event pass-through to match.
func (a *shelf) setPeek(peek bool) {
	if a.peeking == peek {
		return
	}
	a.peeking = peek
	a.peekFrom, a.peekAnimStart = a.peekProgress, time.Now()
	a.syncMousePassThrough()
	if a.win != nil {
		a.win.Invalidate()
	}
}

// panelH is the sheet's fixed height; both levels' sizes come from the
// layout constants, never from page content.
func (a *shelf) panelH() int { return sheetH }

func (a *shelf) show(edge bool) {
	if a.visible {
		return
	}
	if a.win != nil {
		a.placeAt(mygo.Screen.CursorScreenPoint())
	}
	if a.store != nil {
		a.store.Refresh()
	}
	// Opening after a close in flight keeps the old view until the fade is
	// over; cancel that pending reset so the fresh opening cannot be
	// clobbered a frame later.
	a.closeAt = time.Time{}
	// A drag or the clipboard hotkey opens straight on the sheet; a plain
	// toggle opens the island.
	level := 1
	if edge || a.focusMode || (a.dragging && !a.ownDrag) || a.drop.on || a.ocrSheet {
		level = 2
	}
	a.level = level
	a.jumpLevel = true
	a.visible, a.edgeOpened, a.received = true, edge, false
	a.dropZoneSnap(edge)
	a.fx.openMoving = true
	a.animationFrom, a.animationStart = a.progress, time.Now()
	prevFrame := a.frame
	a.syncMousePassThrough()
	a.syncDragGuard()
	if a.win != nil {
		a.win.SetOpacity(1)
		a.win.ShowInactive()
		// ShowInactive 不夺焦，面板收不到按键，只能临时注册全局 Esc 与空格。
		a.holdKeys()
		if a.frame == prevFrame {
			a.win.Invalidate()
		}
	}
}

// Global hotkey seams: holdKeys/releaseKeys register and unregister the
// panel's Esc and Space, which needs the app running; tests stub them to
// check who owns the keys.
var (
	registerKey   = mygo.GlobalShortcut.Register
	unregisterKey = mygo.GlobalShortcut.Unregister
)

// holdKeys takes Esc (close Quick Look, then collapse) and Space (Quick
// Look the selection) while the shelf is in use; releaseKeys returns them
// as soon as the user clicks in another app.
func (a *shelf) holdKeys() {
	if !a.keysAreOurs() {
		// 设置窗口在前台时它自己处理 Esc 与空格（Esc/⌘W 关窗、控件收
		// 空格），全局注册会把按键从它手里抢走；剪贴板搜索模式的窗口同
		// 理。两条路径都先确保已注册的按键已经归还。
		a.releaseKeys()
		return
	}
	if !a.escHeld {
		if err := registerKey("Escape", a.escape); err != nil {
			a.report(fmt.Errorf("Esc 被其他应用占用，请用快捷键或收起按钮：%w", err))
		} else {
			a.escHeld = true
		}
	}
	if !a.spaceHeld {
		// Without Space, Quick Look stays available from the context menu.
		a.spaceHeld = registerKey("Space", a.toggleQuickLook) == nil
	}
	a.syncCopyKey()
}

// ShowInactive does not give the overlay keyboard focus. Hold ⌘C like
// Space only while a selected tray picture can actually be copied.
const copyKey = "Cmd+C"

// wantsCopyKey reports whether the tray would act on ⌘C right now: the
// panel is open with its keys, on the tray page, and exactly one selected
// item is a picture. Pure; unit-tested.
func (a *shelf) wantsCopyKey() bool {
	if !a.visible || !a.keysAreOurs() || a.tab != pageTray || a.selection.count() != 1 || a.store == nil {
		return false
	}
	for _, item := range a.store.Items {
		if a.selection.has(item.ID) {
			return !item.OffTray && !a.isLeaving(item) && convertible(item)
		}
	}
	return false
}

// syncCopyKey follows selection changes without reclaiming keys released
// by an outside click. Headless views have no native key to acquire.
func (a *shelf) syncCopyKey() {
	if a.win == nil {
		return // headless test shelf: no app to register with
	}
	switch want := a.wantsCopyKey() && (a.escHeld || a.spaceHeld); {
	case want && !a.cmdCHeld:
		if registerKey(copyKey, a.copyTrayImage) == nil {
			a.cmdCHeld = true
		}
	case !want && a.cmdCHeld:
		unregisterKey(copyKey)
		a.cmdCHeld = false
	}
}

// keysAreOurs reports whether the shelf may hold the global Esc and Space:
// never while the Settings or 重新命名 window is key, and never while the
// panel itself has the keyboard for clipboard search. Pure; unit-tested.
func (a *shelf) keysAreOurs() bool {
	if a.settingsWin != nil {
		return false
	}
	if a.rn != nil && a.rn.win != nil {
		return false
	}
	return !a.focusMode
}

func (a *shelf) releaseKeys() {
	if a.escHeld {
		unregisterKey("Escape")
		a.escHeld = false
	}
	if a.spaceHeld {
		unregisterKey("Space")
		a.spaceHeld = false
	}
	if a.cmdCHeld {
		unregisterKey(copyKey)
		a.cmdCHeld = false
	}
}

// escape closes Quick Look first, like Finder, then dismisses the
// screen-recording card or clears the clipboard
// search while it holds text; a sheet backs out to the island and the
// island folds away.
func (a *shelf) escape() {
	if mac.QuickLookVisible() {
		mac.CloseQuickLook()
		return
	}
	if a.ocrSheet {
		a.hide(hideExplicit)
		return
	}
	if a.tab == pageClipboard && a.search != "" {
		a.search = ""
		if a.win != nil {
			a.win.Invalidate()
		}
		return
	}
	if a.tab == pageHome {
		if p := a.homePageOf(); p != nil && (p.editing || p.picking) {
			if p.picking {
				p.picking = false
			} else {
				p.editing, p.press = false, nil
			}
			if a.win != nil {
				a.win.Invalidate()
			}
			return
		}
	}
	if a.tab == pageTools {
		if p := a.toolsPage(); p != nil && (p.editing || p.picking) {
			// Esc in the 「+」 catalogue only backs out to the edit grid —
			// same as its 完成; outside it, Esc ends editing. Neither
			// hides the panel.
			if p.picking {
				p.picking = false
			} else {
				p.closeDragPreview()
				p.editing, p.press = false, nil
			}
			if a.win != nil {
				a.win.Invalidate()
			}
			return
		}
	}
	// A sheet backs out to the island first; the island's Esc collapses.
	// The keyboard search flow (⌃⌥V) has no island state to return to, so
	// it closes outright.
	if a.currentLevel() == 2 && !a.focusMode {
		a.setLevel(1)
		return
	}
	a.hide(hideExplicit)
}

// hide collapses the shelf. exit decides whether the remembered app gets
// the keyboard back (see shouldRestoreApp); the remembered pid is always
// cleared so a later explicit hide cannot reactivate it a second time.
// The view state (page, search, keyboard mode) survives until the 120 ms
// content fade ends, so the closing panel fades the page it was showing
// instead of flashing the default one; finishClose retires it.
func (a *shelf) hide(exit hideExit) {
	if !a.visible && !a.peeking {
		return
	}
	a.handoffStatus(time.Now())
	// Any hide — manual or the flash's own timer — ends service ownership
	// so a queued timer can never reclaim a panel the user reopened.
	a.takeServiceOwnership()
	restore := a.shouldRestoreApp(exit)
	prev := a.prevAppPID
	a.visible, a.edgeOpened, a.peeking = false, false, false
	a.closeAt = time.Now()
	// 收起也复位层级进度：二级收起后 fx.levelK 若停在 1，下一次打开一级
	// 的第一帧仍会按二级壳开窗（451pt），在菜单栏图标上方压一层透明窗。
	// 绘制通道由 jumpLevel 在下一帧直接落位，这里同步帧几何读到的值。
	a.fx.levelK = 0
	a.jumpLevel = true
	if a.usage != nil { // 隐藏即取消 AI 用量在途取数和扫描
		a.usage.stopWork()
	}
	a.animationFrom, a.animationStart = a.progress, time.Now()
	a.peekFrom, a.peekAnimStart = a.peekProgress, time.Now()
	a.prevAppPID = 0
	a.releaseKeys()
	// 当前拖拽不再次触发；松开鼠标或出现新会话后解除。
	a.suppressed = true
	a.dropHot = false
	a.syncMousePassThrough()
	if restore {
		a.activatePreviousApp(prev)
	}
	if a.win != nil {
		a.win.Invalidate()
	}
}

// finishClose retires the view state after the content fade: the next
// opening starts on the island's default face with no search or edit.
func (a *shelf) finishClose() {
	a.closeAt = time.Time{}
	a.focusMode, a.tab, a.tabFrom, a.search, a.reveal = false, pageTray, pageTray, "", ""
	a.tabAt = time.Time{}
	a.tabOutK, a.tabOutShift = 1, 0
	a.warming = ""
	a.level = 1
	// 收起即放弃权限说明卡：面板再开时它要么还在等授权（重新触发再出），
	// 要么授权已完成（预检通过直接框选），都不该回到一张旧卡。
	a.ocrSheet = false
	a.resetToolsEdit() // 工具页的编辑与目录不跨收起保留
	a.resetHomeEdit()  // 主页的编辑与目录不跨收起保留
	if p := a.homePageOf(); p != nil {
		p.volLoaded = false // the output device may have changed while closed
	}
	a.selection.clear()
}

// animate updates the shell drawing; idle/peek retain one native surface.
func (a *shelf) animate(now time.Time) {
	finished := false
	if !a.animationStart.IsZero() {
		k := min(float32(now.Sub(a.animationStart))/float32(panelAnimation), 1)
		target := float32(0)
		if a.visible {
			target = 1
		}
		a.progress = a.animationFrom + (target-a.animationFrom)*ui.EaseOut(k)
		if k >= 1 {
			a.animationStart = time.Time{}
			finished = true
		}
	}
	if !a.peekAnimStart.IsZero() {
		k := min(float32(now.Sub(a.peekAnimStart))/float32(peekAnimation), 1)
		target := float32(0)
		if a.peeking || a.visible {
			target = 1
		}
		a.peekProgress = a.peekFrom + (target-a.peekFrom)*ui.EaseOut(k)
		if k >= 1 {
			a.peekAnimStart = time.Time{}
			finished = true
		}
	}
	if a.win == nil {
		return
	}
	// 动画途中由 zone.AnimateTo 以显示器帧率自驱动重绘；结束时补一次穿透状态同步。
	if !a.animationStart.IsZero() || !a.peekAnimStart.IsZero() || finished {
		if finished {
			a.syncMousePassThrough()
		}
		if !a.fx.openMoving && !a.fx.sizeMoving {
			a.win.Invalidate()
		}
	}
}

// stripCenter is shared by the drop surface and its protected approach region.
func (a *shelf) stripCenter() int {
	if a.notchW <= 0 {
		return a.x + sheetW/2
	}
	return a.notchX + a.notchW/2
}

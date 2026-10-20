//go:build darwin

package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo"

	"shelf/internal/mac"
)

// While idle, the resident overlay must let clicks through everywhere —
// under the notch where the hover player would grow and beside it in the
// menu bar — and while the player shows it must take them. Asked of
// WindowServer itself: a view refusing hitTest: still swallows the click,
// which is how the idle strip once blocked a 400×172 area under the notch.
// Run with SHELF_BASKET_SMOKE=1.
func TestIdleOverlayLetsClicksThrough(t *testing.T) {
	a := redesignShelf(t)
	onMain(func() {
		a.settings.IslandNowPlaying = true
		fakeTrack(a, true) // the strip reserves the hover player's size
		a.modules = []module{a.np}
		a.place(mygo.Screen.CursorScreenPoint())
		a.peeking, a.peekProgress, a.visible, a.progress = false, 0, false, 0
		a.syncMousePassThrough()
		a.win.Invalidate()
	})
	time.Sleep(400 * time.Millisecond)
	check := func(wantShelf bool, state string) {
		onMain(func() {
			shelf := mac.WindowNumber(a.win.NativeHandle())
			f := a.desiredFrame()
			screenH := float64(a.dispBounds.Y + a.dispBounds.Height)
			cx := float64(a.stripCenter())
			sampleW := f.Width
			if wantShelf {
				// The ear margins are transparent below the top arc.
				// Hit-test the visible player; idle still checks the
				// entire native strip, margins included.
				sampleW = int(a.fx.w)
			}
			for _, y := range []float64{10, float64(a.notchH) + 20, float64(f.Height) - 10} {
				for _, dx := range []float64{0, -float64(sampleW)/2 + 10, float64(sampleW)/2 - 10} {
					got := mac.WindowAtPoint(cx+dx, float64(a.dispBounds.Y)+y, screenH) == shelf
					if got != wantShelf {
						t.Errorf("%s: click at strip (%+.0f, %.0f) reached the shelf = %v, want %v (strip %+v)", state, dx, y, got, wantShelf, f)
					}
				}
			}
		})
	}
	check(false, "idle")
	onMain(func() {
		a.setPeek(true)
	})
	time.Sleep(200 * time.Millisecond)
	check(true, "hover player")
	peekRestore(t, a)
}

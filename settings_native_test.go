//go:build darwin

package main

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"shelf/internal/mac"
	"shelf/internal/settings"
)

// TestSettingsWindowAnswersClicksAndScroll is the regression test for the
// class-level swizzle leak: the notch overlay's native patches (hitTest:
// band rule on the surface view, constrainFrameRect:toScreen: on the
// window) used to be installed on the shared mygo classes, so every window
// inherited them. The Settings window then hit-tested only inside the
// notch band — its controls and scroll container were dead. The fix grafts
// per-instance subclasses instead; this test proves the notch keeps its
// patches while a real Settings window answers clicks everywhere and
// scrolls its content. Run with SHELF_BASKET_SMOKE=1 (the shared real-app
// harness); uses a temporary data dir and never the user's Shelf data.
func TestSettingsWindowAnswersClicksAndScroll(t *testing.T) {
	if os.Getenv("SHELF_BASKET_SMOKE") != "1" {
		t.Skip("set SHELF_BASKET_SMOKE=1 to run the native window smoke test")
	}
	a := smokeShelf
	// A writable data dir with the real defaults, so the window shows what a
	// user sees (the zero value would render every switch off).
	a.dataDir = t.TempDir()
	a.settings, _ = settings.Load(a.dataDir)
	onMain(func() {
		a.newNotchWindow()
		a.place(mygo.Screen.CursorScreenPoint())
		a.syncMousePassThrough() // idle: only the notch band accepts events
		a.win.ShowInactive()
		a.openSettings()
	})
	if a.win == nil || a.settingsWin == nil {
		t.Fatal("expected both the notch overlay and the Settings window")
	}
	defer onMain(func() {
		if a.settingsWin != nil {
			a.settingsWin.Close()
		}
	})
	// Give AppKit a beat to order the windows in and run the first frames.
	time.Sleep(300 * time.Millisecond)

	notch := a.win.NativeHandle()
	settings := a.settingsWin.NativeHandle()
	// Class hygiene: the patches gate on window/view pointers inside one
	// shared-class method — no subclassing, no reclassing, so every window
	// keeps its stock mygo class (a reclass would break AppKit's KVO
	// isa-swizzling on the titlebar view).
	t.Logf("notch class=%s settings class=%s", mac.WindowClassName(notch), mac.WindowClassName(settings))
	if cls := mac.WindowClassName(settings); cls != "MyGoWindow" {
		t.Fatalf("Settings window class %q, want plain MyGoWindow", cls)
	}
	if cls := mac.WindowClassName(notch); cls != "MyGoPanel" {
		t.Fatalf("notch window class %q, want plain MyGoPanel", cls)
	}

	// Hit-test, while the notch is idle: the Settings surface answers
	// across its content — before the fix only a notch-band-high strip did.
	onMain(func() {
		for _, p := range [][2]float64{{230, 60}, {230, 260}, {230, 450}, {60, 300}} {
			if !mac.SurfaceHitTest(settings, p[0], p[1]) {
				t.Errorf("Settings hitTest(%v,%v) rejected: the notch band leaked into this window", p[0], p[1])
			}
		}
	})
	if t.Failed() {
		return
	}

	// Scroll: a wheel event on the Settings surface must move the tracked
	// scroll state — the window is 520pt tall and its content is taller.
	var before, after float32
	onMain(func() {
		a.settingsWin.Invalidate()
		before = a.settingsScroll.Y
		if !mac.SurfaceScroll(settings, 230, 300, 240) {
			t.Error("SurfaceScroll delivered no event")
		}
	})
	time.Sleep(150 * time.Millisecond)
	onMain(func() {
		a.settingsWin.Invalidate()
		after = a.settingsScroll.Y
	})
	if after == before {
		t.Fatalf("scroll event moved nothing: ScrollState.Y stayed %v (MaxY=%v)", after, a.settingsScroll.MaxY)
	}
	t.Logf("scroll moved %v → %v (max %v)", before, after, a.settingsScroll.MaxY)

	// Screenshot the scrolled window for eyeballing; the id pins
	// `screencapture -l` to this window alone.
	if n := mac.WindowNumber(settings); n != 0 {
		out := os.TempDir() + "/shelf-settings-scrolled.png"
		if err := exec.Command("screencapture", "-l", strconv.Itoa(n), "-x", out).Run(); err == nil {
			t.Logf("scrolled Settings screenshot: %s", out)
		}
	}

	// The basket is the other mygo panel: it must keep the stock surface
	// class (the graft is per-instance, and only the notch asks for it).
	onMain(func() {
		a.showBasket(mygo.Point{X: 200, Y: 300})
	})
	defer onMain(a.closeBasket)
	if a.basket.win == nil {
		t.Fatal("basket window was not created")
	}
	if cls := mac.WindowClassName(a.basket.win.NativeHandle()); cls != "MyGoPanel" {
		t.Errorf("basket window class %q carries a graft it never asked for", cls)
	}
	if !mac.SurfaceHitTest(a.basket.win.NativeHandle(), 110, 80) {
		t.Error("basket surface rejected a centered hit")
	}

	// And the notch itself still gates: outside its idle band it stays
	// click-through, inside the band it stays solid.
	onMain(func() {
		strip := a.win.Bounds()
		if mac.SurfaceHitTest(notch, 4, float64(a.notchH)+4) {
			t.Error("idle notch outside the drawn band must pass clicks through")
		}
		if a.notchW > 0 && !mac.SurfaceHitTest(notch, float64(strip.Width)/2, 8) {
			t.Error("idle notch band must keep accepting hits")
		}
	})
}

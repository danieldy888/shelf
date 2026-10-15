//go:build darwin

package mac

import "testing"

// ConfigureNotchWindow(0) is a documented no-op and must not touch AppKit.
func TestConfigureNotchWindowNilHandle(t *testing.T) {
	ConfigureNotchWindow(0)
}

// A displayID that matches no screen returns 0 without panicking; on any
// screen the inset is never negative.
func TestNotchTopInsetUnknownDisplay(t *testing.T) {
	if got := NotchTopInset(0); got < 0 {
		t.Fatalf("NotchTopInset(0) = %d, want >= 0", got)
	}
}

// SetWindowIgnoresMouseEvents(0, …) is a documented no-op in both directions
// and must not touch AppKit.
func TestSetWindowIgnoresMouseEventsNilHandle(t *testing.T) {
	SetWindowIgnoresMouseEvents(0, true)
	SetWindowIgnoresMouseEvents(0, false)
}

// A displayID that matches no screen must report w=0/h=0/hasNotch=false:
// returning a 180×32 box at (0,0) would invent an interactive zone in the
// top-left corner of the primary screen.
func TestNotchRectUnknownDisplay(t *testing.T) {
	x, y, w, h, hasNotch := NotchRect(0)
	if hasNotch {
		t.Fatalf("NotchRect(0) hasNotch = true, want false for unknown display")
	}
	if w != 0 || h != 0 || x != 0 || y != 0 {
		t.Fatalf("NotchRect(0) = (%d,%d) %dx%d, want zero rect for unknown display", x, y, w, h)
	}
}

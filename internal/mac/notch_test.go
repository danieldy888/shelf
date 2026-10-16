//go:build darwin

package mac

import (
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

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

// ConfigureNotchWindow(0) already covers the nil-handle path. A real
// NSPanel must sit at the top of the screen frame, not the visibleFrame
// below the menu bar — that gap is the floating peek pill and the
// Mission Control edge trigger.
func TestNotchOverlaySitsAtScreenTop(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	var initPanel func(id, objc.SEL, NSRect, uint, uint, bool) id
	purego.RegisterFunc(&initPanel, msgSendAddr)
	var panel id
	withPool(func() {
		send(class("NSApplication"), "sharedApplication")
		content := NSRect{Size: NSSize{Width: 280, Height: 72}}
		panel = initPanel(send(class("NSPanel"), "alloc"), sel("initWithContentRect:styleMask:backing:defer:"),
			content, nsWindowStyleMaskNonactivatingPanel, 2, false)
		if panel == 0 {
			t.Fatal("NSPanel alloc/init failed")
		}
		send(panel, "setReleasedWhenClosed:", 0)
		ConfigureNotchWindow(uintptr(panel))
		SetWindowFrame(uintptr(panel), 200, 0, 280, 72)
		send(panel, "orderFront:", 0)
	})
	defer withPool(func() { send(panel, "orderOut:", 0); send(panel, "close") })
	var frame NSRect
	var screenH float64
	var behavior uint
	withPool(func() {
		frame = msgRect(panel, sel("frame"))
		screenH = primaryScreenHeight()
		behavior = CollectionBehavior(uintptr(panel))
	})
	top := screenH - frame.Origin.Y - frame.Size.Height
	if int(top+0.5) != 0 {
		t.Fatalf("overlay top is %.1f pt below the screen frame (want 0); AppKit still clamped it under the menu bar", top)
	}
	if int(frame.Size.Width+0.5) != 280 || int(frame.Size.Height+0.5) != 72 {
		t.Fatalf("overlay size = %.0fx%.0f, want 280x72", frame.Size.Width, frame.Size.Height)
	}
	if behavior&collectionStationary == 0 {
		t.Fatalf("overlay collectionBehavior=0x%x missing stationary; Mission Control would pull it away", behavior)
	}
}

func TestSetWindowFrameNilHandle(t *testing.T) {
	SetWindowFrame(0, 0, 0, 280, 72)
}

// Clearing click-through must actually drop ignoresMouseEvents. The peek
// pill used to paint while this flag stayed set, so clicks fell through.
func TestEnablingMouseEventsClearsClickThrough(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	var initPanel func(id, objc.SEL, NSRect, uint, uint, bool) id
	purego.RegisterFunc(&initPanel, msgSendAddr)
	var panel id
	withPool(func() {
		send(class("NSApplication"), "sharedApplication")
		content := NSRect{Size: NSSize{Width: 280, Height: 72}}
		panel = initPanel(send(class("NSPanel"), "alloc"), sel("initWithContentRect:styleMask:backing:defer:"),
			content, nsWindowStyleMaskNonactivatingPanel, 2, false)
		if panel == 0 {
			t.Fatal("NSPanel alloc/init failed")
		}
		send(panel, "setReleasedWhenClosed:", 0)
		ConfigureNotchWindow(uintptr(panel))
		SetWindowIgnoresMouseEvents(uintptr(panel), true)
		if send(panel, "ignoresMouseEvents") == 0 {
			t.Fatal("SetWindowIgnoresMouseEvents(true) did not enable click-through")
		}
		SetWindowIgnoresMouseEvents(uintptr(panel), false)
		if send(panel, "ignoresMouseEvents") != 0 {
			t.Fatal("clearing click-through left ignoresMouseEvents set")
		}
	})
	withPool(func() { send(panel, "orderOut:", 0); send(panel, "close") })
}

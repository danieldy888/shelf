//go:build darwin

package mac

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// AppKit refuses to create NSPanel off the main thread. go test runs each
// Test* on a new goroutine, so keep the main OS thread draining this queue.
var mainCalls = make(chan func())

func TestMain(m *testing.M) {
	runtime.LockOSThread()
	go func() { os.Exit(m.Run()) }()
	for fn := range mainCalls {
		fn()
	}
}

func onMain(t *testing.T, fn func() error) {
	t.Helper()
	done := make(chan error, 1)
	mainCalls <- func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("%v", r)
			}
		}()
		done <- fn()
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

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
	onMain(t, func() error {
		if err := Init(); err != nil {
			return err
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
				return
			}
			send(panel, "setReleasedWhenClosed:", 0)
			ConfigureNotchWindow(uintptr(panel))
			SetWindowFrame(uintptr(panel), 200, 0, 280, 72)
			send(panel, "orderFront:", 0)
		})
		if panel == 0 {
			return fmt.Errorf("NSPanel alloc/init failed")
		}
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
			return fmt.Errorf("overlay top is %.1f pt below the screen frame (want 0); AppKit still clamped it under the menu bar", top)
		}
		if int(frame.Size.Width+0.5) != 280 || int(frame.Size.Height+0.5) != 72 {
			return fmt.Errorf("overlay size = %.0fx%.0f, want 280x72", frame.Size.Width, frame.Size.Height)
		}
		if behavior&collectionStationary == 0 {
			return fmt.Errorf("overlay collectionBehavior=0x%x missing stationary; Mission Control would pull it away", behavior)
		}
		return nil
	})
}

func TestSetWindowFrameNilHandle(t *testing.T) {
	SetWindowFrame(0, 0, 0, 280, 72)
}

// Clearing click-through must actually drop ignoresMouseEvents. The peek
// pill used to paint while this flag stayed set, so clicks fell through.
func TestEnablingMouseEventsClearsClickThrough(t *testing.T) {
	onMain(t, func() error {
		if err := Init(); err != nil {
			return err
		}
		var initPanel func(id, objc.SEL, NSRect, uint, uint, bool) id
		purego.RegisterFunc(&initPanel, msgSendAddr)
		var panel id
		var enabled, cleared bool
		withPool(func() {
			send(class("NSApplication"), "sharedApplication")
			content := NSRect{Size: NSSize{Width: 280, Height: 72}}
			panel = initPanel(send(class("NSPanel"), "alloc"), sel("initWithContentRect:styleMask:backing:defer:"),
				content, nsWindowStyleMaskNonactivatingPanel, 2, false)
			if panel == 0 {
				return
			}
			send(panel, "setReleasedWhenClosed:", 0)
			ConfigureNotchWindow(uintptr(panel))
			SetWindowIgnoresMouseEvents(uintptr(panel), true)
			enabled = send(panel, "ignoresMouseEvents") != 0
			SetWindowIgnoresMouseEvents(uintptr(panel), false)
			cleared = send(panel, "ignoresMouseEvents") == 0
		})
		if panel == 0 {
			return fmt.Errorf("NSPanel alloc/init failed")
		}
		withPool(func() { send(panel, "orderOut:", 0); send(panel, "close") })
		if !enabled {
			return fmt.Errorf("SetWindowIgnoresMouseEvents(true) did not enable click-through")
		}
		if !cleared {
			return fmt.Errorf("clearing click-through left ignoresMouseEvents set")
		}
		return nil
	})
}

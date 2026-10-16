//go:build darwin

package main

// The ⌃⌥V flow on a real window: openClipboardSearch activates the app so
// the notch window becomes the key window and the search field takes the
// keyboard; hiding hands the keyboard back to the app the user came from.
// Headless, SHELF_BASKET_SMOKE=1 builds the real app loop (see
// basket_test.go); SHELF_CLIP_HOLD=1 keeps the panel up so an external
// `screencapture -l <id>` can shoot the live window.

import (
	"os"
	"strconv"
	"testing"
	"time"

	"shelf/internal/mac"
)

func TestSearchOpenFocusesWindow(t *testing.T) {
	if os.Getenv("SHELF_BASKET_SMOKE") != "1" {
		t.Skip("set SHELF_BASKET_SMOKE=1 to run the native window smoke test")
	}
	a := smokeShelf
	if a.win == nil {
		onMain(func() { a.newNotchWindow() })
	}
	if a.win == nil {
		t.Fatal("no notch window")
	}
	prev := 0
	onMain(func() { prev = mac.FrontmostPID() })
	t.Logf("frontmost before open: pid=%d", prev)
	if prev == mac.OwnPID() {
		t.Skip("Shelf is already frontmost; run the test from another app's session")
	}
	// Seed a couple of captures so the clipboard tab renders rows.
	onMain(func() {
		_ = a.store.AddClipText("seeded from Terminal", "com.apple.Terminal")
		_ = a.store.AddClipText("second capture", "com.apple.Safari")
		_ = a.store.SetPinned(a.store.Items[0].ID, true)
	})
	onMain(func() { a.openClipboardSearch() })
	// Activation is delivered through the WindowServer; poll briefly. The
	// harness has no monitoring ticker, so the open animation is stepped
	// by hand — without it progress stays 0 and the shell keeps the pill.
	key := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		onMain(func() {
			a.animate(time.Now())
			key = mac.IsKeyWindow(a.win.NativeHandle())
		})
		if key {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("keyWindow=%v isKeyWindow=%v visible=%v focusMode=%v tab=%d prevPID=%d",
		key, a.win.IsFocused(), a.win.IsVisible(), a.focusMode, a.tab, a.prevAppPID)
	if !a.focusMode || a.tab != tabClipboard || a.search != "" {
		t.Fatalf("open-for-search state wrong: focusMode=%v tab=%d search=%q", a.focusMode, a.tab, a.search)
	}
	if a.prevAppPID != prev {
		t.Fatalf("previous app not remembered: %d, want %d", a.prevAppPID, prev)
	}
	if !key {
		t.Fatal("the notch window did not become the key window; typing would go nowhere")
	}
	if num := mac.WindowNumber(a.win.NativeHandle()); num != 0 {
		t.Logf("shelf CGWindowID=%d — screencapture -l %d shoots the panel", num, num)
		// Leave the number where a shell watcher can pick it up while the
		// hold below keeps the panel on screen.
		_ = os.WriteFile("/tmp/shelf-cgwindowid.txt", []byte(strconv.Itoa(num)), 0o600)
	}
	if os.Getenv("SHELF_CLIP_HOLD") != "" {
		time.Sleep(8 * time.Second)
	}
	// Hide hands the keyboard back to where the user came from.
	onMain(func() { a.hide(hideExplicit) })
	if a.focusMode || a.prevAppPID != 0 {
		t.Fatalf("hide kept keyboard state: focusMode=%v prevAppPID=%d", a.focusMode, a.prevAppPID)
	}
	restored := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		onMain(func() {
			restored = !mac.AppActive() && mac.FrontmostPID() == prev
		})
		if restored {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !restored {
		t.Fatalf("the previous app %d was not re-activated on hide", prev)
	}
	t.Logf("focus restored to pid=%d", prev)
}

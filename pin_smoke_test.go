//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"

	"shelf/internal/mac"
)

// The smoke run pins real catalog cards: 隔空投送 floats, 转成 PDF docks,
// 缩小图片 takes the drag-snap. toolCardDefFor draws them as the tools
// page does (cardDesktop surface).

func pinSmokeEnv(t *testing.T) *shelf {
	t.Helper()
	if os.Getenv("SHELF_BASKET_SMOKE") != "1" {
		t.Skip("set SHELF_BASKET_SMOKE=1 to run the native window smoke test")
	}
	if err := os.MkdirAll("/tmp/shelf-B", 0o755); err != nil {
		t.Fatal(err)
	}
	a := smokeShelf
	// The smoke harness never calls ready(), so pinTick gets its own
	// 30 ms ticker for these tests — the same cadence monitorDrag gives
	// it in production — driven by the real cursor and button state.
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		tk := time.NewTicker(pointerInterval)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-tk.C:
				mygo.RunOnMain(func() {
					a.pinTick(now, cursorPoint(), dragLeftDown(), false)
				})
			}
		}
	}()
	return a
}

// shotWindow captures a real window by CGWindowID into /tmp/shelf-B.
func pinShot(t *testing.T, w *mygo.Window, name string) {
	t.Helper()
	num := mac.WindowNumber(w.NativeHandle())
	if num == 0 {
		t.Fatalf("%s: window has no CGWindowID", name)
	}
	out := filepath.Join("/tmp/shelf-B", name+".png")
	cmd := exec.Command("screencapture", "-l", fmt.Sprint(num), "-o", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("screencapture %s: %v: %s", name, err, b)
	}
	t.Logf("saved %s (CGWindowID=%d)", out, num)
}

// pinShotFull captures the window including its native shadow (-o left
// out): the card keeps the design's drop shadow.
func pinShotFull(t *testing.T, w *mygo.Window, name string) {
	t.Helper()
	num := mac.WindowNumber(w.NativeHandle())
	if num == 0 {
		t.Fatalf("%s: window has no CGWindowID", name)
	}
	out := filepath.Join("/tmp/shelf-B", name+".png")
	cmd := exec.Command("screencapture", "-l", fmt.Sprint(num), out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("screencapture %s: %v: %s", name, err, b)
	}
	t.Logf("saved %s", out)
}

// pinShotRect captures a screen region (for the context menu, which lives
// its own window).
func pinShotRect(t *testing.T, r mygo.Rectangle, name string) {
	t.Helper()
	out := filepath.Join("/tmp/shelf-B", name+".png")
	cmd := exec.Command("screencapture", "-x", "-o",
		"-R", fmt.Sprintf("%d,%d,%d,%d", r.X, r.Y, r.Width, r.Height), out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("screencapture %s: %v: %s", name, err, b)
	}
	t.Logf("saved %s", out)
}

// postMouse posts real mouse events at a screen point through the HID
// tap: the same path a physical mouse takes (see realkey_smoke_test.go).
// events are CGEvent types: 5 moved, 1 leftDown, 2 leftUp, 6 leftDragged,
// 3 rightDown, 4 rightUp.
func postMouse(t *testing.T, x, y float64, events ...uint32) {
	t.Helper()
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY)
	if err != nil {
		t.Fatalf("CoreGraphics: %v", err)
	}
	var (
		create  func(source uintptr, typ uint32, p mac.CGPoint, button uint32) uintptr
		post    func(tap uint32, ev uintptr)
		warp    func(p mac.CGPoint) int32
		release func(cf uintptr)
	)
	purego.RegisterLibFunc(&create, lib, "CGEventCreateMouseEvent")
	purego.RegisterLibFunc(&post, lib, "CGEventPost")
	purego.RegisterLibFunc(&warp, lib, "CGWarpMouseCursorPosition")
	libCF, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY)
	if err != nil {
		t.Fatalf("CoreFoundation: %v", err)
	}
	purego.RegisterLibFunc(&release, libCF, "CFRelease")
	button := uint32(0)
	for _, typ := range events {
		if typ == 3 || typ == 4 || typ == 27 {
			button = 1
		}
		ev := create(0, typ, mac.CGPoint{X: x, Y: y}, button)
		if ev == 0 {
			t.Fatal("CGEventCreateMouseEvent returned nil")
		}
		post(0, ev)
		release(ev)
		time.Sleep(30 * time.Millisecond)
	}
	warp(mac.CGPoint{X: x, Y: y})
}

// settlePin lets the 30 ms monitor tick apply a frame, then reports it.
func settlePin(t *testing.T, it *pinItem, d time.Duration) mygo.Rectangle {
	t.Helper()
	time.Sleep(d)
	var f mygo.Rectangle
	onMain(func() { f = it.frame })
	return f
}

// TestPinSmokeWindowsAndShots proves pinned items are real windows and
// captures the four states for the design comparison: floating, edge
// strip, edge-expanded and the file-drag highlight.
func TestPinSmokeWindowsAndShots(t *testing.T) {
	a := pinSmokeEnv(t)
	d := mygo.Screen.PrimaryDisplay()
	var free, docked *pinItem
	onMain(func() {
		free = a.addPin("airdrop", 1, d.ID, pinFree, false,
			mygo.Rectangle{X: d.WorkArea.X + d.WorkArea.Width - 360, Y: d.WorkArea.Y + 300, Width: pinCardW, Height: pinCardH})
		docked = a.addPin("topdf", 1, d.ID, pinRight, false,
			pinStripFrame(pinRight, d.WorkArea, d.WorkArea.Y+500))
	})
	defer onMain(func() {
		a.unpin(free, false)
		a.unpin(docked, false)
	})
	time.Sleep(700 * time.Millisecond)

	// Floating card, expanded.
	if level := mac.WindowLevel(free.win.NativeHandle()); level != 3 {
		t.Fatalf("pin window level = %d, want 3 (floating)", level)
	}
	pinShot(t, free.win, "pin-floating")
	// The design's drop shadow is the window's own: captured without -o.
	pinShotFull(t, free.win, "pin-floating-shadow")
	// Docked strip.
	pinShot(t, docked.win, "pin-strip")

	// Hover the strip: the monitor tick expands it.
	f := settlePin(t, docked, 0)
	postMouse(t, float64(f.X+4), float64(f.Y+pinStripH/2), 5)
	f = settlePin(t, docked, 500*time.Millisecond)
	if !docked.expanded || f.Width != pinCardW {
		t.Fatalf("strip did not expand on hover: expanded=%v frame=%+v", docked.expanded, f)
	}
	pinShot(t, docked.win, "pin-expanded")

	// Drop highlight: the accent frame a dragged file over the card shows.
	onMain(func() { docked.dropAt = time.Now().Add(30 * time.Second); docked.win.Invalidate() })
	time.Sleep(300 * time.Millisecond)
	pinShot(t, docked.win, "pin-hot")
	onMain(func() { docked.dropAt = time.Time{}; docked.win.Invalidate() })

	// Right-click menu: post a real secondary click on the card, capture
	// the region where the menu opens, then dismiss it.
	postMouse(t, float64(f.X+f.Width/2), float64(f.Y+f.Height/2), 3, 4)
	time.Sleep(600 * time.Millisecond)
	menuRect := mygo.Rectangle{X: f.X, Y: f.Y, Width: f.Width + 220, Height: f.Height + 160}
	pinShotRect(t, menuRect, "pin-menu")
	postMouse(t, float64(f.X-300), float64(f.Y+300), 1, 2) // click away: dismiss
	time.Sleep(300 * time.Millisecond)
}

// TestPinSmokeDragSnap moves a floating pin to the left edge with a real
// pointer drag and asserts it docks as a strip.
func TestPinSmokeDragSnap(t *testing.T) {
	a := pinSmokeEnv(t)
	d := mygo.Screen.PrimaryDisplay()
	var it *pinItem
	onMain(func() {
		it = a.addPin("resize", 1, d.ID, pinFree, false,
			mygo.Rectangle{X: d.WorkArea.X + 700, Y: d.WorkArea.Y + 400, Width: pinCardW, Height: pinCardH})
	})
	defer onMain(func() { a.unpin(it, false) })
	time.Sleep(700 * time.Millisecond)

	f := settlePin(t, it, 0)
	// Press the card, drag to within 24 pt of the left edge, release.
	x, y := float64(f.X+f.Width/2), float64(f.Y+f.Height/2)
	postMouse(t, x, y, 5, 1)
	for i := 1; i <= 10; i++ {
		postMouse(t, x-float64(i)*(x-float64(d.WorkArea.X+10))/10, y, 6)
		time.Sleep(40 * time.Millisecond)
	}
	postMouse(t, float64(d.WorkArea.X+10), y, 2)
	time.Sleep(700 * time.Millisecond)
	f = settlePin(t, it, 0)
	if it.edge != pinLeft || it.expanded {
		t.Fatalf("drag to the left edge did not dock: edge=%v expanded=%v frame=%+v", it.edge, it.expanded, f)
	}
	if f.Width != pinStripW || f.X != d.WorkArea.X {
		t.Fatalf("docked frame wrong: %+v", f)
	}
	pinShot(t, it.win, "pin-docked-left")
}

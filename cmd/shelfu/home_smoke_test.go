//go:build darwin

package main

// Real-window evidence for the 主页 canvas and the trimmed tools page.
// Run with SHELF_BASKET_SMOKE=1; shots land in /tmp/shelf-home/.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
	"shelf/internal/mac"
)

const homeShotDir = "/tmp/shelf-home"

func homeShot(t *testing.T, a *shelf, name string) string {
	t.Helper()
	if a.win == nil {
		t.Fatal("no panel window")
	}
	num := mac.WindowNumber(a.win.NativeHandle())
	if num == 0 {
		t.Fatalf("%s: window has no CGWindowID", name)
	}
	time.Sleep(450 * time.Millisecond)
	onMain(a.syncWindowFrame)
	time.Sleep(150 * time.Millisecond)
	if err := os.MkdirAll(homeShotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(homeShotDir, name+".png")
	if out, err := exec.Command("screencapture", "-x", "-o", fmt.Sprintf("-l%d", num), path).CombinedOutput(); err != nil {
		t.Fatalf("%s: screencapture failed (%v): %s", name, err, out)
	}
	t.Logf("shot %s -> %s", name, path)
	return path
}

const homeDragShotDir = "/tmp/shelf-homedrag"

// homeDragShot photographs the test arena: the panel, the preview and the
// backdrop, never the user's desktop.
func homeDragShot(t *testing.T, r mygo.Rectangle, name string) string {
	t.Helper()
	if err := os.MkdirAll(homeDragShotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(homeDragShotDir, name+".png")
	if out, err := exec.Command("screencapture", "-x", "-o", "-R",
		fmt.Sprintf("%d,%d,%d,%d", r.X, r.Y, r.Width, r.Height), path).CombinedOutput(); err != nil {
		t.Fatalf("%s: screencapture failed (%v): %s", name, err, out)
	}
	t.Logf("shot %s -> %s", name, path)
	return path
}

// homeWindowShot photographs one window alone (no desktop behind it).
func homeWindowShot(t *testing.T, num int, name string) string {
	t.Helper()
	if num == 0 {
		t.Fatalf("%s: no window to shoot", name)
	}
	if err := os.MkdirAll(homeDragShotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(homeDragShotDir, name+".png")
	if out, err := exec.Command("screencapture", "-x", "-o",
		fmt.Sprintf("-l%d", num), path).CombinedOutput(); err != nil {
		t.Fatalf("%s: screencapture failed (%v): %s", name, err, out)
	}
	t.Logf("shot %s -> %s", name, path)
	return path
}

// TestHomeSmokeDragPreview drives a real drag out of the 主页 canvas: the
// preview window must carry the widget past the content box's clip, vanish
// when the drag comes back, and become the pin on release. Screenshots of
// the arena land in /tmp/shelf-homedrag/.
func TestHomeSmokeDragPreview(t *testing.T) {
	if os.Getenv("SHELF_BASKET_SMOKE") != "1" {
		t.Skip("native window smoke requires SHELF_BASKET_SMOKE=1")
	}
	arena, area := newSmokePointerArena(t)
	a := &shelf{store: mustStore(t), dataDir: t.TempDir(), tab: pageHome, level: 2, visible: true,
		progress: 1, topInset: 32, notchW: 180, notchH: 32, hasNotch: true,
		x: area.X + 24, y: area.Y + 24, dispBounds: mygo.Screen.PrimaryDisplay().Bounds}
	a.notchX, a.notchY = a.x+(sheetW-180)/2, a.y
	var panelNum, arenaNum int
	onMain(func() {
		l := layout.DefaultWith(false)
		a.toolsLay, a.settings.AIUsage.Enabled = &l, false
		a.newNotchWindow()
		a.syncWindowFrame()
		a.win.ShowInactive()
		panelNum, arenaNum = mac.WindowNumber(a.winH), mac.WindowNumber(arena.NativeHandle())
	})
	t.Cleanup(func() {
		onMain(func() {
			for _, it := range a.pins {
				if it.win != nil {
					it.win.Destroy()
				}
			}
			a.pins = nil
			a.resetHomeEdit()
			if a.win != nil {
				a.win.Destroy()
			}
		})
	})
	time.Sleep(800 * time.Millisecond)

	// The headless view uses the same layout to locate the widget's box,
	// not a hard-coded coordinate.
	var box ui.Rect
	var frame mygo.Rectangle
	onMain(func() {
		frame = a.frame
		l := a.toolsLayout()
		mirror := &shelf{store: a.store, tab: pageHome, level: 2, visible: true, progress: 1,
			topInset: a.topInset, notchW: a.notchW, notchH: a.notchH,
			settings: a.settings, toolsLay: &l, np: a.np, x: a.x, y: a.y}
		tr := ui.NewTester(mirror.view, frame.Width, frame.Height)
		var ok bool
		box, ok = tr.Find("音乐")
		if !ok {
			t.Fatalf("music widget missing from the panel: %v", tr.Texts())
		}
	})
	// The cover art takes no press, so the card's press starts here.
	start := mygo.Point{X: frame.X + int(box.X) + 30, Y: frame.Y + int(box.Y) + 30}
	postMouse(t, float64(start.X), float64(start.Y), 5)
	time.Sleep(150 * time.Millisecond)
	postMouse(t, float64(start.X), float64(start.Y), 1)
	held := true
	t.Cleanup(func() {
		if held {
			postMouse(t, float64(start.X), float64(start.Y), 2)
		}
	})
	time.Sleep(homeLongPress + 250*time.Millisecond)
	var editing bool
	onMain(func() { editing = a.homePageOf().editing })
	if !editing {
		t.Fatal("a real long press did not enter home editing")
	}
	homeDragShot(t, area, "home-edit")

	// Out of the content box: the preview takes the copy over.
	end := mygo.Point{X: area.X + area.Width - 220, Y: start.Y + 120}
	for i := 1; i <= 12; i++ {
		x := start.X + (end.X-start.X)*i/12
		y := start.Y + (end.Y-start.Y)*i/12
		postMouse(t, float64(x), float64(y), 6)
	}
	time.Sleep(200 * time.Millisecond)
	previewNum := 0
	for _, w := range ownSmokeWindows(t) {
		if w.ID != panelNum && w.ID != arenaNum && w.Onscreen && isDragCardWindow(w) && insideRect(end, w.Frame) {
			if previewNum != 0 {
				t.Error("two drag windows follow the same gesture")
			}
			previewNum = w.ID
		}
	}
	homeDragShot(t, area, "home-drag-preview")
	if previewNum != 0 {
		homeWindowShot(t, previewNum, "home-drag-preview-window")
	} else {
		t.Error("the dragged widget has no visible window outside the panel")
	}
	var persisted, live int
	onMain(func() { persisted, live = len(a.toolsLayout().Pins), len(a.pins) })
	if persisted != 0 || live != 0 {
		t.Error("holding a drag published a pin before release")
	}

	// Back over the canvas: the page draws the copy again and the preview
	// must go away without committing.
	back := mygo.Point{X: frame.X + int(box.X) + 60, Y: frame.Y + int(box.Y) + 60}
	postMouse(t, float64(back.X), float64(back.Y), 6)
	time.Sleep(200 * time.Millisecond)
	for _, w := range ownSmokeWindows(t) {
		if w.ID == previewNum && w.Onscreen {
			t.Error("returning to the canvas left the preview visible")
		}
	}
	homeDragShot(t, area, "home-drag-returned")

	// Out again and release: the pin is the preview's own window.
	postMouse(t, float64(end.X), float64(end.Y), 6)
	time.Sleep(200 * time.Millisecond)
	previewNum = 0
	for _, w := range ownSmokeWindows(t) {
		if w.ID != panelNum && w.ID != arenaNum && w.Onscreen && isDragCardWindow(w) && insideRect(end, w.Frame) {
			previewNum = w.ID
		}
	}
	if previewNum == 0 {
		t.Fatal("the second drag out has no preview")
	}
	postMouse(t, float64(end.X), float64(end.Y), 2)
	held = false
	time.Sleep(450 * time.Millisecond)
	var pin *pinItem
	onMain(func() {
		if len(a.pins) == 1 {
			pin = a.pins[0]
		}
	})
	if pin == nil {
		t.Fatal("the release did not create exactly one pin")
	}
	var pinNum int
	onMain(func() {
		if pin.win != nil {
			pinNum = mac.WindowNumber(pin.win.NativeHandle())
		}
	})
	onMain(func() {
		l := a.toolsLayout()
		if _, ok := l.HomeAt(0, 0); ok {
			t.Error("the pinned widget stayed on the canvas")
		}
	})
	if pinNum != previewNum {
		t.Errorf("the release replaced the drag window (%d) instead of reusing the preview (%d)", pinNum, previewNum)
	}
	homeDragShot(t, area, "home-drag-pinned")
	if pinNum != 0 {
		homeWindowShot(t, pinNum, "home-drag-pinned-window")
	}
	raw, err := os.ReadFile(filepath.Join(a.dataDir, layout.FileName))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("saved layout.json=%s", raw)
}

// TestHomeSmokeShots shoots the home canvas with and without music, in
// edit mode and mid-drag, then the tools page with actions only.
func TestHomeSmokeShots(t *testing.T) {
	a := redesignShelf(t)
	onMain(func() {
		a.settings.AIUsage.Enabled = false
		a.toolsLay = nil // the AI-off default: music, awake, clipboard
		a.settings.IslandNowPlaying = true
		fakeTrack(a, true)
		a.setTab(pageHome)
	})
	onMain(func() {
		a.place(mygo.Screen.CursorScreenPoint())
		a.show(false)
		a.setLevel(1)
		a.jumpLevel = true
		a.win.Invalidate()
	})
	homeShot(t, a, "home-island")
	expandShelf(t, a)
	homeShot(t, a, "home-music")

	onMain(func() {
		a.np = &nowPlayingModule{host: a}
		a.win.Invalidate()
	})
	homeShot(t, a, "home-empty")

	onMain(func() {
		fakeTrack(a, true)
		p := a.homePageOf()
		p.editing, p.editAt = true, time.Now()
		a.win.Invalidate()
	})
	homeShot(t, a, "home-edit")

	onMain(func() {
		p := a.homePageOf()
		for _, w := range a.toolsLayout().Home {
			if w.ID == "awake" {
				p.press = &homePress{id: w.ID, dragging: true, orig: w,
					px: homeX(2) + 60, py: homeY(0) + 70, grabDX: 50, grabDY: 40}
			}
		}
		a.win.Invalidate()
	})
	homeShot(t, a, "home-drag")

	onMain(func() {
		p := a.homePageOf()
		p.press, p.editing = nil, false
		l := a.toolsLayout()
		l.Home = nil
		a.toolsLay = &l
		a.setTab(pageTools)
		a.win.Invalidate()
	})
	homeShot(t, a, "tools-actions")

	onMain(func() {
		l := layout.DefaultWith(false)
		a.toolsLay = &l
		motionClosed(a)
	})
}

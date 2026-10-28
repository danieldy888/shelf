//go:build darwin

package main

// Real-window proof for the draggable island: with 刘海样式 = 灵动岛 on the
// built-in screen, a real HID press on the capsule drags it to the middle of
// the screen, the release saves it, a tap there opens the panel, and a drag
// into the bottom-right corner stops at the point where the panel still
// fits. Screenshots land in /tmp/shelf-island-drag/; every mouse event is
// posted inside a test-owned backdrop over the work area and never over
// another app. Run with SHELF_BASKET_SMOKE=1.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"shelf/internal/mac"
	"shelf/internal/settings"
)

const islandDragShotDir = "/tmp/shelf-island-drag"

// islandDragShot captures a screen region of the test backdrop.
func islandDragShot(t *testing.T, r mygo.Rectangle, name string) string {
	t.Helper()
	if err := os.MkdirAll(islandDragShotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(islandDragShotDir, name+".png")
	cmd := exec.Command("screencapture", "-x", "-o", "-R",
		fmt.Sprintf("%d,%d,%d,%d", r.X, r.Y, r.Width, r.Height), out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("screencapture %s: %v: %s", name, err, b)
	}
	t.Logf("shot %s -> %s", name, out)
	return out
}

// islandDragWindowShot captures the resident window by CGWindowID.
func islandDragWindowShot(t *testing.T, a *shelf, name string) string {
	t.Helper()
	num := mac.WindowNumber(a.win.NativeHandle())
	if num == 0 {
		t.Fatalf("%s: window has no CGWindowID", name)
	}
	if err := os.MkdirAll(islandDragShotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(islandDragShotDir, name+".png")
	cmd := exec.Command("screencapture", "-x", "-o", fmt.Sprintf("-l%d", num), out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("screencapture %s: %v: %s", name, err, b)
	}
	t.Logf("shot %s -> %s (CGWindowID=%d, frame %+v)", name, out, num, a.desiredFrame())
	return out
}

// islandDragTicker runs the production 30 ms monitor for this test: the
// smoke harness never starts startMonitoring, and the drag is driven by it.
func islandDragTicker(t *testing.T, a *shelf) {
	t.Helper()
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
					if a.quitting {
						return
					}
					a.animate(now)
					a.monitorDrag(now)
					a.syncWindowFrame()
					a.syncPointerGate()
				})
			}
		}
	}()
}

// requireOwnPress refuses to post a press WindowServer would hand to another
// process. The point must be on a window of this test process — the island
// overlay itself once the mouse gate accepted it. A sibling test app may own
// the same rows (another worktree's build sits on the screen top too); the
// drag is then not ours to press.
func requireOwnPress(t *testing.T, p mygo.Point) {
	t.Helper()
	var hit int
	onMain(func() {
		primary := mygo.Screen.PrimaryDisplay()
		hit = mac.WindowAtPoint(float64(p.X), float64(p.Y), float64(primary.Bounds.Y+primary.Bounds.Height))
	})
	for _, w := range ownSmokeWindows(t) {
		if w.ID == hit {
			return
		}
	}
	t.Skipf("refusing to press %+v: window %d there belongs to another process", p, hit)
}

// TestIslandDragSmokeShots drives the drag with real mouse events and
// captures the four states.
func TestIslandDragSmokeShots(t *testing.T) {
	if os.Getenv("SHELF_BASKET_SMOKE") != "1" {
		t.Skip("set SHELF_BASKET_SMOKE=1 to run the native window smoke test")
	}
	a := redesignShelf(t)
	work := mygo.Screen.PrimaryDisplay().WorkArea
	// The backdrop is the whole visible frame: every posted point is inside
	// a window of this process, and no shot shows the desktop.
	newSmokePointerBackdrop(t, work)
	t.Cleanup(func() {
		onMain(func() {
			next := a.settings
			next.ShellStyle = settings.ShellStyleNotch
			next.IslandPos = nil
			a.applySettings(next)
			a.applyShellStyle()
			a.place(mygo.Point{X: work.X + work.Width/2, Y: work.Y + 1})
			a.visible, a.progress, a.animationFrom = false, 0, 0
			a.peeking, a.peekProgress = false, 0
			a.syncMousePassThrough()
			a.win.Invalidate()
		})
	})

	// 灵动岛 on the built-in screen, resting on its default row with no
	// saved place from an earlier run.
	islandStyle(t, a, settings.ShellStyleIsland)
	onMain(func() {
		a.settings.IslandPos = nil
		a.placeIslandAxis()
		a.syncWindowFrame()
		a.win.ShowInactive()
		a.win.Invalidate()
	})
	islandDragTicker(t, a)
	var stopObs func()
	onMain(func() {
		stopObs = mac.ObserveMouseDown(a.win.NativeHandle(), func(inside bool, button int) {
			a.mouseDown(inside, button, mygo.Screen.CursorScreenPoint())
		})
	})
	t.Cleanup(func() { onMain(stopObs) })
	time.Sleep(300 * time.Millisecond)

	var start, spot0 mygo.Rectangle
	onMain(func() { start = a.islandRect(); spot0 = start })
	t.Logf("resting capsule %+v", start)
	islandDragShot(t, work, "1-before")
	islandDragWindowShot(t, a, "1-before-window")

	// Press on the capsule and drag it to the middle of the screen in
	// steps, the way a hand moves: every step is a real left-dragged event.
	cx, cy := float64(start.X+start.Width/2), float64(start.Y+start.Height/2)
	tx, ty := float64(work.X+work.Width*2/3), float64(work.Y+work.Height*2/3)
	warpPointerTo(t, cx, cy)
	time.Sleep(150 * time.Millisecond) // the mouse gate accepts the capsule
	requireOwnPress(t, mygo.Point{X: int(cx), Y: int(cy)})
	postMouse(t, cx, cy, 1) // leftDown
	const steps = 10
	for i := 1; i <= steps; i++ {
		postMouse(t, cx+(tx-cx)*float64(i)/steps, cy+(ty-cy)*float64(i)/steps, 6) // leftDragged
		time.Sleep(70 * time.Millisecond)
	}
	postMouse(t, tx, ty, 2) // leftUp
	time.Sleep(300 * time.Millisecond)
	// Park the pointer beside the island so the shot is the resting
	// capsule, not the hover pill.
	postMouse(t, float64(work.X+40), float64(work.Y+40), 5)
	time.Sleep(300 * time.Millisecond)

	var moved mygo.Rectangle
	onMain(func() { moved = a.islandRect() })
	if moved.Y <= spot0.Y+100 {
		t.Fatalf("the drag did not move the island: %+v -> %+v", spot0, moved)
	}
	if moved.Y+moved.Height > work.Y+work.Height || moved.X < work.X || moved.X+moved.Width > work.X+work.Width {
		t.Fatalf("the dragged island %+v left the work area %+v", moved, work)
	}
	saved, err := settings.Load(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	key := islandSpotKey(a.dispID)
	if got, ok := saved.IslandPos[key]; !ok || got != (settings.IslandPos{X: moved.X + moved.Width/2, Y: moved.Y}) {
		t.Fatalf("the release did not save the place: %+v (capsule %+v)", saved.IslandPos, moved)
	}
	t.Logf("dragged capsule %+v", moved)
	islandDragShot(t, work, "2-after")
	islandDragWindowShot(t, a, "2-after-window")

	// A tap there opens the panel, in the new place.
	cx, cy = float64(moved.X+moved.Width/2), float64(moved.Y+moved.Height/2)
	warpPointerTo(t, cx, cy)
	time.Sleep(150 * time.Millisecond)
	requireOwnPress(t, mygo.Point{X: int(cx), Y: int(cy)})
	postMouse(t, cx, cy, 1, 2)
	time.Sleep(600 * time.Millisecond)
	var open, panel mygo.Rectangle
	onMain(func() {
		if !a.visible {
			t.Fatal("a tap on the moved island did not open the panel")
		}
		open, panel = a.islandRect(), a.desiredFrame()
	})
	t.Logf("panel at %+v inside the window %+v", open, panel)
	islandDragShot(t, work, "3-panel")
	islandDragWindowShot(t, a, "3-panel-window")

	// Fold it away, then drag the island into the bottom-right corner: it
	// stops where its panel still fits.
	onMain(func() {
		a.hide(hidePassive)
		a.visible, a.progress, a.animationFrom = false, 0, 0
		a.peeking, a.peekProgress = false, 0
		a.syncMousePassThrough()
		a.win.Invalidate()
	})
	time.Sleep(400 * time.Millisecond)
	cx, cy = float64(open.X+open.Width/2), float64(open.Y+open.Height/2)
	edgeX, edgeY := float64(work.X+work.Width-20), float64(work.Y+work.Height-20)
	warpPointerTo(t, cx, cy)
	time.Sleep(150 * time.Millisecond)
	requireOwnPress(t, mygo.Point{X: int(cx), Y: int(cy)})
	postMouse(t, cx, cy, 1)
	for i := 1; i <= steps; i++ {
		postMouse(t, cx+(edgeX-cx)*float64(i)/steps, cy+(edgeY-cy)*float64(i)/steps, 6)
		time.Sleep(70 * time.Millisecond)
	}
	postMouse(t, edgeX, edgeY, 2)
	time.Sleep(300 * time.Millisecond)
	postMouse(t, float64(work.X+40), float64(work.Y+40), 5)
	time.Sleep(300 * time.Millisecond)

	var clamped mygo.Rectangle
	var clampedSpot islandSpot
	onMain(func() { clamped, clampedSpot = a.islandRect(), a.islandSpotNow() })
	pw, ph := islandPanelRoom()
	if clampedSpot.Top != work.Y+work.Height-ph {
		t.Fatalf("the island stopped at top %d, want %d (its panel's bottom on the work area)", clampedSpot.Top, work.Y+work.Height-ph)
	}
	if clampedSpot.Centre != work.X+work.Width-pw/2 {
		t.Fatalf("the island stopped at centre %d, want %d", clampedSpot.Centre, work.X+work.Width-pw/2)
	}
	if clamped.Y+ph > work.Y+work.Height {
		t.Fatalf("the panel would open past the work area: capsule %+v", clamped)
	}
	t.Logf("clamped capsule %+v spot %+v", clamped, clampedSpot)
	islandDragShot(t, work, "4-clamped")
	islandDragWindowShot(t, a, "4-clamped-window")
}

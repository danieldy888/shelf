//go:build darwin

package main

// Real-window evidence for the 主页 catalogue's entry: the dashed cell in
// edit mode and the catalogue it opens. Run with SHELF_BASKET_SMOKE=1;
// shots land in /tmp/shelf-homepicker/.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"shelf/internal/layout"
	"shelf/internal/mac"
)

const homePickerShotDir = "/tmp/shelf-homepicker"

// homePickerShot screenshots the notch window showing the home page.
func homePickerShot(t *testing.T, a *shelf, name string) string {
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
	if err := os.MkdirAll(homePickerShotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(homePickerShotDir, name+".png")
	if out, err := exec.Command("screencapture", "-x", "-o", fmt.Sprintf("-l%d", num), path).CombinedOutput(); err != nil {
		t.Fatalf("%s: screencapture failed (%v): %s", name, err, out)
	}
	t.Logf("shot %s -> %s", name, path)
	return path
}

// TestHomePickerSmokeShots shoots the edit grid with its catalogue entry
// and the open catalogue.
func TestHomePickerSmokeShots(t *testing.T) {
	a := redesignShelf(t)
	onMain(func() {
		a.settings.AIUsage.Enabled = false
		a.toolsLay = nil // music, awake and the clipboard: column 3 stays free
		a.setTab(pageHome)
	})
	onMain(func() {
		a.place(mygo.Screen.CursorScreenPoint())
		a.show(false)
		a.setLevel(1)
		a.jumpLevel = true
		a.win.Invalidate()
	})
	expandShelf(t, a)
	onMain(func() {
		p := a.homePageOf()
		p.editing, p.editAt = true, time.Now()
		a.win.Invalidate()
	})
	homePickerShot(t, a, "home-edit-entry")

	onMain(func() {
		// One widget on the canvas leaves the whole catalogue to show.
		l := layout.Layout{Version: layout.Version, Home: []layout.Widget{
			{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		}}
		a.toolsLay = &l
		p := a.homePageOf()
		p.picking = true
		a.win.Invalidate()
	})
	homePickerShot(t, a, "home-picker")

	onMain(func() {
		p := a.homePageOf()
		p.picking, p.editing, p.press = false, false, nil
		l := layout.DefaultWith(false)
		a.toolsLay = &l
		motionClosed(a)
	})
}

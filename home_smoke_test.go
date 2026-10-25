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

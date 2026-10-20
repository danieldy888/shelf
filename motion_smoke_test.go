//go:build darwin

package main

// Real-window proof for the motion work: the elastic opening (a numeric
// trace of the drawn shell, frame by frame, plus a shot), the completion
// capsule in its three states, its yield to hover, and the progressive
// blur on a scrolled clipboard list. Run with SHELF_BASKET_SMOKE=1; PNGs
// land in /tmp/shelf-motion/. The instance reads the smoke data dir, never
// ~/Library/Application Support/Shelf.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"shelf/internal/mac"
)

const motionShotDir = "/tmp/shelf-motion"

// motionShot captures the notch window to /tmp/shelf-motion/name.png after
// letting wait pass, so a mid-animation or mid-shake state is what shows.
func motionShot(t *testing.T, a *shelf, name string, wait time.Duration) string {
	t.Helper()
	if a.win == nil {
		t.Fatal("no notch window")
	}
	num := mac.WindowNumber(a.win.NativeHandle())
	if num == 0 {
		t.Fatalf("%s: window has no CGWindowID", name)
	}
	time.Sleep(wait)
	// The smoke harness never runs the 30 ms tick that follows the drawing,
	// so apply the frame the real app would have by now.
	onMain(a.syncWindowFrame)
	time.Sleep(120 * time.Millisecond)
	if err := os.MkdirAll(motionShotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shot := filepath.Join(motionShotDir, name+".png")
	out, err := exec.Command("screencapture", "-x", "-o", fmt.Sprintf("-l%d", num), shot).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: screencapture failed (%v): %s", name, err, out)
	}
	t.Logf("shot %s -> %s", name, shot)
	return shot
}

// motionClosed puts the real window into the resting closed state.
func motionClosed(a *shelf) {
	onMain(func() {
		a.hide(hidePassive)
		a.visible, a.edgeOpened = false, false
		a.progress, a.animationFrom = 0, 0
		a.animationStart = time.Time{}
		a.peeking = false
		a.peekProgress, a.peekFrom = 0, 0
		a.peekAnimStart = time.Time{}
		a.fb = feedback{}
		a.status, a.statusUntil, a.statusShown, a.statusErr = "", time.Time{}, time.Time{}, false
		a.place(mygo.Screen.CursorScreenPoint())
		a.win.SetOpacity(1)
		a.win.ShowInactive()
		a.syncMousePassThrough()
		a.syncWindowFrame()
		a.win.Invalidate()
	})
}

// TestSmokeOpeningIsElasticAndNotClipped traces the drawn shell of the real
// window while it opens: the height must overshoot its final size, and the
// window must hold the shell in every sampled frame.
func TestSmokeOpeningIsElasticAndNotClipped(t *testing.T) {
	a := redesignShelf(t)
	motionClosed(a)
	time.Sleep(300 * time.Millisecond)
	onMain(func() { a.show(false) })
	final := float32(0)
	peak := float32(0)
	for i := range 80 {
		var h, w float32
		var frame mygo.Rectangle
		onMain(func() {
			h, w, frame = a.fx.h, a.fx.w, a.desiredFrame()
		})
		peak = max(peak, h)
		final = h
		if float64(frame.Width) < float64(w) && frame.Height < int(h+0.5) {
			t.Fatalf("frame %d: window %+v cuts the shell %.1f tall", i, frame, h)
		}
		if i == 9 {
			motionShot(t, a, "open-mid", 0)
		}
		// Sampling by hopping to the main thread starves the window's own
		// frames if it is too tight; 10 ms keeps both the trace and the draw
		// dense enough.
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("drawn shell: peak %.1f, later %.1f", peak, final)
	if peak <= final+1 {
		// The trace samples the main thread every ~15-30 ms, so it can miss
		// the very top of the flight; the headless test pins the size.
		t.Fatalf("the real window never overshot: peak %.1f, end %.1f", peak, final)
	}
	motionShot(t, a, "open-settled", 250*time.Millisecond)
	motionClosed(a)
}

// TestSmokeCapsuleSuccess shoots the success capsule: green glow, the
// toast's words under the notch, gone after its hold.
func TestSmokeCapsuleSuccess(t *testing.T) {
	a := redesignShelf(t)
	motionClosed(a)
	time.Sleep(300 * time.Millisecond)
	onMain(func() {
		a.toastStatus("已压缩 3 张 · 4.6 MB → 1.8 MB", time.Now().Add(3*time.Second), false)
	})
	shot := motionShot(t, a, "capsule-success", 500*time.Millisecond)
	if shot == "" {
		t.Fatal("no shot")
	}
	var drawn bool
	onMain(func() { drawn = a.fb.drawn })
	if !drawn {
		t.Fatal("the success capsule is not drawn in the real window")
	}
	// The capsule is a real click target even though the window lets the
	// press through: the global monitor expands the shelf for it.
	// A press there reaches the shelf only through the global monitor, which
	// AppKit installs once Shelf has Accessibility access (the same grant the
	// hover pill's click needs). Without it the press is not observable here.
	// The smoke harness never runs ready(), so the app's mouse monitor is
	// installed here: the capsule's press must arrive the way production's
	// does, through the global monitor while the window lets it through.
	var stop func()
	onMain(func() {
		stop = mac.ObserveMouseDown(a.win.NativeHandle(), func(inside bool) {
			a.mouseDown(inside, mygo.Screen.CursorScreenPoint())
		})
	})
	t.Cleanup(func() { onMain(stop) })
	var pill mygo.Rectangle
	onMain(func() { pill = a.peekPillRect() })
	if mac.AccessibilityTrusted() {
		clickAt(t, float64(pill.X+pill.Width/2), float64(pill.Y+pill.Height-8))
		var visible bool
		onMain(func() { visible = a.visible })
		t.Logf("capsule press at %+v: visible=%v", pill, visible)
		if !visible {
			t.Error("a press on the capsule did not expand the shelf")
		}
	} else {
		t.Logf("capsule press at %+v not exercised: no Accessibility access for this test process", pill)
	}
	motionShot(t, a, "capsule-click-expands", 300*time.Millisecond)
	onMain(func() { a.hide(hidePassive) })
	time.Sleep(400 * time.Millisecond)
	onMain(func() { drawn = a.fb.drawn })
	if drawn {
		t.Fatal("the capsule stayed drawn after the panel opened")
	}
	motionClosed(a)
}

// TestSmokeCapsuleFailure shoots the failure capsule twice: during its
// shake and after it, showing the red status and the settled offset.
func TestSmokeCapsuleFailure(t *testing.T) {
	a := redesignShelf(t)
	motionClosed(a)
	time.Sleep(300 * time.Millisecond)
	onMain(func() {
		a.toastStatus("压缩包已加密，无法解压", time.Now().Add(4*time.Second), true)
	})
	motionShot(t, a, "capsule-failure-shake", 418*time.Millisecond)
	motionShot(t, a, "capsule-failure-hold", 500*time.Millisecond)
	var offset float32
	onMain(func() { offset = a.fb.shake(time.Now(), false) })
	if offset != 0 {
		t.Fatalf("the failure capsule is still shaking after its run: %.2f", offset)
	}
	motionClosed(a)
}

// TestSmokeCapsuleRunning holds a converted job open under a closed panel:
// the orange capsule reports its progress until the job ends.
func TestSmokeCapsuleRunning(t *testing.T) {
	a := redesignShelf(t)
	motionClosed(a)
	time.Sleep(300 * time.Millisecond)
	job := &convertJob{target: toJPEG, unit: "张", started: time.Now().Add(-time.Second)}
	job.total.Store(3)
	job.done.Store(1)
	onMain(func() {
		a.conv = &converter{host: a, job: job}
		a.win.Invalidate()
	})
	t.Cleanup(func() { onMain(func() { a.conv = nil }) })
	// The capsule grows out of the notch: its strip must be resized before
	// the shell outgrows the old one, and hold the glow's blur as well.
	for i := range 24 {
		var w, h float32
		var frame mygo.Rectangle
		onMain(func() {
			a.syncWindowFrame()
			w, h, frame = a.fx.w, a.fx.h, a.frame
		})
		if frame.Width < int(w+0.5) || frame.Height < int(h+0.5) {
			t.Fatalf("frame %d: window %+v cuts the capsule drawn at %.1fx%.1f", i, frame, w, h)
		}
		if i == 23 && frame.Width < fbMaxW+2*fbGlowPad {
			t.Fatalf("frame %d: strip %+v cannot hold the capsule and its glow", i, frame)
		}
		time.Sleep(10 * time.Millisecond)
	}
	shot := motionShot(t, a, "capsule-running", 300*time.Millisecond)
	if shot == "" {
		t.Fatal("no shot")
	}
	var text string
	var live bool
	onMain(func() { text, live = a.fb.text, a.fb.live })
	if !live || text != "正在转成 JPEG · 2/3" {
		t.Fatalf("running capsule = %q live=%v", text, live)
	}
	// Hover takes the peek shape back: the chevron, not the capsule.
	onMain(func() {
		a.peeking, a.peekProgress, a.peekFrom = true, 1, 1
		a.peekAnimStart = time.Time{}
		a.win.Invalidate()
	})
	motionShot(t, a, "capsule-yields-to-hover", 300*time.Millisecond)
	onMain(func() {
		if a.capsuleUp(time.Now()) {
			t.Error("the capsule must yield while the pointer hovers the notch")
		}
		pw, ph := a.peekSize()
		t.Logf("hover shell drawn %.1fx%.1f, chevron asks for %.1fx%.1f", a.fx.w, a.fx.h, pw, ph)
		if a.fx.w > pw+2 || a.fx.h > ph+2 {
			t.Errorf("the hover shape still carries the capsule's size: %.1fx%.1f", a.fx.w, a.fx.h)
		}
	})
	motionClosed(a)
}

// TestSmokeProgressiveBlur scrolls a long clipboard list in the real window
// and shoots the list at the top and in the middle: the band melts into the
// shell, and the bottom band appears only once there is more below.
func TestSmokeProgressiveBlur(t *testing.T) {
	a := redesignShelf(t)
	onMain(func() {
		for i := range 24 {
			if err := a.store.AddClipText(fmt.Sprintf("剪贴板第 %d 条：把这段文字复制到别处 %d", i+1, i+1), "com.example.App"); err != nil {
				t.Error(err)
				return
			}
		}
		a.setTab(pageClipboard)
	})
	expandShelf(t, a)
	time.Sleep(400 * time.Millisecond)
	var page *clipboardPage
	onMain(func() { page = a.page(pageClipboard).(*clipboardPage) })
	if page == nil {
		t.Fatal("no clipboard page")
	}
	var maxY float32
	onMain(func() { maxY = page.scroll.MaxY })
	if maxY <= 0 {
		t.Fatalf("the 24-row list does not scroll: MaxY=%.0f", maxY)
	}
	motionShot(t, a, "blur-top", 200*time.Millisecond)
	// A real wheel scroll over the list, as a trackpad would deliver it.
	// One event already runs to the end of a 24-row list, so scroll back up
	// once to land partway, where both bands melt.
	for _, delta := range []float64{40, -20} {
		onMain(func() {
			if !mac.SurfaceScroll(a.win.NativeHandle(), 280, 240, delta) {
				t.Error("the scroll event did not reach the list")
			}
		})
		time.Sleep(40 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	var y float32
	onMain(func() { y = page.scroll.Y })
	motionShot(t, a, "blur-scrolled", 200*time.Millisecond)
	if y <= 0 || y >= maxY {
		t.Fatalf("the wheel left the list at Y=%.1f of %.1f, neither band visible", y, maxY)
	}
	t.Logf("scrolled to %.1f of %.1f", y, maxY)
	onMain(func() {
		a.tab = pageTray
		a.store.Clear()
	})
	motionClosed(a)
}

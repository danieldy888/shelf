//go:build darwin

package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"shelf/internal/mac"
	"testing"
	"time"
)

func TestCollapsedMarginsReachUnderlyingWindow(t *testing.T) {
	a := redesignShelf(t)
	oldCursor := cursorPoint
	t.Cleanup(func() { onMain(func() { cursorPoint = oldCursor }); motionClosed(a) })
	for _, state := range []string{"peek", "capsule"} {
		motionClosed(a)
		time.Sleep(600 * time.Millisecond)
		onMain(func() {
			a.dragging, a.pointerDrag = false, false
			if state == "peek" {
				a.setPeek(true)
			} else {
				a.toastStatus("已复制", time.Now().Add(8*time.Second), statusOK)
				a.peekProgress = 0.5 // hover-out tail can coexist with the capsule
			}
			a.win.Invalidate()
		})
		time.Sleep(650 * time.Millisecond)
		var receiver *mygo.Window
		var point mygo.Point
		var receiverNum, hit, received int
		var stop func()
		var ignoring bool
		onMain(func() {
			frame := a.win.Bounds()
			point = mygo.Point{X: frame.X + 2, Y: a.y + a.notchH + 12}
			cursorPoint = func() mygo.Point { return point }
			receiver = mygo.NewWindow(mygo.WindowOptions{Title: "ShelfU click receiver", Width: 40, Height: 40, Hidden: true, Frameless: true, DisableShadow: true, Panel: true, SkipTaskbar: true, Content: ui.View(func(c *ui.Context) { ui.Text(c, "Receiver") })})
			h := receiver.NativeHandle()
			mac.SetWindowFrame(h, point.X-10, point.Y-10, 40, 40)
			receiver.ShowInactive()
			mac.SetWindowLevel(h, mac.StatusWindowLevel-1)
			receiverNum = mac.WindowNumber(h)
			stop = mac.ObserveMouseDown(h, func(inside bool, button int) {
				if inside {
					received++
				}
			})
			a.syncMousePassThrough()
			ignoring = a.ignoringMouse
		})
		t.Cleanup(func() { onMain(func() { stop(); receiver.Close() }) })
		time.Sleep(150 * time.Millisecond) // let WindowServer apply ordering and ignoresMouseEvents
		onMain(func() {
			mac.SetWindowFrame(receiver.NativeHandle(), point.X-10, point.Y-10, 40, 40)
			hit = mac.WindowAtPoint(float64(point.X), float64(point.Y), float64(a.dispBounds.Y+a.dispBounds.Height))
		})
		if !ignoring || hit != receiverNum {
			t.Fatalf("%s: margin ignores=%v native target=%d want receiver=%d", state, ignoring, hit, receiverNum)
		}
		if mac.AccessibilityTrusted() {
			clickAt(t, float64(point.X), float64(point.Y))
			var count int
			onMain(func() { count = received })
			if count == 0 {
				t.Fatalf("%s: underlying window did not receive the click", state)
			}
			t.Logf("%s: native target=%d, underlying mouse-downs=%d", state, hit, count)
		} else {
			t.Logf("%s: native target=%d; injected click not exercised without Accessibility", state, hit)
		}
	}
}

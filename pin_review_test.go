//go:build darwin

package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func pinReviewDrag(t *testing.T, a *shelf, p *mygo.Point, active bool) {
	t.Helper()
	oldP, oldS, oldT, oldD := cursorPoint, dragState, dragTypes, dragLeftDown
	cursorPoint = func() mygo.Point { return *p }
	dragState = func() (bool, int64) { return active, 7 }
	dragTypes = func() []string { return []string{"public.file-url"} }
	dragLeftDown = func() bool { return true }
	a.dragSerial = 7
	t.Cleanup(func() { cursorPoint, dragState, dragTypes, dragLeftDown = oldP, oldS, oldT, oldD })
}

func TestPinReturnZoneHoldsPeek(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	a := motionShelf()
	a.notchX, a.notchY, a.notchW, a.notchH = 600, 0, 180, 32
	a.dispBounds = pinTestDisplays[0].Bounds
	p := mygo.Point{X: 690, Y: 78}
	pinReviewDrag(t, a, &p, false)
	a.pinRegister(&pinItem{cardID: "zip", cardW: pinCardW, display: 1, pressed: true, moving: true,
		frame: mygo.Rectangle{X: 690, Y: 78, Width: pinCardW, Height: pinCardH}})
	for range 4 {
		a.monitorDrag(time.Now())
		if !a.pinReturning || !a.peeking {
			t.Fatalf("return invitation folded inside the release zone: returning=%v peeking=%v", a.pinReturning, a.peeking)
		}
	}
	p.Y = 300
	a.monitorDrag(time.Now())
	if a.pinReturning || a.peeking {
		t.Fatal("return invitation survived leaving the release zone")
	}
}

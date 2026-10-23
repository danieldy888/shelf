//go:build darwin

package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
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

func TestPinReturnPillBeatsOtherContent(t *testing.T) {
	for _, state := range []string{"player", "capsule", "retracting"} {
		t.Run(state, func(t *testing.T) {
			a := motionWindow()
			a.settings.IslandNowPlaying = true
			a.np = &nowPlayingModule{host: a, have: true, last: mac.NowPlayingState{Playing: true, Title: "Review Track"}}
			if state != "player" {
				a.toastStatus("已复制", time.Now().Add(time.Hour), statusOK)
				a.fb.retracting = state == "retracting"
			}
			if state == "capsule" && !a.capsuleUp(time.Now()) {
				t.Fatal("completion capsule fixture is not visible")
			}
			a.pinReturning, a.peekProgress = true, 1
			a.peeking = state != "capsule"
			w, h := a.peekSize()
			if w != float32(max(180, a.notchW))+dropPillExtraW || h != float32(max(32, a.notchH))+dropPillBody {
				t.Fatalf("wrong return shape: %.1fx%.1f", w, h)
			}
			view := ui.NewTester(a.view, panelWidth, panelHeight)
			view.Frame()
			if !view.HasText("松开放回") || view.HasText("Review Track") || view.HasText("已复制") || a.fx.peekKind != peekReturn {
				t.Fatalf("return invitation obscured: kind=%v texts=%v", a.fx.peekKind, view.Texts())
			}
		})
	}
}

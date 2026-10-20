//go:build darwin

package main

// The motion and completion-feedback rules of DESIGN.md 「动效」 and
// visual-v1 groups 1, 3 and 4: the springs, the frame that holds the
// opening's overshoot, the progressive blur's bands, and the notch capsule's
// states. The headless frames come from ui.Tester, so the springs are driven
// by a fake clock and every drawn frame can be checked.

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// motionShelf is a closed, empty shelf without a native window: state
// tests never reach the window.
func motionShelf() *shelf {
	return &shelf{
		notchX:       600,
		notchY:       0,
		notchW:       180,
		notchH:       32,
		x:            410,
		y:            0,
		h:            560,
		thumbnails:   map[string]*ui.Bitmap{},
		thumbPending: map[string]bool{},
		appNames:     map[string]string{},
	}
}

// motionWindow is motionShelf with a placeholder native window, so view()
// takes its production branch (the tester supplies the surface).
func motionWindow() *shelf {
	a := motionShelf()
	a.win = &mygo.Window{}
	return a
}

// TestOpeningIsElasticAndTheFrameNeverClips drives the opening frame by
// frame: the drawn shell must overshoot its final height (the spring's
// elasticity) and the window must hold every frame of that flight — the
// bottom corners are never cut — then settle back to the panel's size.
func TestOpeningIsElasticAndTheFrameNeverClips(t *testing.T) {
	a := motionWindow()
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame() // the idle frame the spring starts from
	a.visible = true
	target := float32(a.panelH() - 8)
	peak := float32(0)
	for i := range 120 {
		view.Frame()
		peak = max(peak, a.fx.h)
		f := a.desiredFrame()
		if f.Width < int(a.fx.w+0.5) || f.Height < int(a.fx.h+0.5) {
			t.Fatalf("frame %d: window %dx%d cuts the shell drawn at %.1fx%.1f", i, f.Width, f.Height, a.fx.w, a.fx.h)
		}
		if f.Y != a.y {
			t.Fatalf("frame %d: the window moved off the top edge: %+v", i, f)
		}
		if f.X != a.stripCenter()-f.Width/2 {
			t.Fatalf("frame %d: the shell is not centred in the window: %+v", i, f)
		}
		view.Advance(16 * time.Millisecond)
	}
	if a.fx.openMoving {
		t.Fatal("the opening spring never came to rest")
	}
	if peak < target*1.02 {
		t.Fatalf("the shell peaked at %.1f, only %.1f%% past the final %.1f: the opening has no bounce",
			peak, (peak/target-1)*100, target)
	}
	if got := a.fx.h; got > target+0.5 || got < target-2 {
		t.Fatalf("the opening settled at %.1f, not the panel's %.1f", got, target)
	}
	if got := a.desiredFrame().Width; got != panelWidth {
		t.Fatalf("the settled frame is %d wide, want the panel's %d", got, panelWidth)
	}
}

// TestClosingFoldsCrisp closes a settled panel frame by frame: the shell may
// only shrink — a bouncy collapse would visibly pop — and must reach the
// notch band without dipping below it.
func TestClosingFoldsCrisp(t *testing.T) {
	a := motionWindow()
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame()
	a.visible = true
	for range 120 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	base := a.fx.h
	a.visible = false
	prev := base
	for i := range 120 {
		view.Frame()
		if a.fx.h > prev+0.01 {
			t.Fatalf("frame %d: the closing shell grew from %.1f to %.1f", i, prev, a.fx.h)
		}
		if a.fx.h < 32-0.01 {
			t.Fatalf("frame %d: the shell folded to %.1f, below the notch band", i, a.fx.h)
		}
		prev = a.fx.h
		view.Advance(16 * time.Millisecond)
	}
	if prev > 32.5 {
		t.Fatalf("the closing shell stopped at %.1f, above the notch band", prev)
	}
}

// TestSpringsMatchTheDesign pins the two curves the panel must use: the
// opening overshoots (Bounce 0.28 over 380ms), the collapse and the
// capsule's retraction do not, and the peek shapes morph with a light
// bounce.
func TestSpringsMatchTheDesign(t *testing.T) {
	if openSpring.Duration != 0 || openSpring.Spring.Duration != 380*time.Millisecond || openSpring.Spring.Bounce != 0.28 {
		t.Fatalf("open spring = %+v, want the design's 380ms/Bounce 0.28", openSpring)
	}
	for name, s := range map[string]ui.Animation{
		"close": closeSpring, "capsule retract": retractSpring,
	} {
		if s.Spring.Bounce != 0 || s.Spring.Duration != 300*time.Millisecond {
			t.Fatalf("%s spring = %+v, want a 300ms spring that cannot overshoot", name, s)
		}
	}
	if peekSpring.Spring.Bounce != 0.15 || peekSpring.Spring.Duration != peekAnimation {
		t.Fatalf("peek spring = %+v, want %v at Bounce 0.15", peekSpring, peekAnimation)
	}
}

// TestOpenFrameRoomHoldsTheOvershoot is the frame arithmetic on its own:
// room for a 4% overshoot plus a tick of catch-up, never less.
func TestOpenFrameRoomHoldsTheOvershoot(t *testing.T) {
	for _, expH := range []float32{140, 328, 552} {
		w, h := openFrameRoom(expH)
		expW := float32(panelWidth - 16)
		if float64(w) < float64(expW)*(1+openOvershoot) {
			t.Fatalf("room %d px cannot hold a shell %.0f wide overshooting 4%%", w, expW)
		}
		if float64(h) < float64(expH)*(1+openOvershoot) {
			t.Fatalf("room %d px cannot hold a shell %.0f tall overshooting 4%%", h, expH)
		}
	}
}

// TestPeekStripHoldsFoldingShapes keeps the native strip as big as the
// shape it is folding back from: a capsule that just expired, or a player
// whose track stopped, must not be clipped on its way down.
func TestPeekStripHoldsFoldingShapes(t *testing.T) {
	a := motionShelf()
	a.fx = shellMotion{peek: true, w: 380, h: 54}
	if f := a.desiredFrame(); f.Width < 380 || f.Height < 54 {
		t.Fatalf("strip %+v cuts the folding shell 380x54", f)
	}
	// The capsule's own room, glow included, appears with it.
	a.fx = shellMotion{}
	a.toastStatus("已压缩 3 张", time.Now().Add(3*time.Second), false)
	if f := a.desiredFrame(); f.Width < fbMaxW || f.Height < 32+fbBody+fbGlowPad {
		t.Fatalf("strip %+v cannot hold the capsule and its glow", f)
	}
}

// TestScrollEdgesOnlyWhereThereIsMore: the melts appear only on an edge
// that still has content beyond it, and never for content that fits.
func TestScrollEdgesOnlyWhereThereIsMore(t *testing.T) {
	cases := []struct {
		name          string
		y, maxY, size float32
		top, bottom   bool
	}{
		{"fits", 0, 0, 200, false, false},
		{"start with more below", 0, 500, 200, false, true},
		{"scrolled", 120, 500, 200, true, true},
		{"at the end", 500, 500, 200, true, false},
		{"moved by a pixel", 0.2, 500, 200, false, true},
	}
	for _, c := range cases {
		st := &ui.ScrollState{Y: c.y, MaxY: c.maxY}
		top, bottom, viewH := scrollEdges(st, c.size)
		if top != c.top || bottom != c.bottom {
			t.Fatalf("%s: bands top=%v bottom=%v, want %v/%v", c.name, top, bottom, c.top, c.bottom)
		}
		if viewH <= 0 {
			t.Fatalf("%s: viewport height %.1f", c.name, viewH)
		}
	}
}

// TestEdgeFadeIsProgressive: the blur weight rises smoothly to the edge,
// is full at it, and is gone past the band.
func TestEdgeFadeIsProgressive(t *testing.T) {
	if got := edgeFade(0); got != 1 {
		t.Fatalf("at the edge the fade is %.2f, want 1", got)
	}
	if got := edgeFade(scrollEdgeH); got != 0 {
		t.Fatalf("past the band the fade is %.2f, want 0", got)
	}
	if got := edgeFade(scrollEdgeH + 10); got != 0 {
		t.Fatalf("well past the band the fade is %.2f, want 0", got)
	}
	prev := float32(1)
	for d := float32(0); d <= scrollEdgeH; d += 4 {
		got := edgeFade(d)
		if got > prev+1e-6 {
			t.Fatalf("the fade rose from %.3f to %.3f at %.0fpt", prev, got, d)
		}
		if got < 0 || got > 1 {
			t.Fatalf("fade %.2f out of range at %.0fpt", got, d)
		}
		prev = got
	}
	if mid := edgeFade(scrollEdgeH / 2); mid < 0.3 || mid > 0.7 {
		t.Fatalf("halfway into the band the fade is %.2f, want about half", mid)
	}
}

// TestRowsFadeOnlyInTheBand: only rows visible in the viewport and reaching
// into an active band lose their sharpness; a row scrolled out, or away from
// the edges, is left alone.
func TestRowsFadeOnlyInTheBand(t *testing.T) {
	const viewH = 300
	cases := []struct {
		name       string
		top        float32
		topBand    bool
		bottomBand bool
		want       float32
	}{
		{"first row under the top band", 0, true, false, 1},
		{"row inside the band", 10, true, false, 0.5},
		{"row past the band", 60, true, false, 0},
		{"row scrolled fully out", -46, true, true, 0},
		{"row straddling the top edge", -20, true, false, 1},
		{"last row at the bottom band", 254, false, true, 1},
		{"row above the bottom band", 200, false, true, 0},
		{"bands off", 0, false, false, 0},
	}
	for _, c := range cases {
		got := rowFade(c.top, clipRowH, viewH, c.topBand, c.bottomBand)
		switch {
		case c.want == 1 && got < 0.99:
			t.Fatalf("%s: weight %.2f, want the edge weight", c.name, got)
		case c.want == 0 && got != 0:
			t.Fatalf("%s: weight %.2f, want 0", c.name, got)
		case c.want > 0 && c.want < 1 && (got <= 0 || got >= 1):
			t.Fatalf("%s: weight %.2f, want a partial edge weight", c.name, got)
		}
	}
	// Progressive, not a step: a row a third into the band is dimmer than
	// one at its inner end and sharper than one at the edge.
	inner := rowFade(28, clipRowH, viewH, true, false)
	middle := rowFade(17, clipRowH, viewH, true, false)
	if !(inner < middle && middle < 1) {
		t.Fatalf("weights inner %.2f middle %.2f do not progress toward the edge", inner, middle)
	}
}

// TestCapsuleOnlyWhileThePanelIsClosed: the capsule belongs to the closed
// notch; opening, hovering and a file drag all take the peek shape back.
func TestCapsuleOnlyWhileThePanelIsClosed(t *testing.T) {
	now := time.Now()
	a := motionShelf()
	a.toastStatus("已压缩 3 张", now.Add(3*time.Second), false)
	if !a.capsuleUp(now) {
		t.Fatal("a closed panel with a fresh toast has no capsule")
	}
	a.visible = true
	if a.capsuleUp(now) {
		t.Fatal("an open panel must keep its toast at the bottom, not the notch")
	}
	a.visible = false
	a.peeking = true
	if a.capsuleUp(now) {
		t.Fatal("hover must take the peek shape back from the capsule")
	}
	a.peeking = false
	a.dragging = true
	if a.capsuleUp(now) {
		t.Fatal("an incoming file drag must take the peek shape back")
	}
	a.ownDrag = true
	if !a.capsuleUp(now) {
		t.Fatal("Shelf's own drag-out does not ask for the drop pill: the capsule stays")
	}
	a.ownDrag, a.dragging = false, false
	if a.capsuleUp(now.Add(fbExpand + fbHoldOK + time.Millisecond)) {
		t.Fatal("the capsule outlived its hold")
	}
}

// TestToastWhileClosedBecomesCapsule: a status posted with the panel closed
// is what the capsule says, in the status colour and for the design's time;
// with the panel open the bottom pill keeps it instead.
func TestToastWhileClosedBecomesCapsule(t *testing.T) {
	a := motionShelf()
	now := time.Now()
	a.toastStatus("已压缩 3 张", now.Add(3*time.Second), false)
	if a.fb.text != "已压缩 3 张" || a.fb.isErr {
		t.Fatalf("capsule = %q err=%v, want the success text", a.fb.text, a.fb.isErr)
	}
	if got := a.fb.color(); got != fbGreen {
		t.Fatalf("success colour %v, want %v", got, fbGreen)
	}
	if held := a.fb.until.Sub(a.fb.shownAt); held != fbExpand+fbHoldOK {
		t.Fatalf("success held %v, want %v", held, fbExpand+fbHoldOK)
	}
	a.toastStatus("压缩包已加密", now.Add(4*time.Second), true)
	if a.fb.color() != fbRed {
		t.Fatalf("failure colour %v, want %v", a.fb.color(), fbRed)
	}
	if held := a.fb.until.Sub(a.fb.shownAt); held != fbExpand+fbHoldErr {
		t.Fatalf("failure held %v, want %v", held, fbExpand+fbHoldErr)
	}
	// The panel open: no capsule, the toast layer's pill carries it.
	a.fb = feedback{}
	a.visible = true
	a.toastStatus("已放入", now.Add(3*time.Second), false)
	if a.fb.text != "" || a.fb.up(now) {
		t.Fatal("an open panel must not grow a capsule")
	}
	if a.status != "已放入" {
		t.Fatalf("the open panel's toast is %q", a.status)
	}
}

// TestLiveJobDrivesTheCapsuleAndHandsOverToItsVerdict: a closed panel while
// a conversion runs shows the job's progress in orange, never expires on its
// own, and the job's finishing toast replaces it with the verdict.
func TestLiveJobDrivesTheCapsuleAndHandsOverToItsVerdict(t *testing.T) {
	a := motionWindow()
	j := &convertJob{target: toJPEG, unit: "张", started: time.Now().Add(-time.Second)}
	j.total.Store(3)
	j.done.Store(1)
	a.conv = &converter{host: a, job: j}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame()
	if a.fb.text != "正在转成 JPEG · 2/3" || !a.fb.live {
		t.Fatalf("capsule = %q live=%v, want the job's progress", a.fb.text, a.fb.live)
	}
	if a.fb.color() != fbOrange {
		t.Fatalf("running colour %v, want %v", a.fb.color(), fbOrange)
	}
	if !a.fb.until.IsZero() {
		t.Fatal("a running job's capsule must not expire by itself")
	}
	if !view.HasText("正在转成 JPEG · 2/3") {
		t.Fatal("the closed notch does not show the running job")
	}
	// The job ends and its toast lands: the capsule turns into the verdict.
	a.conv.job = nil
	a.toastStatus("已转换 3 项 · 已选中，可直接拖出", time.Now().Add(3*time.Second), false)
	view.Frame()
	if a.fb.live || a.fb.text != "已转换 3 项 · 已选中，可直接拖出" {
		t.Fatalf("after the job the capsule is %q live=%v", a.fb.text, a.fb.live)
	}
	if a.fb.color() != fbGreen {
		t.Fatalf("verdict colour %v, want %v", a.fb.color(), fbGreen)
	}
	// And it goes away when the hold runs out.
	a.fb.shownAt = time.Now().Add(-fbExpand - fbHoldOK - time.Second)
	a.fb.until = time.Now().Add(-time.Second)
	view.Frame()
	if a.fb.drawn {
		t.Fatal("the capsule is still drawn past its hold")
	}
}

// TestCapsuleGeometry: as wide as the words, at least the notch plus 60,
// never wider than fbMaxW, and exactly the notch band plus 22 tall.
func TestCapsuleGeometry(t *testing.T) {
	f := feedback{text: "好"}
	if w, h := f.capsule(180, 32); w != 240 || h != 54 {
		t.Fatalf("a short text makes %0.fx%0.f, want the 240 minimum and 54 tall", w, h)
	}
	f.text = "已压缩 · 132 KB · 已选中，可直接拖出"
	w, h := f.capsule(180, 32)
	if w < 240 || w > fbMaxW {
		t.Fatalf("a normal text makes %0.f wide, want within 240…%d", w, fbMaxW)
	}
	if h != 54 {
		t.Fatalf("capsule height %.0f, want the notch band plus %d", h, fbBody)
	}
	f.pillW = 200
	if w, _ := f.capsule(180, 32); w != 200+fbDot+fbGap+2*fbPadX {
		t.Fatalf("a measured text makes %0.f wide, want its width plus the chrome", w)
	}
	f.pillW = 1000
	if w, _ := f.capsule(180, 32); w != fbMaxW {
		t.Fatalf("an overlong text makes %0.f wide, want the %d cap", w, fbMaxW)
	}
	f.pillW = 0
	f.text = "已压缩 3 张，2 张本来就小于目标 · 4.6 MB → 1.8 MB，另外还有一些没能压到 1 MB 以内"
	if w, _ := f.capsule(180, 32); w != fbMaxW {
		t.Fatalf("a long unmeasured text makes %0.f wide, want the %d cap", w, fbMaxW)
	}
}

// TestFailureShakesOnce: the failure's shake swings left first, decays, and
// is over within fbShake; successes and Reduce Motion never shake.
func TestFailureShakesOnce(t *testing.T) {
	now := time.Now()
	f := feedback{text: "失败", isErr: true, shownAt: now, until: now.Add(time.Hour)}
	if got := f.shake(now, false); got != 0 {
		t.Fatalf("the shake started before the capsule grew: %.2f", got)
	}
	first := f.shake(now.Add(fbExpand+fbShake/10), false)
	if first >= 0 || -first < fbShakeX*0.6 {
		t.Fatalf("first swing %.2f, want a strong left swing", first)
	}
	second := f.shake(now.Add(fbExpand+3*fbShake/10), false)
	if second <= 0 || second >= -first {
		t.Fatalf("second swing %.2f, want a weaker right swing than %.2f", second, -first)
	}
	if got := f.shake(now.Add(fbExpand+fbShake), false); got != 0 {
		t.Fatalf("the shake outlived its run: %.2f", got)
	}
	if got := f.shake(now.Add(fbExpand+fbShake/10), true); got != 0 {
		t.Fatal("Reduce Motion must not shake")
	}
	f.isErr = false
	if got := f.shake(now.Add(fbExpand+fbShake/10), false); got != 0 {
		t.Fatalf("a success shook: %.2f", got)
	}
}

// TestCapsuleClickExpandsThroughTheMonitor: with the window still letting
// clicks through, the global monitor is what expands the panel from the
// capsule; a press away from it must not.
func TestCapsuleClickExpandsThroughTheMonitor(t *testing.T) {
	a := motionShelf()
	a.toastStatus("已转换 1 项", time.Now().Add(3*time.Second), false)
	a.fb.drawn = true
	r := a.peekPillRect()
	if r.Width < 240 {
		t.Fatalf("the capsule's click box is only %d wide: %+v", r.Width, r)
	}
	a.mouseDown(false, mygo.Point{X: r.X + r.Width/2, Y: r.Y + r.Height - 6})
	if !a.visible {
		t.Fatal("a press on the capsule must expand the shelf")
	}
	b := motionShelf()
	b.toastStatus("已转换 1 项", time.Now().Add(3*time.Second), false)
	b.fb.drawn = true
	b.mouseDown(false, mygo.Point{X: b.notchX - 120, Y: b.notchH + 10})
	if b.visible {
		t.Fatal("a press beside the capsule must pass through")
	}
}

// TestCapsuleGlowBreathes: a running job's glow breathes between dim and
// full over fbBreathe; a verdict's glow is steady, and Reduce Motion keeps
// it steady too.
func TestCapsuleGlowBreathes(t *testing.T) {
	now := time.Unix(0, 0)
	live := feedback{text: "正在解压 · 1/3", live: true}
	var lo, hi float32 = 1, 0
	for i := range 40 {
		v := live.glow(now.Add(time.Duration(i)*fbBreathe/40), false)
		lo, hi = min(lo, v), max(hi, v)
	}
	if lo > 0.5 || hi < 0.9 {
		t.Fatalf("the breath swings %.2f…%.2f, want about 0.35…1", lo, hi)
	}
	if got := live.glow(now, true); got != 0.9 {
		t.Fatalf("Reduce Motion glow %.2f, want steady 0.9", got)
	}
	done := feedback{text: "已完成"}
	if got := done.glow(now, false); got != 0.9 {
		t.Fatalf("a verdict's glow %.2f, want steady 0.9", got)
	}
}

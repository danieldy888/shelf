//go:build darwin

package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/settings"
)

// fakeSnapWindow is a stand-in for the AX window a session follows.
type fakeSnapWindow struct {
	rect     mac.AXRect
	move     bool
	resize   bool
	released bool
	moves    []mac.AXRect
	resizes  []bool
	err      error
}

func (f *fakeSnapWindow) Valid() bool               { return !f.released }
func (f *fakeSnapWindow) Frame() (mac.AXRect, bool) { return f.rect, !f.released }
func (f *fakeSnapWindow) Settable() (bool, bool)    { return f.move, f.resize }
func (f *fakeSnapWindow) Release()                  { f.released = true }
func (f *fakeSnapWindow) MoveResize(r mac.AXRect, resize bool) error {
	f.moves = append(f.moves, r)
	f.resizes = append(f.resizes, resize)
	if f.err == nil && !f.released {
		f.rect = r
	}
	return f.err
}

// snapTestShelf is a closed shelf on a notched display.
func snapTestShelf() *shelf {
	return &shelf{
		notchX: 600, notchY: 0, notchW: 180, notchH: 32,
		x: 410, y: 0, level: 1,
		dispBounds:   mygo.Rectangle{X: 0, Y: 0, Width: 1470, Height: 956},
		settings:     settings.Default(),
		thumbnails:   map[string]*ui.Bitmap{},
		thumbPending: map[string]bool{},
	}
}

type snapSeams struct {
	down    bool
	calls   int
	fake    *fakeSnapWindow
	pid     int
	trusted bool
	own     bool
	work    mygo.Rectangle
}

func installSnapSeams(t *testing.T, s *snapSeams) {
	t.Helper()
	oldTrusted, oldPID, oldOwn := accessibilityProbe, snapOwnPID, snapOwnWindow
	oldAt, oldWork := snapAXWindowAt, snapWorkAreaAt
	oldDown := dragLeftDown
	t.Cleanup(func() {
		accessibilityProbe, snapOwnPID, snapOwnWindow = oldTrusted, oldPID, oldOwn
		snapAXWindowAt, snapWorkAreaAt = oldAt, oldWork
		dragLeftDown = oldDown
	})
	accessibilityProbe = func() bool { return s.trusted }
	snapOwnPID = func() int { return 999 }
	snapOwnWindow = func(*shelf, mac.AXRect) bool { return s.own }
	snapAXWindowAt = func(x, y float64) (snapWindow, int, bool) {
		s.calls++
		if s.fake == nil {
			return nil, 0, false
		}
		return s.fake, s.pid, true
	}
	snapWorkAreaAt = func(mygo.Point) mygo.Rectangle { return s.work }
	dragLeftDown = func() bool { return s.down }
}

func TestSnapTargetRegions(t *testing.T) {
	type tc struct {
		name string
		work mygo.Rectangle
		want []mygo.Rectangle
	}
	cases := []tc{
		{
			name: "bottom Dock",
			work: mygo.Rectangle{X: 0, Y: 25, Width: 1470, Height: 750},
			want: []mygo.Rectangle{
				{X: 0, Y: 25, Width: 735, Height: 750},
				{X: 0, Y: 25, Width: 1470, Height: 750},
				{X: 735, Y: 25, Width: 735, Height: 750},
				{X: 0, Y: 25, Width: 490, Height: 750},
				{X: 490, Y: 25, Width: 490, Height: 750},
				{X: 980, Y: 25, Width: 490, Height: 750},
			},
		},
		{
			name: "left Dock",
			work: mygo.Rectangle{X: 80, Y: 25, Width: 1390, Height: 900},
			want: []mygo.Rectangle{
				{X: 80, Y: 25, Width: 695, Height: 900},
				{X: 80, Y: 25, Width: 1390, Height: 900},
				{X: 775, Y: 25, Width: 695, Height: 900},
				{X: 80, Y: 25, Width: 463, Height: 900},
				{X: 543, Y: 25, Width: 463, Height: 900},
				{X: 1006, Y: 25, Width: 464, Height: 900},
			},
		},
		{
			name: "second display",
			work: mygo.Rectangle{X: 1512, Y: 0, Width: 1920, Height: 1080},
			want: []mygo.Rectangle{
				{X: 1512, Y: 0, Width: 960, Height: 1080},
				{X: 1512, Y: 0, Width: 1920, Height: 1080},
				{X: 2472, Y: 0, Width: 960, Height: 1080},
				{X: 1512, Y: 0, Width: 640, Height: 1080},
				{X: 2152, Y: 0, Width: 640, Height: 1080},
				{X: 2792, Y: 0, Width: 640, Height: 1080},
			},
		},
	}
	for _, c := range cases {
		for slot, want := range c.want {
			if got := snapSlotRect(c.work, slot); got != want {
				t.Errorf("%s slot %d = %+v, want %+v", c.name, slot, got, want)
			}
		}
		// The six targets tile the visible frame exactly.
		left, right := snapSlotRect(c.work, 0), snapSlotRect(c.work, 2)
		if left.Width+right.Width != c.work.Width || right.X != left.X+left.Width {
			t.Errorf("%s: halves do not meet: %+v %+v", c.name, left, right)
		}
		var sum int
		for slot := 3; slot < 6; slot++ {
			r := snapSlotRect(c.work, slot)
			if slot > 3 {
				if prev := snapSlotRect(c.work, slot-1); r.X != prev.X+prev.Width {
					t.Errorf("%s: third %d starts at %d, previous ends at %d", c.name, slot, r.X, prev.X+prev.Width)
				}
			}
			sum += r.Width
		}
		if sum != c.work.Width {
			t.Errorf("%s: thirds cover %d of %d", c.name, sum, c.work.Width)
		}
	}
	if got := snapSlotRect(mygo.Rectangle{X: 0, Y: 0, Width: 100, Height: 100}, -1); got != (mygo.Rectangle{}) {
		t.Fatalf("invalid slot = %+v, want zero", got)
	}
}

func TestSnapTriggerAndTiles(t *testing.T) {
	trigger := snapTriggerRect(mygo.Rectangle{X: 600, Y: 0, Width: 180, Height: 32})
	want := mygo.Rectangle{X: 592, Y: -8, Width: 196, Height: 88}
	if trigger != want {
		t.Fatalf("trigger = %+v, want %+v", trigger, want)
	}
	for _, p := range []mygo.Point{{X: 592, Y: 0}, {X: 787, Y: 79}, {X: 690, Y: 32}} {
		if !insideRect(p, trigger) {
			t.Fatalf("%+v should be in the trigger", p)
		}
	}
	for _, p := range []mygo.Point{{X: 591, Y: 0}, {X: 690, Y: 80}, {X: 789, Y: 10}} {
		if insideRect(p, trigger) {
			t.Fatalf("%+v should be outside the trigger", p)
		}
	}
	panel := snapPanelRect(690, 0)
	if panel != (mygo.Rectangle{X: 510, Y: 0, Width: 360, Height: 150}) {
		t.Fatalf("panel = %+v", panel)
	}
	wantFirst := mygo.Rectangle{X: 522, Y: 40, Width: 108, Height: 46}
	wantLast := mygo.Rectangle{X: 750, Y: 92, Width: 108, Height: 46}
	if got := snapTileRect(panel, 0); got != wantFirst {
		t.Fatalf("tile 0 = %+v, want %+v", got, wantFirst)
	}
	if got := snapTileRect(panel, 5); got != wantLast {
		t.Fatalf("tile 5 = %+v, want %+v", got, wantLast)
	}
	for slot := range snapTargets {
		r := snapTileRect(panel, slot)
		center := mygo.Point{X: r.X + r.Width/2, Y: r.Y + r.Height/2}
		if got := snapHotSlot(panel, center); got != slot {
			t.Fatalf("center of tile %d hits %d", slot, got)
		}
	}
	// The 6pt gaps and the title row are no tile.
	for _, p := range []mygo.Point{{X: 633, Y: 63}, {X: 525, Y: 20}, {X: 525, Y: 145}} {
		if got := snapHotSlot(panel, p); got != -1 {
			t.Fatalf("%+v hit tile %d, want none", p, got)
		}
	}
}

func TestSnapIsWindowDrag(t *testing.T) {
	cases := []struct {
		name       string
		trusted    bool
		pressed    bool
		movable    bool
		fileDrag   bool
		distance   float64
		frameMoved float64
		want       bool
	}{
		{"file drag never snaps", true, true, true, true, 100, 100, false},
		{"window moved", true, true, true, false, 40, 12, true},
		{"window not moving", true, true, true, false, 40, 0, false},
		{"press was not a window", true, false, false, false, 40, 12, false},
		{"immovable window", true, true, false, false, 40, 12, false},
		{"trusted, barely moved", true, true, true, false, 40, 1, false},
		{"no Accessibility, moved", false, false, false, false, 10, 0, true},
		{"no Accessibility, still", false, false, false, false, 1, 0, false},
	}
	for _, c := range cases {
		got := snapIsWindowDrag(c.trusted, c.pressed, c.movable, c.fileDrag, c.distance, c.frameMoved)
		if got != c.want {
			t.Errorf("%s = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSnapSessionArrangesAndCloses(t *testing.T) {
	a := snapTestShelf()
	f := &fakeSnapWindow{rect: mac.AXRect{X: 700, Y: 300, Width: 500, Height: 400}, move: true, resize: true}
	s := &snapSeams{fake: f, pid: 4242, trusted: true, down: true,
		work: mygo.Rectangle{X: 0, Y: 25, Width: 1470, Height: 750}}
	installSnapSeams(t, s)

	a.snapPress(false, 0, mygo.Point{X: 700, Y: 300})
	if !a.snap.armed || a.snap.win == nil {
		t.Fatal("a press on a movable window did not arm a session")
	}
	if s.calls != 1 {
		t.Fatalf("AX lookup ran %d times, want 1", s.calls)
	}
	// Still below the trigger: nothing opens yet.
	a.snapTick(time.Now(), mygo.Point{X: 700, Y: 300}, false)
	if a.visible {
		t.Fatal("the panel opened before the pointer reached the notch")
	}
	// The window follows the pointer; entering the trigger grows the panel.
	f.rect.X -= 30
	f.rect.Y -= 280
	a.snapTick(time.Now(), mygo.Point{X: 690, Y: 10}, false)
	if !a.snap.dragging || !a.visible || !a.snap.panel || !a.snap.drawn {
		t.Fatalf("entering the trigger: dragging=%v visible=%v panel=%v drawn=%v",
			a.snap.dragging, a.visible, a.snap.panel, a.snap.drawn)
	}
	// Hover 左半: the preview targets the visible frame's left half.
	f.rect.X -= 20
	a.snapTick(time.Now(), mygo.Point{X: 576, Y: 63}, false)
	if a.snap.hot != 0 {
		t.Fatalf("hot tile = %d, want 0", a.snap.hot)
	}
	if a.snap.ghost == nil {
		t.Fatal("no preview box")
	}
	wantGhost := mygo.Rectangle{X: 0, Y: 25, Width: 735, Height: 750}
	if a.snap.ghost.target != wantGhost {
		t.Fatalf("preview target = %+v, want %+v", a.snap.ghost.target, wantGhost)
	}
	// Release: the window is arranged and the panel folds away.
	s.down = false
	a.snapTick(time.Now(), mygo.Point{X: 576, Y: 63}, false)
	if len(f.moves) != 1 || f.moves[0] != (mac.AXRect{X: 0, Y: 25, Width: 735, Height: 750}) {
		t.Fatalf("window moves = %+v", f.moves)
	}
	if !f.resizes[0] {
		t.Fatal("the resizable window was only moved")
	}
	if a.visible || a.snap.panel || a.snap.armed || a.snap.ghost != nil {
		t.Fatalf("after release: visible=%v panel=%v armed=%v ghost=%v",
			a.visible, a.snap.panel, a.snap.armed, a.snap.ghost)
	}
	if !f.released {
		t.Fatal("the AX window reference leaked")
	}
}

func TestSnapOnlyMovesUnresizableWindow(t *testing.T) {
	a := snapTestShelf()
	f := &fakeSnapWindow{rect: mac.AXRect{X: 700, Y: 300, Width: 500, Height: 400}, move: true, resize: false}
	s := &snapSeams{fake: f, pid: 4242, trusted: true, down: true,
		work: mygo.Rectangle{X: 0, Y: 25, Width: 1470, Height: 750}}
	installSnapSeams(t, s)
	a.snapPress(false, 0, mygo.Point{X: 700, Y: 300})
	f.rect.X -= 60
	a.snapTick(time.Now(), mygo.Point{X: 690, Y: 10}, false)
	f.rect.X -= 20
	a.snapTick(time.Now(), mygo.Point{X: 576, Y: 63}, false)
	if a.snap.hot != 0 {
		t.Fatalf("hot tile = %d, want 0", a.snap.hot)
	}
	s.down = false
	a.snapTick(time.Now(), mygo.Point{X: 576, Y: 63}, false)
	if len(f.resizes) != 1 || f.resizes[0] {
		t.Fatalf("resize flags = %v, want one move-only call", f.resizes)
	}
	want := mac.AXRect{X: 0, Y: 25, Width: 735, Height: 750}
	if len(f.moves) != 1 || f.moves[0] != want {
		t.Fatalf("window moves = %+v, want %+v", f.moves, want)
	}
	if a.status != "这个窗口不能调整大小" {
		t.Fatalf("status = %q, want the resize notice", a.status)
	}
}

func TestSnapLeavingTriggerCancels(t *testing.T) {
	a := snapTestShelf()
	f := &fakeSnapWindow{rect: mac.AXRect{X: 700, Y: 300, Width: 500, Height: 400}, move: true, resize: true}
	s := &snapSeams{fake: f, pid: 4242, trusted: true, down: true,
		work: mygo.Rectangle{X: 0, Y: 25, Width: 1470, Height: 750}}
	installSnapSeams(t, s)

	a.snapPress(false, 0, mygo.Point{X: 700, Y: 300})
	f.rect.X -= 60
	a.snapTick(time.Now(), mygo.Point{X: 690, Y: 10}, false)
	if !a.visible || !a.snap.panel {
		t.Fatal("the panel did not open")
	}
	a.snapTick(time.Now(), mygo.Point{X: 1200, Y: 500}, false)
	if a.visible || a.snap.panel || a.snap.armed || a.snap.ghost != nil {
		t.Fatalf("leaving did not cancel: visible=%v panel=%v armed=%v",
			a.visible, a.snap.panel, a.snap.armed)
	}
	if len(f.moves) != 0 {
		t.Fatalf("cancel moved the window: %+v", f.moves)
	}
	if !f.released {
		t.Fatal("cancel leaked the AX window reference")
	}
}

func TestSnapFileDragNeverOpensPanel(t *testing.T) {
	a := snapTestShelf()
	f := &fakeSnapWindow{rect: mac.AXRect{X: 700, Y: 300, Width: 500, Height: 400}, move: true, resize: true}
	s := &snapSeams{fake: f, pid: 4242, trusted: true, down: true,
		work: mygo.Rectangle{X: 0, Y: 25, Width: 1470, Height: 750}}
	installSnapSeams(t, s)

	a.snapPress(false, 0, mygo.Point{X: 700, Y: 300})
	f.rect.X -= 60
	a.snapTick(time.Now(), mygo.Point{X: 690, Y: 10}, true)
	if a.visible || a.snap.panel || a.snap.armed {
		t.Fatalf("a file drag armed the snap panel: visible=%v panel=%v armed=%v", a.visible, a.snap.panel, a.snap.armed)
	}
	if !f.released {
		t.Fatal("the file drag leaked the AX reference")
	}
}

func TestSnapOwnWindowAndNoPermission(t *testing.T) {
	a := snapTestShelf()
	f := &fakeSnapWindow{rect: mac.AXRect{X: 700, Y: 300, Width: 500, Height: 400}, move: true, resize: true}
	s := &snapSeams{fake: f, pid: 999, trusted: true, down: true, own: true,
		work: mygo.Rectangle{X: 0, Y: 25, Width: 1470, Height: 750}}
	installSnapSeams(t, s)
	// ShelfU's own window: pid matches, the session must not arm.
	a.snapPress(false, 0, mygo.Point{X: 700, Y: 300})
	if a.snap.armed || a.snap.win != nil {
		t.Fatal("a press on ShelfU's own window armed a session")
	}
	if !f.released {
		t.Fatal("the rejected own window leaked")
	}
	// A normal window of the same process that is not one of ShelfU's own
	// interface windows is allowed (the smoke drag's case).
	s.own = false
	f = &fakeSnapWindow{rect: mac.AXRect{X: 700, Y: 300, Width: 500, Height: 400}, move: true, resize: true}
	s.fake = f
	a.snapPress(false, 0, mygo.Point{X: 700, Y: 300})
	if !a.snap.armed || a.snap.win == nil {
		t.Fatal("a normal same-process window was rejected as ShelfU's own")
	}
	a.endSnapSession(false)
	// A press that is not on a window at all is rejected too.
	s.fake = nil
	a.snapPress(false, 0, mygo.Point{X: 700, Y: 300})
	if a.snap.armed {
		t.Fatal("a press over nothing armed a session")
	}

	// Without Accessibility the panel still grows greyed out, and stays
	// after release so the 开启 row can be clicked.
	a = snapTestShelf()
	s = &snapSeams{trusted: false, down: true,
		work: mygo.Rectangle{X: 0, Y: 25, Width: 1470, Height: 750}}
	installSnapSeams(t, s)
	a.snapPress(false, 0, mygo.Point{X: 700, Y: 300})
	fake2 := &fakeSnapWindow{}
	s.fake = fake2
	if s.calls != 0 {
		t.Fatalf("AX lookup ran without permission")
	}
	a.snapTick(time.Now(), mygo.Point{X: 690, Y: 20}, false)
	if !a.visible || !a.snap.panel || a.snap.trusted {
		t.Fatalf("no-permission panel: visible=%v panel=%v trusted=%v", a.visible, a.snap.panel, a.snap.trusted)
	}
	if a.snap.hot != -1 || a.snap.ghost != nil {
		t.Fatalf("no-permission tiles must not light up: hot=%d ghost=%v", a.snap.hot, a.snap.ghost)
	}
	s.down = false
	a.snapTick(time.Now(), mygo.Point{X: 690, Y: 20}, false)
	if !a.visible || !a.snap.panel {
		t.Fatal("the permission panel closed on release")
	}
	if a.snap.armed || a.snap.win != nil {
		t.Fatal("the session stayed armed after release")
	}
	// A click elsewhere dismisses it, and the intent does not leak to a
	// later ordinary opening.
	a.mouseDown(false, 0, mygo.Point{X: 1000, Y: 500})
	if a.visible || a.snap.panel {
		t.Fatalf("outside click: visible=%v panel=%v", a.visible, a.snap.panel)
	}
	a.show(false)
	if a.snap.drawn {
		t.Fatal("the snap box survived into a later ordinary opening")
	}
}

func TestSnapSettingOffIgnoresPresses(t *testing.T) {
	a := snapTestShelf()
	a.settings.SnapWindows = false
	s := &snapSeams{fake: &fakeSnapWindow{move: true, resize: true}, pid: 4242, trusted: true, down: true,
		work: mygo.Rectangle{X: 0, Y: 25, Width: 1470, Height: 750}}
	installSnapSeams(t, s)
	a.snapPress(false, 0, mygo.Point{X: 700, Y: 300})
	if a.snap.armed || s.calls != 0 {
		t.Fatalf("the setting off still traced a window: armed=%v calls=%d", a.snap.armed, s.calls)
	}
}

func TestSnapPanelView(t *testing.T) {
	a := snapTestShelf()
	a.visible, a.snap.panel, a.snap.drawn, a.snap.trusted = true, true, true, true
	view := ui.NewTester(a.view, snapW, snapH)
	view.Frame()
	for _, text := range []string{"排列窗口", "左半", "全屏", "右半", "左 1/3", "中 1/3", "右 1/3"} {
		if !view.HasText(text) {
			t.Fatalf("panel is missing %q; texts %v", text, view.Texts())
		}
	}
	if view.HasText("需要「辅助功能」权限") {
		t.Fatal("the permission row showed with Accessibility granted")
	}
	// The first tile sits at the design's 12,40 (the shell starts at the
	// window's left edge here: 360 points wide). Find reports the tile row
	// that carries the label.
	box, ok := view.Find("左半")
	if !ok {
		t.Fatal("no 左半 box")
	}
	if box != (ui.Rect{X: 12, Y: 40, W: 108, H: 46}) {
		t.Fatalf("左半 tile box = %+v, want 12×40 108×46", box)
	}

	a.snap.trusted = false
	a.snap.hot = -1
	view.Frame()
	for _, text := range []string{"需要「辅助功能」权限", "开启"} {
		if !view.HasText(text) {
			t.Fatalf("permission row is missing %q; texts %v", text, view.Texts())
		}
	}
}

func TestSnapAnchorFollowsTheIsland(t *testing.T) {
	a := snapTestShelf()
	a.island, a.islandTop = true, 7 // no housing: 7pt below the top edge
	// The island's anchor is 120 × 30, centred on the same axis.
	if got, want := a.snapAnchor(), (mygo.Rectangle{X: 630, Y: 7, Width: 120, Height: 30}); got != want {
		t.Fatalf("island anchor = %+v, want %+v", got, want)
	}
	if got, want := snapTriggerRect(a.snapAnchor()), (mygo.Rectangle{X: 622, Y: -1, Width: 136, Height: 86}); got != want {
		t.Fatalf("island trigger = %+v, want %+v", got, want)
	}
	if got := snapPanelRect(a.stripCenter(), a.y+a.sheetTop()); got.Y != 7 {
		t.Fatalf("island panel top = %d, want 7", got.Y)
	}
	// A notched screen choosing 灵动岛 puts the capsule below the housing.
	a.islandTop = a.notchH + 6 // 38
	if got, want := a.snapAnchor(), (mygo.Rectangle{X: 630, Y: 38, Width: 120, Height: 30}); got != want {
		t.Fatalf("notched island anchor = %+v, want %+v", got, want)
	}
	if got, want := snapTriggerRect(a.snapAnchor()), (mygo.Rectangle{X: 622, Y: 30, Width: 136, Height: 86}); got != want {
		t.Fatalf("notched island trigger = %+v, want %+v", got, want)
	}
	if got := snapPanelRect(a.stripCenter(), a.y+a.sheetTop()); got.Y != 38 {
		t.Fatalf("notched island panel top = %d, want 38", got.Y)
	}
	// The panel itself stays 360 × 150 and its content draws unchanged.
	a.visible, a.snap.panel, a.snap.drawn, a.snap.trusted = true, true, true, true
	view := ui.NewTester(a.view, snapW, snapH)
	view.Frame()
	for _, text := range []string{"排列窗口", "左半", "右半"} {
		if !view.HasText(text) {
			t.Fatalf("island panel is missing %q; texts %v", text, view.Texts())
		}
	}
}

func TestSnapPanelUsesItsOwnBox(t *testing.T) {
	a := snapTestShelf()
	a.visible, a.snap.panel, a.snap.drawn, a.level = true, true, true, 1
	if w, h := a.panelBox(); w != snapW || h != snapH {
		t.Fatalf("panelBox = %g×%g, want %d×%d", w, h, snapW, snapH)
	}
	if a.panelH() != snapH {
		t.Fatalf("panelH = %d, want %d", a.panelH(), snapH)
	}
	w, h := a.shellSize(1, 0, 0, 0, 0)
	if int(w) != snapW || int(h) != snapH {
		t.Fatalf("shellSize = %g×%g, want %d×%d", w, h, snapW, snapH)
	}
	frame := a.desiredFrame()
	if frame.Width < snapW || frame.Height != snapH {
		t.Fatalf("open frame = %+v, want at least %d wide and snapH tall", frame, snapW)
	}
	a.snap.panel, a.snap.drawn, a.visible = false, false, false
	if w, h := a.panelBox(); w != islandW || h != islandH {
		t.Fatalf("ordinary island box = %g×%g", w, h)
	}
}

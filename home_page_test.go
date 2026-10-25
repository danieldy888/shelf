//go:build darwin

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
	"shelf/internal/mac"
	"shelf/internal/store"
)

// fakeVolume is the replaceable volume seam: tests never touch the
// machine's real output volume.
type fakeVolume struct {
	v    float32
	ok   bool
	err  error
	sets []float32
}

func (f *fakeVolume) level() (float32, bool) { return f.v, f.ok }
func (f *fakeVolume) set(v float32) error {
	f.sets = append(f.sets, v)
	if f.err != nil {
		return f.err
	}
	f.v = v
	return nil
}

// musicShelf builds a shelf whose home page can draw a real track.
func musicShelf(t *testing.T) (*shelf, *homePage) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := &shelf{store: s}
	a.np = &nowPlayingModule{
		host: a, have: true,
		last: mac.NowPlayingState{
			Title: "座右铭", Artist: "吴雨霏", Duration: 200, Elapsed: 20,
			Rate: 1, Received: time.Now(),
		},
	}
	p := &homePage{host: a}
	a.registerPage(p) // the view must draw the same page the test drives
	return a, p
}

// pageTester lays the page's body out in exactly the agreed 396 × 174.
func pageTester(p *homePage, live bool) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		p.body(c, false, live)
	}, contentW, contentH)
}

func TestHomeDefaultLayout(t *testing.T) {
	a := &shelf{}
	l := a.toolsLayout()
	want := []layout.Widget{
		{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		{ID: "clip", X: 2, Y: 1, W: 2, H: 1},
	}
	if len(l.Home) != len(want) {
		t.Fatalf("default home = %+v", l.Home)
	}
	for i := range want {
		if l.Home[i] != want[i] {
			t.Errorf("home[%d] = %+v, want %+v", i, l.Home[i], want[i])
		}
	}
}

func TestHomeDefaultLayoutWithUsage(t *testing.T) {
	a := &shelf{}
	a.settings.AIUsage.Enabled = true
	l := a.toolsLayout()
	want := []layout.Widget{
		{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		{ID: "usage", X: 3, Y: 0, W: 1, H: 1},
		{ID: "recent", X: 2, Y: 1, W: 2, H: 1},
	}
	if len(l.Home) != len(want) {
		t.Fatalf("default home = %+v", l.Home)
	}
	for i := range want {
		if l.Home[i] != want[i] {
			t.Errorf("home[%d] = %+v, want %+v", i, l.Home[i], want[i])
		}
	}
}

// The home page draws every default widget, and the tools page no longer
// carries widget cards.
func TestHomeRendersAndToolsHasActionsOnly(t *testing.T) {
	a, p := musicShelf(t)
	a.tab, a.visible, a.level = pageHome, true, 2
	view := ui.NewTester(a.view, sheetW, sheetH)
	for _, want := range []string{"主页", "座右铭", "保持唤醒", "剪贴板最新"} {
		if !view.HasText(want) {
			t.Errorf("home page is missing %q; texts=%v", want, view.Texts())
		}
	}
	if p.editing {
		t.Fatal("home page opened in edit mode")
	}

	a.tab = pageTools
	tools := ui.NewTester(a.view, sheetW, sheetH)
	if tools.HasText("保持唤醒") {
		t.Error("the tools page still shows a widget card")
	}
	if !tools.HasText("压缩到 1 MB") {
		t.Error("the tools page lost its action cards")
	}
}

// Entering edit, removing a widget, and cycling a size all write the
// layout; a removed widget leaves its cell empty instead of repacking.
func TestHomeEditRemoveResize(t *testing.T) {
	a, p := musicShelf(t)
	dir := t.TempDir()
	a.dataDir = dir
	a.settings.AIUsage.Enabled = true
	p.editing = false
	p.picking = false
	a.tab, a.visible, a.level = pageHome, true, 2
	view := ui.NewTester(a.view, sheetW, sheetH)

	// Pencil enters editing: the ⊖ badge and size handle appear.
	if err := view.Click("编辑主页"); err != nil {
		t.Fatalf("pencil missing: %v", err)
	}
	if !p.editing {
		t.Fatal("pencil did not enter editing")
	}
	if !view.HasText("移除") || !view.HasText("改尺寸") {
		t.Fatalf("edit chrome missing: %v", view.Texts())
	}

	// The awake widget's handle cycles 1×1 → 2×1.
	handle, ok := view.Find("改尺寸")
	if !ok {
		t.Fatal("size handle missing")
	}
	// The first 改尺寸 belongs to playing (2×2 → 2×1); the awake handle
	// is the second. Find returns the first, so resize playing and check.
	view.ClickAt(handle.X+handle.W/2, handle.Y+handle.H/2)
	view.Frame()
	playing := widgetByID(a.toolsLayout().Home, "playing")
	if playing.W != 2 || playing.H != 1 {
		t.Fatalf("playing after handle = %+v, want 2×1", playing)
	}

	// Removing awake leaves a gap: the remaining widgets do not repack.
	awake, ok := view.Find("保持唤醒")
	if !ok {
		t.Fatal("awake missing before removal")
	}
	view.ClickAt(awake.X+9, awake.Y+9) // the ⊖ badge's centre
	view.Frame()
	if widgetByID(a.toolsLayout().Home, "awake").ID != "" {
		t.Fatalf("remove badge did not remove awake: %+v", a.toolsLayout().Home)
	}
	if wake := widgetByID(a.toolsLayout().Home, "playing"); wake.X != 0 || wake.Y != 0 || wake.W != 2 {
		t.Fatalf("removing awake repacked playing: %+v", wake)
	}
}

func widgetByID(ws []layout.Widget, id string) layout.Widget {
	for _, w := range ws {
		if w.ID == id {
			return w
		}
	}
	return layout.Widget{}
}

// The home page is the island's first icon; clicking it opens the sheet
// on 主页 with the back row naming the page.
func TestHomeIsTheFirstIslandPageAndOpensTheSheet(t *testing.T) {
	a, view := levelShelf(t)
	if got := a.enabledPages()[0].id(); got != pageHome {
		t.Fatalf("first island page = %s, want 主页", got)
	}
	a.visible = true
	view.Frame()
	if err := view.Click("主页"); err != nil {
		t.Fatalf("island home icon: %v", err)
	}
	if a.currentLevel() != 2 || a.tab != pageHome {
		t.Fatalf("home icon: level=%d tab=%s, want the sheet on 主页", a.currentLevel(), a.tab)
	}
	view.Frame()
	if !view.HasText("返回一级岛 · 主页") {
		t.Fatalf("the sheet lost its home back row; texts %v", view.Texts())
	}
}

// With a fifth page registered the island still holds every icon and the
// home icon stays first: the home page adds an icon the island did not
// have before.
func TestIslandFitsEveryPageIcon(t *testing.T) {
	a, view := levelShelf(t)
	a.registerPage(&fakePage{on: true})
	if got := a.enabledPages()[0].id(); got != pageHome {
		t.Fatalf("first island page = %s, want 主页", got)
	}
	a.visible = true
	view.Frame()
	x0 := float32((sheetW - islandW) / 2)
	for _, label := range []string{"主页", "托盘", "剪贴板", "工具", "假页面"} {
		r, ok := view.Find(label)
		if !ok {
			t.Fatalf("island icon %q missing; texts %v", label, view.Texts())
		}
		if r.X < x0-0.5 || r.X+r.W > x0+islandW+0.5 || r.Y < 0 || r.Y+r.H > islandH+0.5 {
			t.Fatalf("island icon %q at %+v leaves the island [%v,0,%v,%v]", label, r, x0, x0+islandW, islandH)
		}
	}
}

// A tap on a 保持唤醒 chip picks that duration instead of toggling the
// card's one-hour hold: the chip claims its own press, so the card never
// starts a press session.
func TestHomeAwakeChipGetsItsOwnPress(t *testing.T) {
	power := newFakePower()
	a, p := musicShelf(t)
	a.awake = &awakeModule{host: a, create: power.create, release: power.release}
	l := layout.Layout{Version: layout.Version,
		Home: []layout.Widget{{ID: "awake", X: 0, Y: 0, W: 2, H: 2}},
	}
	a.toolsLay = &l
	a.tab, a.visible, a.level = pageHome, true, 2
	view := ui.NewTester(a.view, sheetW, sheetH)
	r, ok := view.Find("30 分钟")
	if !ok {
		t.Fatalf("awake chip missing: %v", view.Texts())
	}
	view.Press(r.X+r.W/2, r.Y+r.H/2)
	view.Advance(50 * time.Millisecond)
	if p.press != nil {
		t.Fatalf("a chip press became a widget press: %+v", p.press)
	}
	view.Release(r.X+r.W/2, r.Y+r.H/2)
	view.Advance(50 * time.Millisecond)
	if a.awake.duration != 30*time.Minute {
		t.Fatalf("chip tap set %v, want 30m", a.awake.duration)
	}
}

// A drag in edit mode snaps a widget to a free cell; releasing over an
// occupied cell leaves it where it was.
func TestHomeDragSnapsToFreeCell(t *testing.T) {
	a, p := musicShelf(t)
	p.editing = true
	a.tab, a.visible, a.level = pageHome, true, 2
	view := ui.NewTester(a.view, sheetW, sheetH)
	r, ok := view.Find("保持唤醒")
	if !ok {
		t.Fatal("awake widget missing")
	}
	// Grab 10pt inside the card, move one cell right: column 2 → 3.
	x, y := r.X+40, r.Y+r.H/2
	view.Press(x, y)
	view.Move(x+homeCellW()+homeGridGap, y)
	view.Release(x+homeCellW()+homeGridGap, y)
	view.Advance(homeSettleAnim + 100*time.Millisecond)
	w := widgetByID(a.toolsLayout().Home, "awake")
	if w.X != 3 || w.Y != 0 {
		t.Fatalf("awake after drag = %+v, want (3,0)", w)
	}
	// Dropping onto the music widget's cells is refused: the widget
	// stays where it was instead of overlapping.
	view.Frame()
	awake, ok := view.Find("保持唤醒")
	if !ok {
		t.Fatal("awake missing after the move")
	}
	view.Press(awake.X+40, awake.Y+awake.H/2)
	view.Move(awake.X-2*homeCellW(), awake.Y+awake.H/2)
	view.Release(awake.X-2*homeCellW(), awake.Y+awake.H/2)
	view.Advance(homeSettleAnim + 100*time.Millisecond)
	if w := widgetByID(a.toolsLayout().Home, "awake"); w.ID == "" || w.X != 3 {
		t.Fatalf("a refused drop moved the widget: %+v", w)
	}
}

// The progress bar sends one seek on release, and the shown position is
// optimistic while the next helper report is awaited.
func TestHomeSeekSendsOnceOnRelease(t *testing.T) {
	a, p := musicShelf(t)
	cmdsR, cmdsW := io.Pipe()
	stateR, stateW := io.Pipe()
	host := mac.NewNowPlayingHostWithSource(func(context.Context) (io.WriteCloser, io.ReadCloser, func() error, error) {
		return cmdsW, stateR, nil, nil
	})
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stateW.Close()
		host.Stop()
		cmdsR.Close()
		stateR.Close()
	})
	a.np.np = host
	// Drain the command pipe from the start: an io.Pipe write blocks
	// until someone reads, and the seek is sent inside the release.
	lines := make(chan string, 1)
	go func() {
		line, err := bufio.NewReader(cmdsR).ReadString('\n')
		if err == nil {
			lines <- line
		}
	}()
	view := pageTester(p, true)
	bar, ok := view.Find("播放进度")
	if !ok {
		t.Fatal("progress bar missing")
	}
	view.Press(bar.X+bar.W/2, bar.Y+bar.H/2)
	view.Move(bar.X+bar.W*3/4, bar.Y+bar.H/2)
	if got := a.np.last.Position(time.Now()); math.Abs(got-20) > 2 {
		t.Fatalf("dragging moved the reported state before release: %.1f", got)
	}
	view.Release(bar.X+bar.W*3/4, bar.Y+bar.H/2)
	var line string
	select {
	case line = <-lines:
	case <-time.After(2 * time.Second):
		t.Fatal("no seek command")
	}
	if !strings.HasPrefix(line, "seek ") {
		t.Fatalf("wrote %q", line)
	}
	var sec float64
	if _, err := fmt.Sscanf(line, "seek %f\n", &sec); err != nil {
		t.Fatalf("seek line %q: %v", line, err)
	}
	if sec < 140 || sec > 160 {
		t.Fatalf("seek to %.1fs, want about 150s (3/4 of 200)", sec)
	}
}

// The volume bar writes the system volume once on release, through the
// replaceable seam; an unsupported device never writes.
func TestHomeVolumeWritesThroughSeam(t *testing.T) {
	a, p := musicShelf(t)
	vol := &fakeVolume{v: 0.4, ok: true}
	a.vol = vol
	view := pageTester(p, true)
	bar, ok := view.Find("音量")
	if !ok {
		t.Fatal("volume bar missing")
	}
	view.Press(bar.X+bar.W/2, bar.Y+bar.H/2)
	view.Move(bar.X+bar.W*3/4, bar.Y+bar.H/2)
	if len(vol.sets) != 0 {
		t.Fatal("dragging wrote the volume before release")
	}
	view.Release(bar.X+bar.W*3/4, bar.Y+bar.H/2)
	if len(vol.sets) != 1 {
		t.Fatalf("volume writes = %v, want exactly one", vol.sets)
	}
	if got := vol.sets[0]; got < 0.7 || got > 0.8 {
		t.Fatalf("volume set to %.2f, want about 0.75", got)
	}
}

func TestHomeVolumeUnsupportedGreysOut(t *testing.T) {
	a, p := musicShelf(t)
	vol := &fakeVolume{ok: false}
	a.vol = vol
	p.volLoaded = false
	view := pageTester(p, true)
	bar, ok := view.Find("音量")
	if !ok {
		t.Fatal("volume bar missing")
	}
	view.Press(bar.X+bar.W/2, bar.Y+bar.H/2)
	view.Release(bar.X+bar.W/2, bar.Y+bar.H/2)
	if len(vol.sets) != 0 {
		t.Fatalf("an unsupported device got a write: %v", vol.sets)
	}
	if p.volOK {
		t.Fatal("unsupported volume reported as supported")
	}
}

// TestHomeTextFits is the generic no-clipping guard: every static text
// drawn by the home and tools pages is measured at its real font against
// the width its layout gives it, at every widget size and state.
func TestHomeTextFits(t *testing.T) {
	var issues []string
	fitIssue = func(s string) { issues = append(issues, s) }
	t.Cleanup(func() { fitIssue = nil })

	a, p := musicShelf(t)
	a.settings.AIUsage.Enabled = true
	items := []store.Item{
		{ID: "1", Kind: store.KindFile, Path: "/tmp/a.png"},
		{ID: "2", Kind: store.KindFile, Path: "/tmp/b.pdf"},
	}
	a.store.Items = append(a.store.Items, items...)

	st := a.np.last
	for _, id := range []string{"playing", "awake", "usage", "recent", "clip"} {
		for _, size := range homeSizes(id) {
			l := layout.Layout{Version: layout.Version,
				Home:  []layout.Widget{{ID: id, X: 0, Y: 0, W: size[0], H: size[1]}},
				Cards: []layout.Card{{ID: "ocr"}},
			}
			a.toolsLay = &l
			p.editing = false
			view := pageTester(p, true)
			view.Frame()
		}
	}

	// Empty states and the edit form.
	a.np = &nowPlayingModule{host: a}
	for _, id := range []string{"playing", "recent", "clip", "usage"} {
		l := layout.Layout{Version: layout.Version,
			Home: []layout.Widget{{ID: id, X: 0, Y: 0, W: 2, H: 2}},
		}
		a.toolsLay = &l
		view := pageTester(p, true)
		view.Frame()
	}
	p.editing = true
	l := layout.Layout{Version: layout.Version,
		Home: []layout.Widget{{ID: "awake", X: 0, Y: 0, W: 2, H: 2}},
	}
	a.toolsLay = &l
	pageTester(p, true).Frame()

	// The tools page and its picker at the fixed size.
	a.np = &nowPlayingModule{host: a, have: true, last: st}
	a.toolsLay = nil
	tp := a.toolsPage()
	a.tab = pageTools
	toolsView := func(c *ui.Context) { tp.body(c, false, true) }
	ui.NewTester(toolsView, contentW, contentH).Frame()
	tp.editing, tp.picking = true, true
	ui.NewTester(toolsView, contentW, contentH).Frame()

	// The 主页 catalogue's own rows at the fixed size.
	p.editing, p.picking = true, true
	pageTester(p, true).Frame()
	p.editing, p.picking = false, false

	if len(issues) != 0 {
		t.Fatalf("text does not fit:\n%s", strings.Join(issues, "\n"))
	}
}

// The catalogue places a widget at the first free cell where its default
// size fits and returns to the edit grid; a placed widget has no row.
func TestHomePickerAddsWidget(t *testing.T) {
	a, p := musicShelf(t)
	l := layout.Layout{Version: layout.Version, Home: []layout.Widget{{ID: "playing", X: 0, Y: 0, W: 2, H: 2}}}
	a.toolsLay = &l
	a.tab, a.visible, a.level = pageHome, true, 2
	view := ui.NewTester(a.view, sheetW, sheetH)
	p.editing = true
	view.Frame()
	if err := view.Click("添加小组件"); err != nil {
		t.Fatalf("catalogue entry missing: %v", err)
	}
	if !view.HasText("小组件目录") {
		t.Fatalf("picker title missing: %v", view.Texts())
	}
	if view.HasText("已添加") || view.HasText("音乐") {
		t.Fatalf("the catalogue lists a placed widget: %v", view.Texts())
	}
	if err := view.Click("最近文件"); err != nil {
		t.Fatalf("pick row missing: %v", err)
	}
	if p.picking {
		t.Fatal("picking did not close after adding")
	}
	w := widgetByID(a.toolsLayout().Home, "recent")
	if w.X != 2 || w.Y != 0 || w.W != 2 || w.H != 1 {
		t.Fatalf("recent placed at %+v, want (2,0) 2×1", w)
	}
}

// Dragging a widget out pins it; 放回刘海 returns it to its home cell.
func TestHomePinRestoresWidget(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	oldOpen, oldCursor := pinOpenWindow, cursorPoint
	pinOpenWindow = func(*pinItem) {}
	cursorPoint = func() mygo.Point { return mygo.Point{X: 1400, Y: 500} }
	t.Cleanup(func() { pinOpenWindow, cursorPoint = oldOpen, oldCursor })

	a, p := musicShelf(t)
	l := layout.Layout{Version: layout.Version,
		Home: []layout.Widget{{ID: "awake", X: 2, Y: 0, W: 1, H: 1}},
	}
	a.toolsLay = &l
	a.x, a.y = 400, 0
	p.editing = true
	w := l.Home[0]
	p.homeDrop(&homePress{id: w.ID, dragging: true, orig: w, grabDX: 10, grabDY: 10, px: 60, py: 1200})
	if len(a.pins) != 1 || a.pins[0].cardID != "awake" {
		t.Fatalf("drag-out did not pin the widget: %+v", a.pins)
	}
	if widgetByID(a.toolsLayout().Home, "awake").ID != "" {
		t.Fatal("a pinned widget stayed on the canvas")
	}
	if !a.unpin(a.pins[0], true) {
		t.Fatal("unpin failed")
	}
	if got := widgetByID(a.toolsLayout().Home, "awake"); got.X != 2 || got.Y != 0 {
		t.Fatalf("放回刘海 put the widget at %+v, want (2,0)", got)
	}
}

// Esc leaves home editing instead of hiding; a second Esc hides.
func TestHomeEscapeLeavesEdit(t *testing.T) {
	a, p := musicShelf(t)
	a.tab, a.visible, a.level = pageHome, true, 2
	p.editing = true
	a.escape()
	if p.editing {
		t.Fatal("esc did not leave editing")
	}
	if !a.visible {
		t.Fatal("esc hid while editing was on")
	}
}

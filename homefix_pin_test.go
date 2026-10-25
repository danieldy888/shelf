//go:build darwin

package main

// Regression tests for the desktop-pin review: 放回刘海 returning a widget
// to its cell after a restart, a canvas without room keeping the pin, and
// a pinned widget's content never being clipped.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
	"shelf/internal/store"
)

// pinCanvasDrag stubs the seams a canvas drag-out needs: the display set,
// the window opener, and a release point on the desktop, clear of the
// panel and of both snap edges.
func pinCanvasDrag(t *testing.T) {
	t.Helper()
	withPinDisplays(t, pinTestDisplays)
	oldOpen, oldCursor := pinOpenWindow, cursorPoint
	pinOpenWindow = func(*pinItem) {}
	cursorPoint = func() mygo.Point { return mygo.Point{X: 1400, Y: 500} }
	t.Cleanup(func() { pinOpenWindow, cursorPoint = oldOpen, oldCursor })
}

// pinFixture fills the tray and clipboard the widget cards read.
func pinFixture(t *testing.T, a *shelf) {
	t.Helper()
	a.store.Items = append(a.store.Items,
		store.Item{ID: "r1", Kind: store.KindFile, Path: filepath.Join(t.TempDir(), "alpha.png")},
		store.Item{ID: "r2", Kind: store.KindFile, Path: filepath.Join(t.TempDir(), "bravo.pdf")},
		store.Item{ID: "r3", Kind: store.KindFile, Path: filepath.Join(t.TempDir(), "charlie.txt")},
		store.Item{ID: "c1", Kind: store.KindText, Text: "剪贴板里的一段很长的文字", Source: store.SourceClipboard},
	)
}

// dropPin drags the only widget on the canvas out of the panel and returns
// the shelf and the pin it created.
func dropPin(t *testing.T, rect layout.Widget) (*shelf, *pinItem) {
	t.Helper()
	a, p := musicShelf(t)
	a.dataDir = t.TempDir()
	pinFixture(t, a)
	homeReviewLayout(a, rect)
	a.x, a.y = 400, 0
	p.editing = true
	p.homeDrop(&homePress{id: rect.ID, dragging: true, orig: rect, grabDX: 10, grabDY: 10, px: 60, py: 1200})
	if len(a.pins) != 1 || a.pins[0].cardID != rect.ID {
		t.Fatalf("drag-out did not pin %s: %+v", rect.ID, a.pins)
	}
	return a, a.pins[0]
}

// TestPinKeepsWidgetCellAcrossRestart is review #3: the pin stores the
// widget's canvas rect, so 放回刘海 after a restart puts it back in its
// cell and size instead of the top-left corner at the catalogue size.
func TestPinKeepsWidgetCellAcrossRestart(t *testing.T) {
	pinCanvasDrag(t)
	a, _ := dropPin(t, layout.Widget{ID: "usage", X: 3, Y: 0, W: 1, H: 1})
	if got := widgetByID(a.toolsLayout().Home, "usage"); got.ID != "" {
		t.Fatalf("the pinned widget stayed on the canvas: %+v", got)
	}
	raw, err := os.ReadFile(filepath.Join(a.dataDir, layout.FileName))
	if err != nil {
		t.Fatal(err)
	}
	// The rect is in the file, not only in memory: a restart reads it.
	if !strings.Contains(string(raw), `"homeX": 3`) || !strings.Contains(string(raw), `"homeW": 1`) {
		t.Fatalf("layout.json does not record the canvas rect:\n%s", raw)
	}

	// Restart: a fresh shelf on the same data directory.
	b := &shelf{dataDir: a.dataDir}
	b.pinRestore()
	if len(b.pins) != 1 {
		t.Fatalf("restore lost the pin: %+v", b.pins)
	}
	if !b.unpin(b.pins[0], true) {
		t.Fatal("放回刘海 failed after the restart")
	}
	if got := widgetByID(b.toolsLayout().Home, "usage"); got.X != 3 || got.Y != 0 || got.W != 1 || got.H != 1 {
		t.Fatalf("放回刘海 put the widget at %+v, want (3,0) 1×1", got)
	}
}

// TestPinWithoutSlotReturnsToCatalogueCell is the fallback half of review
// #3: a pin saved before the rect was recorded goes to the catalogue's
// cell for its size, not the top-left corner.
func TestPinWithoutSlotReturnsToCatalogueCell(t *testing.T) {
	pinCanvasDrag(t)
	a := &shelf{dataDir: t.TempDir()}
	a.mutateTools(func(l *layout.Layout) {
		l.Home = []layout.Widget{{ID: "awake", X: 2, Y: 0, W: 1, H: 1}}
		l.AddPin(layout.Pin{ID: "recent", Span: 2, Display: "1"})
	})
	a.pinRestore()
	if len(a.pins) != 1 {
		t.Fatalf("restore lost the pin: %+v", a.pins)
	}
	if !a.unpin(a.pins[0], true) {
		t.Fatal("放回刘海 failed")
	}
	// 最近文件's catalogue rect is (2,1) 2×1; (0,0) is free too, so a
	// top-left placement would show the slot was guessed.
	if got := widgetByID(a.toolsLayout().Home, "recent"); got.X != 2 || got.Y != 1 || got.W != 2 || got.H != 1 {
		t.Fatalf("recent returned at %+v, want its catalogue cell (2,1) 2×1", got)
	}
}

// TestPinPutBackKeepsTheRecordedSize is the re-review's second item: when
// the cell a widget was pinned from is taken, a free cell of the size it
// had keeps the widget whole; the catalogue size is only the last resort
// before 主页没有空位.
func TestPinPutBackKeepsTheRecordedSize(t *testing.T) {
	pinCanvasDrag(t)
	a, it := dropPin(t, layout.Widget{ID: "usage", X: 2, Y: 0, W: 2, H: 1})
	// Its cell is filled while the widget is on the desktop; the bottom
	// row's 2×1 stays free.
	a.settings.AIUsage.Enabled = true
	l := a.toolsLayout()
	l.Home = []layout.Widget{
		{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		{ID: "clip", X: 2, Y: 0, W: 2, H: 1},
	}
	a.toolsLay = &l
	if !a.unpin(it, true) {
		t.Fatal("放回刘海 failed")
	}
	if got := widgetByID(a.toolsLayout().Home, "usage"); got.X != 2 || got.Y != 1 || got.W != 2 || got.H != 1 {
		t.Fatalf("usage returned at %+v, want the free 2×1 at (2,1)", got)
	}
}

// TestPinStaysWhenHomeIsFull is review #4: with every cell taken, 放回刘海
// keeps the pin and its window and asks for a free cell, instead of
// deleting the card from the desktop.
func TestPinStaysWhenHomeIsFull(t *testing.T) {
	pinCanvasDrag(t)
	a, it := dropPin(t, layout.Widget{ID: "clip", X: 0, Y: 0, W: 2, H: 1})
	a.settings.AIUsage.Enabled = true
	homeReviewLayout(a,
		layout.Widget{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		layout.Widget{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		layout.Widget{ID: "usage", X: 3, Y: 0, W: 1, H: 1},
		layout.Widget{ID: "recent", X: 2, Y: 1, W: 2, H: 1},
	)
	// The pin itself has to be in the file for the check to mean anything.
	a.mutateTools(func(l *layout.Layout) {
		l.Pins = []layout.Pin{{ID: "clip", Span: 2, Display: "1"}}
	})
	before := len(a.toolsLayout().Home)
	if a.unpin(it, true) {
		t.Fatal("放回刘海 reported success with no free cell")
	}
	if len(a.pins) != 1 || a.pins[0] != it {
		t.Fatalf("the pin left the desktop: %+v", a.pins)
	}
	if len(a.toolsLayout().Home) != before {
		t.Fatalf("the canvas changed without room: %+v", a.toolsLayout().Home)
	}
	if got := pinIDsOf(a.toolsLayout().Pins); len(got) != 1 || got[0] != "clip" {
		t.Fatalf("the pin left layout.json: %v", got)
	}
	if a.status != "主页没有空位，先移除一个小组件" {
		t.Fatalf("hint = %q, want 主页没有空位，先移除一个小组件", a.status)
	}
	if it.wantKill {
		t.Fatal("the failed menu action will be retried every tick")
	}
}

// pinIDsOf lists the pinned ids.
func pinIDsOf(ps []layout.Pin) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}

// pinLine is one word a pinned card draws: the font it uses, or a minimum
// box for a control whose label is not drawn. min is 0 when the natural
// text width is the floor.
type pinLine struct {
	text   string
	size   float32
	weight int
	min    float32
}

// pinLines lists what each widget's desktop card must show whole.
func pinLines(a *shelf, id string) []pinLine {
	switch id {
	case "playing":
		lines := []pinLine{{text: "座右铭", size: 12, weight: 600},
			{text: "上一首", min: 15}, {text: "播放", min: 15}, {text: "下一首", min: 15}}
		if st, ok := a.np.track(); ok {
			if meta := a.np.cardMeta(st); meta != "" {
				lines = append(lines, pinLine{text: meta, size: 10})
			}
		}
		return lines
	case "recent":
		return []pinLine{
			{text: "最近文件", size: 12, weight: 600},
			{text: "拖出即可", size: 10},
			{text: "alpha.png", min: 40}, {text: "bravo.pdf", min: 40}, {text: "charlie.txt", min: 40},
		}
	case "clip":
		return []pinLine{{text: "剪贴板里的一段很长的文字", size: 12, weight: 600}, {text: "点按复制", size: 10}}
	case "awake":
		return []pinLine{{text: "保持唤醒", size: 12, weight: 600}, {text: "关", size: 10}}
	case "usage":
		return []pinLine{{text: "AI 用量", size: 12, weight: 600}, {text: "暂无数据", size: 10}}
	}
	return nil
}

// pinClipped renders the card body a pin window shows at width w and
// returns what does not fit whole: a word drawn narrower than its text, or
// a control squashed below its size.
func pinClipped(t *testing.T, a *shelf, id string, span int, w float32) []string {
	t.Helper()
	view := ui.NewTester(func(c *ui.Context) {
		a.toolCard(c, layout.Card{ID: id, Span: span}, cardDesktop).Fill()
	}, int(w), pinCardH)
	var bad []string
	for _, line := range pinLines(a, id) {
		r, ok := view.Find(line.text)
		if !ok {
			bad = append(bad, fmt.Sprintf("%q is not drawn", line.text))
			continue
		}
		need := line.min
		if need == 0 {
			need = measureHomeText(t, line.text, line.size, line.weight)
		}
		if r.W+0.5 < need {
			bad = append(bad, fmt.Sprintf("%q is %.1fpt of %.1fpt", line.text, r.W, need))
		}
	}
	return bad
}

// TestPinnedWidgetContentIsWhole is review #5: a widget pinned from the
// canvas draws its whole body inside the pin window at every canvas size it
// can have. The measurement is the card's own text and controls: a
// one-column pin is shown to cut the music, recent-file and clipboard
// bodies, and the pinned widths must not.
func TestPinnedWidgetContentIsWhole(t *testing.T) {
	pinCanvasDrag(t)
	for _, id := range []string{"playing", "awake", "usage", "recent", "clip"} {
		for _, size := range homeSizes(id) {
			name := fmt.Sprintf("%s %dx%d", id, size[0], size[1])
			t.Run(name, func(t *testing.T) {
				a, it := dropPin(t, layout.Widget{ID: id, X: 0, Y: 0, W: size[0], H: size[1]})
				if bad := pinClipped(t, a, id, it.span, float32(it.cardW)); len(bad) > 0 {
					t.Fatalf("the %dpt pinned card cuts its content: %s", it.cardW, strings.Join(bad, "; "))
				}
			})
		}
	}

	// Why three widgets pin wide: at one column their bodies are cut.
	a, _ := musicShelf(t)
	pinFixture(t, a)
	for _, id := range []string{"playing", "recent", "clip"} {
		if bad := pinClipped(t, a, id, 1, pinCardW); len(bad) == 0 {
			t.Errorf("%s is whole in a one-column pin, so a wide pin is not needed", id)
		}
		if bad := pinClipped(t, a, id, 2, pinCardW2); len(bad) > 0 {
			t.Errorf("%s is cut in a two-column pin: %s", id, strings.Join(bad, "; "))
		}
	}
	// 保持唤醒 and AI 用量 fit one column: no wide pin for them.
	for _, id := range []string{"awake", "usage"} {
		if bad := pinClipped(t, a, id, 1, pinCardW); len(bad) > 0 {
			t.Errorf("%s does not fit a one-column pin: %s", id, strings.Join(bad, "; "))
		}
	}
}

//go:build darwin

package main

// Regression tests for the 主页 canvas review: resizing past a size that
// does not fit, a light drag never becoming a tap, a switched-off widget
// leaving its cells free, the 2×2 player's clock labels, and the volume
// bar following the output device.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
	"shelf/internal/mac"
	"shelf/internal/store"
)

// homeReviewLayout installs a canvas and returns it.
func homeReviewLayout(a *shelf, ws ...layout.Widget) layout.Layout {
	l := layout.Layout{Version: layout.Version, Home: ws}
	a.toolsLay = &l
	return l
}

// TestHomeResizeSkipsSizesThatDoNotFit is review #1's first half: the
// corner handle keeps trying the following sizes when one is refused, so
// 最近文件 on the bottom row reaches 1×1 behind a 2×2 that leaves the grid.
func TestHomeResizeSkipsSizesThatDoNotFit(t *testing.T) {
	a, p := musicShelf(t)
	a.settings.AIUsage.Enabled = true
	a.dataDir = t.TempDir()
	homeReviewLayout(a,
		layout.Widget{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		layout.Widget{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		layout.Widget{ID: "usage", X: 3, Y: 0, W: 1, H: 1},
		layout.Widget{ID: "recent", X: 2, Y: 1, W: 2, H: 1},
	)
	p.cycleSize(layout.Widget{ID: "recent", X: 2, Y: 1, W: 2, H: 1})
	got := widgetByID(a.toolsLayout().Home, "recent")
	if got.W != 1 || got.H != 1 || got.X != 2 || got.Y != 1 {
		t.Fatalf("recent after the handle = %+v, want 1×1 at (2,1): the refused 2×2 ended the cycle", got)
	}
	if disk := widgetByID(layoutOf(t, a.dataDir).Home, "recent"); disk.W != 1 || disk.H != 1 {
		t.Fatalf("layout.json kept %+v, want the 1×1 that was applied", disk)
	}
}

// TestHomeResizeWritesNothingWhenNoSizeFits is review #1's second half: a
// widget whose every other size is blocked keeps its file untouched — the
// same layout must not be rewritten.
func TestHomeResizeWritesNothingWhenNoSizeFits(t *testing.T) {
	a, p := musicShelf(t)
	a.settings.AIUsage.Enabled = true
	a.dataDir = t.TempDir()
	homeReviewLayout(a,
		layout.Widget{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		layout.Widget{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		layout.Widget{ID: "usage", X: 3, Y: 0, W: 1, H: 1},
		layout.Widget{ID: "recent", X: 2, Y: 1, W: 2, H: 1},
	)
	file := filepath.Join(a.dataDir, layout.FileName)
	if err := layout.Write(a.dataDir, a.toolsLayout()); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	// 保持唤醒 at (2,0): 2×1 would land on usage, 2×2 would too.
	p.cycleSize(layout.Widget{ID: "awake", X: 2, Y: 0, W: 1, H: 1})
	if got := widgetByID(a.toolsLayout().Home, "awake"); got.W != 1 || got.H != 1 {
		t.Fatalf("a blocked resize changed the widget: %+v", got)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatalf("a resize that fit nothing rewrote layout.json (mtime %v, want %v)", info.ModTime(), old)
	}
}

// TestHomeMovedPressIsNotATap is review #2: a press that moves past the
// 6pt slop and comes back is not a tap — it must not toggle playback,
// switch 保持唤醒, or copy the clipboard item.
func TestHomeMovedPressIsNotATap(t *testing.T) {
	drag := func(view *ui.Tester, r ui.Rect) {
		t.Helper()
		x, y := r.X+20, r.Y+22
		view.Press(x, y)
		view.Move(x+20, y+20)
		view.Release(x+20, y+20)
		view.Frame()
	}
	tap := func(view *ui.Tester, r ui.Rect) {
		t.Helper()
		view.Press(r.X+20, r.Y+22)
		view.Release(r.X+20, r.Y+22)
		view.Frame()
	}

	t.Run("music", func(t *testing.T) {
		a, p := musicShelf(t)
		// A host that never ran: Send is a no-op, and the module still
		// applies the play/pause it sent to its own state.
		a.np.np = mac.NewNowPlayingHost()
		homeReviewLayout(a, layout.Widget{ID: "playing", X: 0, Y: 0, W: 2, H: 2})
		view := pageTester(p, true)
		card, ok := view.Find("音乐")
		if !ok {
			t.Fatalf("music card missing: %v", view.Texts())
		}
		tap(view, card)
		if !a.np.last.Playing {
			t.Fatal("fixture: a clean tap did not start playback")
		}
		a.np.last.Playing = false
		view.Frame()
		drag(view, card)
		if a.np.last.Playing {
			t.Fatal("a 20pt drag toggled playback: the moved press became a tap")
		}
	})

	t.Run("awake", func(t *testing.T) {
		a, p := musicShelf(t)
		power := newFakePower()
		a.awake = &awakeModule{host: a, create: power.create, release: power.release}
		homeReviewLayout(a, layout.Widget{ID: "awake", X: 0, Y: 0, W: 1, H: 1})
		view := pageTester(p, true)
		card, ok := view.Find("保持唤醒")
		if !ok {
			t.Fatalf("awake card missing: %v", view.Texts())
		}
		tap(view, card)
		if power.held() != 1 {
			t.Fatal("fixture: a clean tap did not start 保持唤醒")
		}
		a.awake.stop(time.Now())
		if power.held() != 0 {
			t.Fatal("fixture: stop did not release the assertion")
		}
		view.Frame()
		drag(view, card)
		if power.held() != 0 {
			t.Fatal("a 20pt drag switched 保持唤醒: the moved press became a tap")
		}
	})

	t.Run("usage page", func(t *testing.T) {
		a, p := musicShelf(t)
		a.settings.AIUsage.Enabled = true
		a.usage = &usageFeature{host: a}
		a.registerPage(a.usage)
		a.tab = pageHome
		homeReviewLayout(a, layout.Widget{ID: "usage", X: 0, Y: 0, W: 2, H: 1})
		view := pageTester(p, true)
		card, ok := view.Find("AI 用量")
		if !ok {
			t.Fatalf("usage card missing: %v", view.Texts())
		}
		tap(view, card)
		if a.tab != pageUsage {
			t.Fatalf("fixture: a clean tap left the page on %s", a.tab)
		}
		a.tab = pageHome
		view.Frame()
		drag(view, card)
		if a.tab != pageHome {
			t.Fatalf("a 20pt drag switched to %s", a.tab)
		}
	})

	t.Run("clipboard", func(t *testing.T) {
		a, p := musicShelf(t)
		a.store.Items = append(a.store.Items, store.Item{
			ID: "c1", Kind: store.KindText, Text: "剪贴板里的一段文字", Source: store.SourceClipboard,
		})
		copied := 0
		a.write = recordClipFunc(func(string) error { copied++; return nil })
		homeReviewLayout(a, layout.Widget{ID: "clip", X: 0, Y: 0, W: 2, H: 1})
		view := pageTester(p, true)
		card, ok := view.Find("剪贴板最新")
		if !ok {
			t.Fatalf("clip card missing: %v", view.Texts())
		}
		tap(view, card)
		if copied != 1 {
			t.Fatalf("fixture: a clean tap copied %d times, want 1", copied)
		}
		view.Frame()
		drag(view, card)
		if copied != 1 {
			t.Fatalf("a 20pt drag copied the item again (%d writes)", copied)
		}
	})
}

// TestHomeDisabledWidgetLeavesItsCellsFree is review #6: AI 用量 switched
// off takes no space — another widget may move onto its cell and one may
// be added there — while the entry stays in the file; when the feature
// comes back and the cells are taken, the widget returns to the catalogue.
func TestHomeDisabledWidgetLeavesItsCellsFree(t *testing.T) {
	a, p := musicShelf(t)
	a.dataDir = t.TempDir()
	a.settings.AIUsage.Enabled = false
	homeReviewLayout(a,
		layout.Widget{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		layout.Widget{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		layout.Widget{ID: "usage", X: 3, Y: 0, W: 1, H: 1},
		layout.Widget{ID: "recent", X: 2, Y: 1, W: 2, H: 1},
	)
	// The page draws with the feature off: 用量 is not on the canvas.
	p.settleWidgets()
	if got := widgetByID(a.toolsLayout().Home, "usage"); got.ID == "" {
		t.Fatal("the switched-off widget was dropped from the layout")
	}
	if !a.mutateToolsChanged(func(l *layout.Layout) bool {
		return l.MoveWidget("awake", 3, 0)
	}) {
		t.Fatal("a cell under the switched-off widget refused a move")
	}
	if got := widgetByID(a.toolsLayout().Home, "awake"); got.X != 3 || got.Y != 0 {
		t.Fatalf("awake = %+v, want (3,0)", got)
	}
	if !a.mutateToolsChanged(func(l *layout.Layout) bool {
		return l.AddWidget(layout.Widget{ID: "clip", X: 2, Y: 0, W: 1, H: 1})
	}) {
		t.Fatal("a cell under the switched-off widget refused an added widget")
	}

	// Feature back on: its stored rect is taken, so it goes to the
	// catalogue instead of overlapping.
	a.settings.AIUsage.Enabled = true
	p.settleWidgets()
	if got := widgetByID(a.toolsLayout().Home, "usage"); got.ID != "" {
		t.Fatalf("the returning widget kept its taken cell: %+v", got)
	}
	if got := widgetByID(a.toolsLayout().Home, "awake"); got.X != 3 || got.Y != 0 {
		t.Fatalf("the returning widget disturbed awake: %+v", got)
	}
	if got := widgetByID(layoutOf(t, a.dataDir).Home, "usage"); got.ID != "" {
		t.Fatalf("layout.json still places the returning widget: %+v", got)
	}
}

// layoutOf reads the written layout back.
func layoutOf(t *testing.T, dir string) layout.Layout {
	t.Helper()
	l, err := layout.Load(dir)
	if err != nil {
		t.Fatalf("reading the layout: %v", err)
	}
	return l
}

// TestHomeMusicClockLabelsFit is review #7: the 2×2 player's time labels
// are as wide as their own text, so -9:59, -10:00, 1:02:03 and -1:00:00
// are whole inside the card.
func TestHomeMusicClockLabelsFit(t *testing.T) {
	cases := []struct {
		pos, dur float64
		left     string
		right    string
	}{
		{1, 600, "0:01", "-9:59"},
		{0, 600, "0:00", "-10:00"},
		{3723, 3723, "1:02:03", "-0:00"},
		{0, 3600, "0:00", "-1:00:00"},
	}
	for _, c := range cases {
		a, p := musicShelf(t)
		a.np.last = mac.NowPlayingState{
			Title: "座右铭", Artist: "吴雨霏", Duration: c.dur, Elapsed: c.pos,
			Rate: 1, Received: time.Now(),
		}
		homeReviewLayout(a, layout.Widget{ID: "playing", X: 0, Y: 0, W: 2, H: 2})
		view := pageTester(p, true)
		card, ok := view.Find("音乐")
		if !ok {
			t.Fatalf("%s: music card missing: %v", c.right, view.Texts())
		}
		for _, label := range []string{c.left, c.right} {
			r, ok := view.Find(label)
			if !ok {
				t.Fatalf("%s: label %q is not drawn: %v", c.right, label, view.Texts())
			}
			need := measureHomeText(t, label, 9.5, 500)
			if r.W+0.5 < need {
				t.Errorf("%s: label %q is %.1fpt wide, its text needs %.1fpt", c.right, label, r.W, need)
			}
			if r.X < card.X-0.5 || r.X+r.W > card.X+card.W+0.5 {
				t.Errorf("%s: label %q at %+v leaves the card %+v", c.right, label, r, card)
			}
		}
	}
}

// measureHomeText is the natural width of text at a font, as the engine
// measures it.
func measureHomeText(t *testing.T, text string, size float32, weight int) float32 {
	t.Helper()
	var w float32
	probe := ui.NewTester(func(c *ui.Context) {
		w, _ = c.MeasureText(0, ui.Span{Text: text, Size: size, Weight: weight})
	}, 400, 40)
	probe.Frame()
	if w <= 0 {
		t.Fatalf("measuring %q gave %.1fpt", text, w)
	}
	return w
}

// TestHomeVolumeFollowsOutputDevice is review #8: the drawn player
// re-reads the output volume on the tick's pace — switching the system
// output while the panel is open reaches the bar — and a drag in progress
// keeps owning the shown value.
func TestHomeVolumeFollowsOutputDevice(t *testing.T) {
	a, p := musicShelf(t)
	vol := &fakeVolume{v: 0.4, ok: true}
	a.vol = vol
	// The panel is open on 主页: the row may ask for its own next frame.
	a.visible, a.tab = true, pageHome
	homeReviewLayout(a, layout.Widget{ID: "playing", X: 0, Y: 0, W: 2, H: 2})
	view := pageTester(p, true)
	view.Frame()
	if p.vol != 0.4 {
		t.Fatalf("first read = %.2f, want 0.40", p.vol)
	}

	// A different output device picks a new level.
	vol.v = 0.8
	view.Advance(volumePoll / 4)
	view.Frame()
	if p.vol != 0.4 {
		t.Fatalf("the volume was re-read on every frame (%.2f before the poll)", p.vol)
	}
	// The row asks for the next frame itself, so the poll runs even while
	// nothing else animates.
	view.Advance(volumePoll)
	if p.vol != 0.8 {
		t.Fatalf("volume = %.2f after a device switch, want 0.80", p.vol)
	}

	// A drag holds the knob even while a poll would fire.
	bar, ok := view.Find("音量")
	if !ok {
		t.Fatal("volume bar missing")
	}
	view.Press(bar.X+bar.W/4, bar.Y+bar.H/2)
	view.Move(bar.X+bar.W*3/4, bar.Y+bar.H/2)
	dragged := p.volFrac
	vol.v = 0.2
	view.Advance(2 * volumePoll)
	view.Frame()
	if p.vol != 0.8 {
		t.Fatalf("a poll during the drag changed the stored volume to %.2f", p.vol)
	}
	if p.volFrac != dragged {
		t.Fatalf("a poll during the drag moved the knob %.2f → %.2f", dragged, p.volFrac)
	}
	view.Release(bar.X+bar.W*3/4, bar.Y+bar.H/2)
	view.Frame()
	if len(vol.sets) != 1 {
		t.Fatalf("volume writes = %v, want exactly one", vol.sets)
	}
}

//go:build darwin

package main

// Level geometry and navigation: the island opens first, a page icon opens
// the sheet, and ‹ / Esc / outside clicks / the shortcut close it in the
// documented order. Every page must fit the fixed 396 × 174 content box
// without clipped or wrapped text.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/aiusage"
	"shelf/internal/mac"
	"shelf/internal/settings"
	"shelf/internal/store"
)

// levelShelf is a headless shelf on the island with a little of both
// stores so every page has content.
func levelShelf(t *testing.T) (*shelf, *ui.Tester) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.AddText("托盘里一条"); err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		if err := s.AddClipText(fmt.Sprintf("剪贴板内容 %d · 一段足够长的文字", i), "com.example.Source"); err != nil {
			t.Fatal(err)
		}
	}
	a := &shelf{
		store: s, dataDir: t.TempDir(), level: 1, settings: settings.Default(),
		thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}, appNames: map[string]string{},
	}
	return a, ui.NewTester(a.view, sheetW, sheetH)
}

func TestIslandOpensFirstAndPageIconOpensSheet(t *testing.T) {
	a, view := levelShelf(t)
	a.visible = true
	view.Frame()
	if a.currentLevel() != 1 {
		t.Fatalf("level = %d, want the island", a.currentLevel())
	}
	if !view.HasText("托盘 1 项 · 剪贴板 12 条") {
		t.Fatalf("the island lost its counts; texts %v", view.Texts())
	}
	if view.HasText("返回一级岛") {
		t.Fatal("the island drew a sheet back row")
	}
	if err := view.Click("托盘"); err != nil {
		t.Fatalf("island page icon: %v", err)
	}
	if a.currentLevel() != 2 || a.tab != pageTray {
		t.Fatalf("page icon: level=%d tab=%s, want the sheet on the tray", a.currentLevel(), a.tab)
	}
	view.Frame()
	name, count := a.currentPage().title()
	if !view.HasText("返回一级岛 · " + name + " " + count) {
		t.Fatalf("the sheet lost its back row; texts %v", view.Texts())
	}
	if err := view.Click("返回一级岛 · " + name + " " + count); err != nil {
		t.Fatalf("back button: %v", err)
	}
	if a.currentLevel() != 1 {
		t.Fatalf("back left level %d, want the island", a.currentLevel())
	}
}

func TestEscapeStepsBackThenCloses(t *testing.T) {
	a, view := levelShelf(t)
	a.visible, a.level = true, 2
	view.Frame()
	view.Key(0, ui.KeyEscape)
	if a.currentLevel() != 1 || !a.visible {
		t.Fatalf("first Esc: level=%d visible=%v, want the island still open", a.currentLevel(), a.visible)
	}
	view.Frame()
	view.Key(0, ui.KeyEscape)
	if a.visible {
		t.Fatal("second Esc did not close the island")
	}
}

func TestOutsideClickAndShortcutCloseFromEitherLevel(t *testing.T) {
	a, _ := levelShelf(t)
	for _, level := range []int{1, 2} {
		a.visible, a.level = true, level
		a.mouseDown(false, 0, mygo.Point{X: a.x, Y: 400})
		if a.visible {
			t.Fatalf("level %d: an outside click did not close the panel", level)
		}
		// The shortcut toggles the same state.
		a.toggle()
		if !a.visible || a.currentLevel() != 1 {
			t.Fatalf("level %d: toggle opened level %d visible=%v, want the island", level, a.currentLevel(), a.visible)
		}
		a.toggle()
		if a.visible {
			t.Fatalf("level %d: toggle did not close", level)
		}
		a.closeAt = time.Time{} // no fade to wait for between cases
	}
}

func TestIncomingDragOpensTheSheetOnTheTray(t *testing.T) {
	a, view := levelShelf(t)
	t.Setenv("SHELF_DEBUG_DROP_FILES", "/tmp/a.png,/tmp/b.png")
	a.dragging = true
	a.inBand = true
	a.show(true)
	view.Frame()
	if a.currentLevel() != 2 || a.tab != pageTray {
		t.Fatalf("drag open: level=%d tab=%s, want the sheet on the tray", a.currentLevel(), a.tab)
	}
	if !view.HasText("放入托盘") || !view.HasText("投放") {
		t.Fatalf("the drop zone is missing; texts %v", view.Texts())
	}
}

func TestClipboardShortcutOpensTheSheetOnSearch(t *testing.T) {
	a, view := levelShelf(t)
	a.openClipboardSearch()
	view.Frame()
	if a.currentLevel() != 2 || a.tab != pageClipboard || !a.focusMode {
		t.Fatalf("⌃⌥V: level=%d tab=%s focus=%v", a.currentLevel(), a.tab, a.focusMode)
	}
	if !view.Focused(searchLabel) {
		t.Fatal("⌃⌥V did not focus the search field")
	}
}

func TestMiniPlayerOnlyWhileATrackIsLoaded(t *testing.T) {
	a, view := levelShelf(t)
	a.settings.IslandNowPlaying = true
	a.np = &nowPlayingModule{host: a, have: true, last: mac.NowPlayingState{Playing: true, Title: "夜曲", Artist: "周杰伦"}}
	a.visible = true
	view.Frame()
	if !view.HasText("夜曲") || !view.HasText("周杰伦") {
		t.Fatalf("the island lost its mini player; texts %v", view.Texts())
	}
	if view.HasText("托盘 1 项 · 剪贴板 12 条") {
		t.Fatal("the counts drew beside the mini player")
	}
	a.np = nil
	view.Frame()
	if view.HasText("夜曲") || !view.HasText("托盘 1 项 · 剪贴板 12 条") {
		t.Fatalf("without a track the island must show the counts; texts %v", view.Texts())
	}
}

// assertPageFits walks every text the tester drew inside the content box
// and fails when one leaves the 396 × 174 box, is clipped by it, or wraps
// (two lines are allowed only for text that carries an explicit newline).
func assertPageFits(t *testing.T, view *ui.Tester, page string, containers map[string]bool) {
	t.Helper()
	boxX, boxY := float32((sheetW-contentW)/2), float32(bandH+backRowH+contentGap)
	boxR, boxB := boxX+contentW, boxY+contentH
	seen := map[string]bool{}
	count := map[string]int{}
	for _, text := range view.Texts() {
		count[text]++
	}
	for _, text := range view.Texts() {
		key := page + "\x00" + text
		if seen[key] {
			continue
		}
		seen[key] = true
		r, ok := view.Find(text)
		if !ok || r.W <= 0 || r.H <= 0 {
			continue
		}
		if r.Y < boxY-0.5 || r.Y >= boxB-0.5 {
			continue // band, back row, or fully scrolled out
		}
		if r.X < boxX-0.5 || r.X+r.W > boxR+0.5 || r.Y+r.H > boxB+0.5 {
			t.Fatalf("%s: text %q at %+v leaves the content box [%v,%v,%v,%v]",
				page, text, r, boxX, boxY, boxR, boxB)
		}
		// A row or tile carries the same words as its own label; Find
		// answers the container first, so its height says nothing about
		// the words. Only labels that occur once are text nodes.
		if count[text] > 1 || containers[text] {
			continue
		}
		limit := float32(26) // one line of the largest page font (19pt: the home canvas' countdown)
		if strings.Contains(text, "\n") {
			limit = 36 // up to two explicit lines (empty states, quota messages)
		}
		if r.H > limit {
			t.Fatalf("%s: text %q is %v tall, over the %v single-line limit (wrapped?)",
				page, text, r.H, limit)
		}
	}
}

func TestEveryPageFitsTheContentBox(t *testing.T) {
	a, view := levelShelf(t)
	a.visible, a.level = true, 2
	for _, id := range []pageID{pageHome, pageTray, pageClipboard, pageTools} {
		a.setTab(id)
		view.Frame()
		ignore := map[string]bool{}
		for _, it := range a.store.Items {
			ignore[itemTitle(it)] = true
		}
		for _, card := range a.toolsLayout().Cards {
			ignore[card.ID] = true
		}
		if id == pageHome {
			// A widget's card carries its title as a label; the same
			// words may be a text or not, so the container is skipped by
			// name.
			for _, w := range a.toolsLayout().Home {
				if def := toolCardDefFor(w.ID); def != nil {
					ignore[def.title] = true
				}
			}
		}
		assertPageFits(t, view, string(id), ignore)
	}
}

func TestUsagePageStatesFitTheContentBox(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	build := func(t *testing.T, mutate func(*usageFixture)) *ui.Tester {
		t.Helper()
		fx := newUsageFixture(t, true)
		fx.codex.q = okQuota(aiusage.ToolCodex, "Plus", now)
		fx.claude.q = okQuota(aiusage.ToolClaude, "Pro", now)
		fx.scan.summaries = map[aiusage.Range]aiusage.Summary{
			aiusage.RangeToday: {
				Range:   aiusage.RangeToday,
				Total:   aiusage.Totals{Input: 8_400_000, Output: 1_100_000, CacheRead: 5_200_000, Requests: 412, Cost: 3.72},
				ByTool:  map[aiusage.Tool]aiusage.Totals{aiusage.ToolClaude: {Requests: 260, Cost: 2.4}, aiusage.ToolCodex: {Requests: 152, Cost: 1.32}},
				Buckets: []aiusage.Bucket{{Start: now, Tokens: map[aiusage.Tool]int64{aiusage.ToolOmp: 1_000_000, aiusage.ToolClaude: 400_000}}},
			},
		}
		if mutate != nil {
			mutate(fx)
		}
		fx.f.tick(now)
		fx.a.level, fx.a.visible, fx.a.tab = 2, true, pageUsage
		fx.view.Frame()
		return fx.view
	}
	t.Run("today", func(t *testing.T) {
		assertPageFits(t, build(t, nil), "usage/today", nil)
	})
	t.Run("30 days", func(t *testing.T) {
		assertPageFits(t, build(t, func(fx *usageFixture) { fx.f.rangeSel = aiusage.Range30d }), "usage/30d", nil)
	})
	t.Run("not logged in", func(t *testing.T) {
		assertPageFits(t, build(t, func(fx *usageFixture) {
			fx.codex.q = aiusage.Quota{Tool: aiusage.ToolCodex, Status: aiusage.QuotaNotLoggedIn, FetchedAt: now}
			fx.claude.q = aiusage.Quota{Tool: aiusage.ToolClaude, Status: aiusage.QuotaExpired, FetchedAt: now}
		}), "usage/not-logged-in", nil)
	})
	t.Run("first scan", func(t *testing.T) {
		assertPageFits(t, build(t, func(fx *usageFixture) {
			fx.f.hasSummary = false
			fx.f.localBusy = true
			fx.f.scanDone.Store(37)
			fx.f.scanTotal.Store(100)
		}), "usage/first-scan", nil)
	})
}

func TestUsageHoverDetailStaysInsideTheContentBox(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	fx := newUsageFixture(t, true)
	fx.codex.q = okQuota(aiusage.ToolCodex, "Plus", now)
	fx.claude.q = okQuota(aiusage.ToolClaude, "Pro", now)
	fx.a.level, fx.a.visible, fx.a.tab = 2, true, pageUsage
	fx.f.tick(now)
	fx.view.Frame()
	fx.view.Move(100, 100) // over the first quota card
	fx.view.Frame()
	if !fx.view.HasText("比均匀用量") {
		t.Fatalf("the hover detail did not appear; texts %v", fx.view.Texts())
	}
	assertPageFits(t, fx.view, "usage-hover", map[string]bool{"Codex": true, "Claude Code": true})
}

func TestDragOverTheIslandOpensTheSheet(t *testing.T) {
	a, view := levelShelf(t)
	t.Setenv("SHELF_DEBUG_DROP_FILES", "/tmp/a.png")
	a.visible, a.level = true, 1
	a.dragging = true
	view.Frame()
	if a.currentLevel() != 2 || a.tab != pageTray {
		t.Fatalf("island drag: level=%d tab=%s, want the sheet on the tray", a.currentLevel(), a.tab)
	}
	if !view.HasText("放入托盘") {
		t.Fatalf("the drop zone is missing; texts %v", view.Texts())
	}
}

func TestActivityChipActionsStillWork(t *testing.T) {
	t.Run("converter cancel", func(t *testing.T) {
		j := &convertJob{target: toJPEG, started: time.Now().Add(-time.Second)}
		j.total.Store(3)
		j.done.Store(1)
		a := &shelf{level: 2}
		a.conv = &converter{host: a, job: j}
		a.modules = []module{a.conv}
		view := ui.NewTester(a.view, sheetW, sheetH)
		view.Frame()
		if !view.HasText("正在转成 JPEG · 2/3") {
			t.Fatalf("the converter chip lost its progress tooltip; texts %v", view.Texts())
		}
		if err := view.Click("正在转成 JPEG · 2/3"); err != nil {
			t.Fatalf("tap the chip: %v", err)
		}
		view.Frame()
		if err := view.Click("取消转换"); err != nil {
			t.Fatalf("the chip did not reveal 取消: %v", err)
		}
		if !j.cancel.Load() {
			t.Fatal("取消 did not cancel the job")
		}
	})
	t.Run("awake stop", func(t *testing.T) {
		p := newFakePower()
		m := newAwakeStub(p)
		m.start(time.Now(), 30*time.Minute)
		a := &shelf{level: 2}
		a.modules = []module{m}
		view := ui.NewTester(a.view, sheetW, sheetH)
		view.Frame()
		if !view.HasText("保持唤醒") {
			t.Fatalf("the awake chip is missing; texts %v", view.Texts())
		}
		if err := view.Click("保持唤醒"); err != nil {
			t.Fatalf("tap the chip: %v", err)
		}
		if m.active() || p.held() != 0 {
			t.Fatal("tapping the chip did not stop the keep-awake hold")
		}
	})
}

func TestIslandHoldsFivePageIconsAndCounts(t *testing.T) {
	fx := newUsageFixture(t, true)
	a := fx.a
	a.level, a.visible = 1, true
	a.registerPage(&fakePage{on: true}) // stands in for H's 主页
	view := ui.NewTester(a.view, sheetW, sheetH)
	view.Frame()
	for _, label := range []string{"托盘", "剪贴板", "工具", "AI 用量", "假页面"} {
		if _, ok := view.Find(label); !ok {
			t.Fatalf("the island lost the %q icon; texts %v", label, view.Texts())
		}
	}
	counts, ok := view.Find("托盘 0 项 · 剪贴板 0 条")
	if !ok {
		t.Fatalf("the island lost its counts; texts %v", view.Texts())
	}
	first, _ := view.Find("托盘")
	if counts.X+counts.W > first.X-2 {
		t.Fatalf("counts %+v run into the page icons at %+v", counts, first)
	}
}

func TestTwoActivityChipsFitTheLeftEar(t *testing.T) {
	a := &shelf{level: 2, notchW: 180, notchH: 32, topInset: 32, settings: settings.Default()}
	a.settings.IslandNowPlaying = true
	a.np = &nowPlayingModule{host: a, have: true, last: mac.NowPlayingState{Playing: true, Title: "夜曲", Artist: "周杰伦"}}
	p := newFakePower()
	awake := newAwakeStub(p)
	awake.start(time.Now(), 30*time.Minute)
	a.modules = []module{a.np, awake}
	view := ui.NewTester(a.view, sheetW, sheetH)
	view.Frame()
	music, ok := view.Find("夜曲")
	if !ok {
		t.Fatalf("the now-playing chip is missing; texts %v", view.Texts())
	}
	hold, ok := view.Find("保持唤醒")
	if !ok {
		t.Fatalf("the awake chip is missing; texts %v", view.Texts())
	}
	housingLeft := float32(sheetPadX + (sheetW-24-(180+12))/2)
	if music.X < sheetPadX-0.5 || hold.X+hold.W > housingLeft+0.5 {
		t.Fatalf("chips %+v and %+v leave the left ear (housing at %v)", music, hold, housingLeft)
	}
}

func TestMusicAndCancelChipsFitTheLeftEar(t *testing.T) {
	j := &convertJob{target: toJPEG, started: time.Now().Add(-time.Second)}
	j.total.Store(3)
	j.done.Store(1)
	a := &shelf{level: 2, notchW: 180, notchH: 32, topInset: 32, settings: settings.Default()}
	a.settings.IslandNowPlaying = true
	a.np = &nowPlayingModule{host: a, have: true, last: mac.NowPlayingState{Playing: true, Title: "夜曲"}}
	a.conv = &converter{host: a, job: j, cancelOpen: true}
	a.modules = []module{a.np, a.conv}
	view := ui.NewTester(a.view, sheetW, sheetH)
	view.Frame()
	music, ok := view.Find("夜曲")
	if !ok {
		t.Fatalf("the now-playing chip is missing; texts %v", view.Texts())
	}
	cancel, ok := view.Find("取消转换")
	if !ok {
		t.Fatalf("the cancel state is missing; texts %v", view.Texts())
	}
	housingLeft := float32(sheetPadX + (sheetW-24-(180+12))/2)
	if music.X < sheetPadX-0.5 || cancel.X+cancel.W > housingLeft+0.5 {
		t.Fatalf("chips %+v and %+v leave the left ear (housing at %v)", music, cancel, housingLeft)
	}
}

func TestLevelSpringsBetweenTheTwoSizes(t *testing.T) {
	a := motionWindow()
	a.level, a.visible, a.jumpLevel = 1, true, true
	view := ui.NewTester(a.view, sheetW, sheetH)
	for range 60 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	if a.fx.w < islandW-0.5 || a.fx.w > islandW+1 || a.fx.h < islandH-0.5 || a.fx.h > islandH+1 {
		t.Fatalf("island shell = %.1f×%.1f, want %d×%d", a.fx.w, a.fx.h, islandW, islandH)
	}
	a.setLevel(2)
	for i := range 80 {
		view.Frame()
		f := a.desiredFrame()
		if f.Width < int(a.fx.w+0.5)+2*earR-1 || f.Height < int(a.fx.h+0.5) {
			t.Fatalf("frame %d cuts the growing shell %.1f×%.1f", i, a.fx.w, a.fx.h)
		}
		view.Advance(16 * time.Millisecond)
	}
	if a.fx.w < sheetW-0.5 || a.fx.h < sheetH-0.5 {
		t.Fatalf("sheet shell = %.1f×%.1f, want %d×%d", a.fx.w, a.fx.h, sheetW, sheetH)
	}
	a.setLevel(1)
	for range 80 {
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	if a.fx.w > islandW+1 || a.fx.h > islandH+1 {
		t.Fatalf("back to island: shell %.1f×%.1f", a.fx.w, a.fx.h)
	}
}

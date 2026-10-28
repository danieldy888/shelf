//go:build darwin

package main

// English coverage: the whole interface says English with no Chinese left in
// it, every page still fits the fixed boxes, and a language change shows at
// once without a restart. The rest of the suite runs in the source language.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/aiusage"
	"shelf/internal/settings"
	"shelf/internal/store"
	"shelf/locales"
)

// hasHan reports whether s has a Han character, the residue an English
// interface must never show. Only user data may.
func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func assertNoHan(t *testing.T, where string, texts []string) {
	t.Helper()
	for _, text := range texts {
		if hasHan(text) {
			t.Errorf("%s: Chinese left in the English interface: %q", where, text)
		}
	}
}

// englishShelf is levelShelf with English user data and the app showing
// English: the panel's language and the native surfaces' language agree, as
// they do in the app.
func englishShelf(t *testing.T) (*shelf, *ui.Tester) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.AddText("A note from the tray"); err != nil {
		t.Fatal(err)
	}
	for i := range 11 {
		if err := s.AddClipText(fmt.Sprintf("Clipboard line %d, long enough to fill the row", i), "com.example.Source"); err != nil {
			t.Fatal(err)
		}
	}
	// The newest row, and the one a context menu is opened on.
	if err := s.AddClipText("Clipboard newest", "com.example.Source"); err != nil {
		t.Fatal(err)
	}
	a := &shelf{
		store: s, dataDir: t.TempDir(), level: 1, settings: settings.Default(), locale: "en",
		thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}, appNames: map[string]string{},
	}
	view := ui.NewTester(a.view, sheetW, sheetH)
	view.SetLocale("en")
	return a, view
}

// TestEnglishPanelPagesHaveNoChineseAndFit draws the island and every sheet
// page in English: no label may say Chinese, and the 396 × 174 audit must
// find nothing cut or wrapped.
func TestEnglishPanelPagesHaveNoChineseAndFit(t *testing.T) {
	a, view := englishShelf(t)
	drain := fitSink(t)

	a.visible = true
	view.Frame()
	view.Frame()
	assertNoHan(t, "island", view.Texts())
	if !view.HasText(a.appLoc().PanelCounts(1, 12)) {
		t.Fatalf("the island does not say its counts in English; texts %v", view.Texts())
	}
	if notes := drain(); len(notes) > 0 {
		t.Errorf("island: %v", notes)
	}

	a.level = 2
	for _, id := range []pageID{pageHome, pageTray, pageClipboard, pageTools} {
		a.setTab(id)
		view.Frame()
		drain() // the first frame of a tab has no boxes yet
		view.Frame()
		assertNoHan(t, string(id), view.Texts())
		if name := pageTitleText(a.appLoc(), a.page(id)); !view.HasText(name) {
			t.Errorf("%s: the back row lost the English page name %q; texts %v", id, name, view.Texts())
		}
		assertPageFits(t, view, string(id), drain(), a.sheetTop())
	}

	// The home page's edit grid and its catalogue say English too.
	a.setTab(pageHome)
	p := a.homePageOf()
	p.editing, p.picking = true, true
	view.Frame()
	drain() // the first frame of the state has no boxes yet
	view.Frame()
	assertNoHan(t, "home-picker", view.Texts())
	if notes := drain(); len(notes) > 0 {
		t.Errorf("home-picker: %v", notes)
	}
	p.editing, p.picking = false, false
}

// TestEnglishSettingsWindowHasNoChinese: every group, row, caption and
// control of the Settings window says English.
func TestEnglishSettingsWindowHasNoChinese(t *testing.T) {
	a, view := newSettingsShelf(t)
	a.locale = "en"
	view.SetLocale("en")
	view.Frame()
	view.Frame()
	// The language's own name is shown in itself ("简体中文"), as macOS shows
	// language menus: that is not a leftover translation.
	texts := view.Texts()
	for i, text := range texts {
		if text == "简体中文" {
			texts[i] = ""
		}
	}
	assertNoHan(t, "settings", texts)
	l := locales.In("en")
	joined := strings.Join(view.Texts(), "\n")
	for _, want := range []string{
		l.General(), l.SettingsLoginRow(), l.SettingsAutoCheckRow(),
		l.SettingsLiveSection(), l.SettingsClipboardSection(), l.SettingsAISection(),
		l.LanguageRow(), l.LanguageFollowSystem(), l.LanguageEnglish(),
		l.SettingsVersionLine(appName, appVersion()),
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("settings: %q missing from the English window", want)
		}
	}
}

// TestEnglishSettingsWindowLayout is the Settings window's truncation and
// overlap audit: in English no group's text leaves the window, no two texts
// overlap, no single-line label is cut (the settingsFit seam), and a caption
// that wraps grows its row instead of pressing the hairline or the next row.
func TestEnglishSettingsWindowLayout(t *testing.T) {
	var cut []string
	orig := settingsFit
	settingsFit = func(s string) { cut = append(cut, s) }
	t.Cleanup(func() { settingsFit = orig })

	a, view := newSettingsShelf(t)
	a.locale = "en"
	view.SetLocale("en")
	l := locales.In("en")

	// Both ends of the scroll, so every group is drawn at least once.
	seen := map[string]bool{}
	var audit func(where string)
	audit = func(where string) {
		view.Frame()
		view.Frame()
		texts := view.Texts()
		if where == "top" {
			for i, text := range texts {
				if text == "简体中文" {
					texts[i] = ""
				}
			}
			assertNoHan(t, "settings/"+where, texts)
		}
		for _, text := range view.Texts() {
			seen[text] = true
		}
		assertSettingsLayout(t, view, where)
	}

	a.settingsScroll.Y = 0
	audit("top")
	// Each group is on screen at one end or the other.
	for _, group := range []string{
		l.General(), l.PermTitle(), l.SettingsLiveSection(), l.SettingsAISection(),
		l.SettingsClipboardSection(), l.SettingsTraySection(), l.SettingsShortcutsSection(),
	} {
		if !seen[group] {
			t.Errorf("settings: the group %q was never drawn", group)
		}
	}
	// The rows that wrap keep air above and below: the caption's box ends
	// before the next row starts.
	for _, pair := range [][2]string{
		{l.ShellStyleCaption(), l.LanguageRow()},
		{l.LanguageCaption(), l.SettingsSnapRow()},
	} {
		cap, ok := view.Find(pair[0])
		if !ok {
			t.Fatalf("caption %q missing", pair[0])
		}
		next, ok := view.Find(pair[1])
		if !ok {
			t.Fatalf("row %q missing", pair[1])
		}
		if cap.H <= 20 {
			t.Errorf("%q did not wrap into two lines (height %.1f)", pair[0], cap.H)
		}
		if gap := next.Y - (cap.Y + cap.H); gap < 2 {
			t.Errorf("%q ends %.1fpt above %q: the caption presses the next row", pair[0], gap, pair[1])
		}
	}

	a.settingsScroll.Y = 1e6
	audit("bottom")

	// A one-line row keeps its 40pt height: the padding only shows up where a
	// caption wraps, so the Chinese layout is the one it always was.
	a.settingsScroll.Y = 0
	view.Frame()
	view.Frame()
	if r, ok := view.Find(l.SettingsLoginRow()); ok && r.H != 40 {
		t.Errorf("a one-line row is %.1fpt tall, want 40", r.H)
	}
	for _, note := range cut {
		t.Errorf("settings: %s", note)
	}
}

// assertSettingsLayout checks the Settings window's boxes: no text leaves the
// window sideways, and no two texts drawn in it overlap.
func assertSettingsLayout(t *testing.T, view *ui.Tester, where string) {
	t.Helper()
	type box struct {
		text string
		r    ui.Rect
	}
	var boxes []box
	seen := map[ui.Rect]bool{}
	for _, text := range view.Texts() {
		r, ok := view.Find(text)
		if !ok || r.W <= 0 || r.H <= 0 || seen[r] {
			continue
		}
		seen[r] = true
		if r.X < -0.5 || r.X+r.W > settingsWidth+0.5 {
			t.Errorf("%s: %q leaves the window sideways: %+v", where, text, r)
		}
		if r.Y < -0.5 || r.Y+r.H > settingsHeight+0.5 {
			continue // scrolled out of the viewport
		}
		boxes = append(boxes, box{text, r})
	}
	// A toggle row carries its label as its accessible name, so Find answers
	// the row, which holds its own label and caption: a box that contains
	// another box is a container, not a line of text.
	var lines []box
	for i, b := range boxes {
		container := false
		for j, o := range boxes {
			if i != j && o.r.X >= b.r.X-0.5 && o.r.Y >= b.r.Y-0.5 &&
				o.r.X+o.r.W <= b.r.X+b.r.W+0.5 && o.r.Y+o.r.H <= b.r.Y+b.r.H+0.5 {
				container = true
				break
			}
		}
		if !container {
			lines = append(lines, b)
		}
	}
	boxes = lines
	for i := range boxes {
		for j := i + 1; j < len(boxes); j++ {
			a, b := boxes[i], boxes[j]
			if a.r.X < b.r.X+b.r.W-0.5 && b.r.X < a.r.X+a.r.W-0.5 &&
				a.r.Y < b.r.Y+b.r.H-0.5 && b.r.Y < a.r.Y+a.r.H-0.5 {
				t.Errorf("%s: %q at %+v overlaps %q at %+v", where, a.text, a.r, b.text, b.r)
			}
		}
	}
}

// TestSettingsLabelReportsCutText proves the seam the settings audit relies
// on: a single-line label wider than its box is reported.
func TestSettingsLabelReportsCutText(t *testing.T) {
	var cut []string
	orig := settingsFit
	settingsFit = func(s string) { cut = append(cut, s) }
	t.Cleanup(func() { settingsFit = orig })

	view := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Width(80).Children(func() {
			settingsLabel(c, "A label far wider than eighty points", 13, 0)
		})
	}, 200, 40)
	view.Frame()
	view.Frame()
	if len(cut) == 0 {
		t.Fatal("a label wider than its box was not reported")
	}
}

// TestEnglishMenusHaveNoChinese: the tray menu and an item's context menu
// say English, labels and submenu labels alike.
func TestEnglishMenusHaveNoChinese(t *testing.T) {
	a, view := englishShelf(t)
	labels := menuLabels(a.trayMenu())
	for _, want := range []string{"Show / Hide ShelfU", "Settings…", "Quit ShelfU"} {
		if !slices.Contains(labels, want) {
			t.Fatalf("the tray menu lacks %q; labels %v", want, labels)
		}
	}
	assertNoHan(t, "tray menu", labels)

	// The clipboard row's context menu: open it on a row and read it.
	a.visible, a.level = true, 0 // level 0 without a native window is the sheet
	a.setTab(pageClipboard)
	view.Frame()
	view.Frame()
	if err := view.RightClick("Clipboard newest"); err != nil {
		t.Fatalf("right-click a clipboard row: %v", err)
	}
	menu := view.Menu()
	if len(menu) == 0 {
		t.Fatal("no context menu on a clipboard row")
	}
	for _, want := range []string{"Copy Text", "Pin", "Remove"} {
		if !slices.Contains(menu, want) {
			t.Fatalf("the item menu lacks %q; menu %v", want, menu)
		}
	}
	assertNoHan(t, "item menu", menu)
}

// TestEnglishToastsHaveNoChinese: a status and an error the user sees say
// English.
func TestEnglishToastsHaveNoChinese(t *testing.T) {
	a, _ := englishShelf(t)
	a.toastStatus(a.appLoc().ItemCopied(), time.Now().Add(time.Second), statusOK)
	assertNoHan(t, "status", []string{a.status})
	a.report(errors.New("boom"))
	assertNoHan(t, "report", []string{a.status})
	if !strings.Contains(a.status, "boom") {
		t.Fatalf("the failure lost its detail: %q", a.status)
	}
}

// TestEnglishUsagePageHasNoChineseAndFits draws the AI usage page in English
// while it has no data yet and once it has a summary.
func TestEnglishUsagePageHasNoChineseAndFits(t *testing.T) {
	fx := newUsageFixture(t, true)
	fx.a.locale = "en"
	fx.view.SetLocale("en")
	fx.a.setTab(pageUsage)
	fx.a.level = 2
	drain := fitSink(t)
	for _, name := range []string{"empty", "empty-again"} {
		fx.view.Frame()
		drain()
		fx.view.Frame()
		assertNoHan(t, "usage-"+name, fx.view.Texts())
		assertPageFits(t, fx.view, "usage-"+name, drain(), fx.a.sheetTop())
	}
}

// TestEnglishUsagePageStatesHaveNoChineseAndFit draws the AI usage page in
// English with quotas, a local summary and their failure states: the 84pt
// quota cards, the 64pt columns and the chart must all fit, and no Chinese
// may show (dates and weekdays in English too).
func TestEnglishUsagePageStatesHaveNoChineseAndFit(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	summary := func(r aiusage.Range) aiusage.Summary {
		return aiusage.Summary{
			Range:   r,
			Total:   aiusage.Totals{Input: 8_400_000, Output: 1_100_000, CacheRead: 5_200_000, CacheWrite: 90_000, Requests: 412, Unpriced: 7, Cost: 3.72},
			ByTool:  map[aiusage.Tool]aiusage.Totals{aiusage.ToolClaude: {Requests: 260, Cost: 2.4}, aiusage.ToolCodex: {Requests: 152, Cost: 1.32}},
			Buckets: []aiusage.Bucket{{Start: now, Tokens: map[aiusage.Tool]int64{aiusage.ToolOmp: 1_000_000, aiusage.ToolClaude: 400_000}}},
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*usageFixture)
	}{
		{"data", nil},
		{"not logged in", func(fx *usageFixture) {
			fx.codex.q = aiusage.Quota{Tool: aiusage.ToolCodex, Status: aiusage.QuotaNotLoggedIn, FetchedAt: now}
			fx.claude.q = aiusage.Quota{Tool: aiusage.ToolClaude, Status: aiusage.QuotaNotLoggedIn, FetchedAt: now}
		}},
		{"limit reached", func(fx *usageFixture) {
			fx.codex.q = aiusage.Quota{Tool: aiusage.ToolCodex, Status: aiusage.QuotaLimitReached, FetchedAt: now}
			fx.claude.q = aiusage.Quota{Tool: aiusage.ToolClaude, Status: aiusage.QuotaLimitReached,
				LimitResetsAt: now.Add(2*time.Hour + 7*time.Minute), FetchedAt: now}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drain := fitSink(t)
			fx := newUsageFixture(t, true)
			fx.codex.q = okQuota(aiusage.ToolCodex, "Plus", now)
			fx.claude.q = okQuota(aiusage.ToolClaude, "Pro", now)
			fx.scan.summaries = map[aiusage.Range]aiusage.Summary{}
			for _, r := range []aiusage.Range{aiusage.RangeToday, aiusage.Range7d, aiusage.Range30d, aiusage.RangeAll} {
				fx.scan.summaries[r] = summary(r)
			}
			fx.a.level, fx.a.visible, fx.a.tab = 2, true, pageUsage
			fx.a.locale = "en"
			fx.view.SetLocale("en")
			if tc.mutate != nil {
				tc.mutate(fx)
			}
			fx.f.tick(now)
			fx.view.Frame()
			drain()
			fx.view.Frame()
			assertNoHan(t, "usage/"+tc.name, fx.view.Texts())
			assertPageFits(t, fx.view, "usage/"+tc.name, drain(), fx.a.sheetTop())
			if tc.name == "data" && !fx.view.HasText("Requests") {
				t.Fatalf("the local panel says no English metrics; texts %v", fx.view.Texts())
			}
		})
	}
}

// TestEnglishFallback: following the system, a Chinese system reads Chinese
// (Traditional Chinese included) and every other reads English — mygo alone
// would fall back to the source language, Chinese, for French or Japanese.
func TestEnglishFallback(t *testing.T) {
	for _, tc := range []struct {
		locale string
		pref   []string
		want   bool
	}{
		{"zh-Hans", []string{"fr-FR"}, true},
		{"zh-Hans", []string{"ja-JP", "en-US"}, false},
		{"zh-Hans", []string{"zh-Hant-TW"}, false},
		{"zh-Hans", []string{"zh-Hans-CN"}, false},
		{"zh-Hans", nil, true},
		{"en", []string{"fr-FR"}, false},
	} {
		if got := englishFallback(tc.locale, tc.pref); got != tc.want {
			t.Errorf("englishFallback(%q, %v) = %v, want %v", tc.locale, tc.pref, got, tc.want)
		}
	}
}

// TestTimesFollowTheSystemClock: times follow the user's 12- or 24-hour
// clock in every language, so an English interface on a 24-hour Mac says
// 14:05 rather than 2:05 PM.
func TestTimesFollowTheSystemClock(t *testing.T) {
	fixed := time.Date(2026, 10, 19, 14, 5, 0, 0, time.Local)
	render := func(locale string, r i18n.Region) []string {
		view := ui.NewTester(func(c *ui.Context) {
			ui.Text(c, tloc(c).ClipTodayAt(fixed))
			ui.Text(c, stampIn(c.Locale(), c.Region(), fixed))
		}, 300, 100)
		view.SetLocale(locale)
		view.SetRegion(r)
		view.Frame()
		return view.Texts()
	}
	if texts := render("en", i18n.Region{HourCycle: 24}); !slices.Contains(texts, "Today 14:05") {
		t.Fatalf("24-hour English: %v", texts)
	}
	if texts := render("en", i18n.Region{HourCycle: 12}); !slices.Contains(texts, "Today 2:05 PM") {
		t.Fatalf("12-hour English: %v", texts)
	}
	if texts := render("zh-Hans", i18n.Region{HourCycle: 24}); !slices.Contains(texts, "今天 14:05") {
		t.Fatalf("24-hour Chinese: %v", texts)
	}
	// The clipboard row's screenshot title keeps the source language's
	// compact date, and takes the region's date and clock in English.
	if got := stampIn("zh-Hans", i18n.Region{}, fixed); got != "10-19 14:05" {
		t.Fatalf("Chinese stamp = %q", got)
	}
	if got := stampIn("en", i18n.Region{}, fixed); !strings.Contains(got, "2:05") {
		t.Fatalf("English stamp = %q", got)
	}
}

// TestEnglishBasketHasNoChinese: the drop card's three states say English.
func TestEnglishBasketHasNoChinese(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := &shelf{store: s, locale: "en"}
	view := ui.NewTester(a.basketView, basketWidth, basketHeight)
	view.SetLocale("en")
	view.Frame()
	assertNoHan(t, "basket", view.Texts())
	if !view.HasText("Drop Here") {
		t.Fatalf("basket prompt is missing; texts %v", view.Texts())
	}
	a.basketReceive(transfer.TextData("hello"))
	view.Frame()
	assertNoHan(t, "basket saved", view.Texts())
	if !view.HasText("Added to ShelfU") {
		t.Fatalf("basket confirmation is missing; texts %v", view.Texts())
	}
}

// TestEnglishRenameWindowHasNoChinese: every mode, row and preview of the
// rename window says English.
func TestEnglishRenameWindowHasNoChinese(t *testing.T) {
	a, view, _ := renameShelf(t, "Report.txt", "Notes.txt")
	a.locale = "en"
	view.SetLocale("en")
	r := openHeadlessRename(t, a)
	r.win = &mygo.Window{}
	for _, mode := range []int{0, 1, 2} {
		a.rn.mode = mode
		view.Frame()
		view.Frame()
		assertNoHan(t, fmt.Sprintf("rename mode %d", mode), view.Texts())
	}
	if !view.HasText("Rename") {
		t.Fatalf("the rename window is not English; texts %v", view.Texts())
	}
}

// TestLanguageSwitchAppliesWithoutRestart: the same view says Chinese and
// then English as the language changes, with no new window, and the native
// menus follow the shelf's language.
func TestLanguageSwitchAppliesWithoutRestart(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.AddText("A note from the tray"); err != nil {
		t.Fatal(err)
	}
	a := &shelf{
		store: s, dataDir: t.TempDir(), visible: true, level: 1, settings: settings.Default(),
		thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}, appNames: map[string]string{},
	}
	view := ui.NewTester(a.view, sheetW, sheetH)
	view.Frame()
	view.Frame()
	if !view.HasText("托盘 1 项") {
		t.Fatalf("the source language does not say the counts; texts %v", view.Texts())
	}
	view.SetLocale("en")
	view.Frame()
	view.Frame()
	if view.HasText("托盘 1 项") || !view.HasText(locales.In("en").PageTray()) {
		t.Fatalf("the view did not switch to English; texts %v", view.Texts())
	}
	view.SetLocale("zh-Hans")
	view.Frame()
	view.Frame()
	if !view.HasText("托盘 1 项") {
		t.Fatalf("the view did not switch back; texts %v", view.Texts())
	}

	// Native surfaces read the shelf's language, which OnLocaleChanged sets.
	a.locale = ""
	if labels := menuLabels(a.trayMenu()); !slices.Contains(labels, "显示 / 收起 ShelfU") {
		t.Fatalf("the tray menu is not in the source language: %v", labels)
	}
	a.localeChanged("en")
	if labels := menuLabels(a.trayMenu()); !slices.Contains(labels, "Show / Hide ShelfU") {
		t.Fatalf("the tray menu did not follow the language: %v", labels)
	}
	assertNoHan(t, "tray menu after switch", menuLabels(a.trayMenu()))
}

// TestSettingsLanguageRowSavesAndApplies: choosing English in Settings
// General saves it and asks mygo to show it, without a restart.
func TestSettingsLanguageRowSavesAndApplies(t *testing.T) {
	stubSettingsSeams(t, false, nil, nil)
	orig := setAppLocale
	var applied []string
	setAppLocale = func(tag string) { applied = append(applied, tag) }
	t.Cleanup(func() { setAppLocale = orig })

	a, view := newSettingsShelf(t)
	view.Frame()
	view.Frame()
	view.Click("English")
	if len(applied) != 1 || applied[0] != "en" {
		t.Fatalf("choosing English applied %v, want [en]", applied)
	}
	loaded, err := settings.Load(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Language != "en" {
		t.Fatalf("settings.json kept %q, want en", loaded.Language)
	}

	// Back to following the system.
	view.Frame()
	// The window still says the source language here: the seam replaces the
	// real repaint mygo would do.
	view.Click(locales.In("").LanguageFollowSystem())
	if len(applied) != 2 || applied[1] != "" {
		t.Fatalf("choosing Follow System applied %v, want [en \"\"]", applied)
	}
	if loaded, err = settings.Load(a.dataDir); err != nil || loaded.Language != "" {
		t.Fatalf("settings.json kept %q (err %v), want \"\"", loaded.Language, err)
	}
}

// pageTitleText is the back row's page name in the language of l, as the
// panel asks for it.
func pageTitleText(l locales.Messages, p panelPage) string {
	if t, ok := p.(pageText); ok {
		return t.titleIn(l)
	}
	return p.title()
}

// menuLabels returns every label of m and its submenus.
func menuLabels(m *mygo.Menu) []string {
	var out []string
	var walk func(items []*mygo.MenuItem)
	walk = func(items []*mygo.MenuItem) {
		for _, it := range items {
			out = append(out, it.Label)
			walk(it.Submenu)
		}
	}
	walk(m.Items())
	return out
}

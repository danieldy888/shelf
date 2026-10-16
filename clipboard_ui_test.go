//go:build darwin

package main

// Tests for the clipboard tab: filtering, search, pinned-first order, the
// pin and exclude-app menu items, and the ↑/↓/⏎ keyboard flow. They run
// headless through ui.Tester with a temporary store and data dir; copying
// goes to a test recorder instead of the system clipboard.

import (
	"bytes"
	"image"
	"image/png"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"shelf/internal/settings"
	"shelf/internal/store"
)

// recordClip collects copies for assertions; shelf.writer picks it over
// the system clipboard.
type recordClip struct {
	text string
	png  []byte
}

func (r *recordClip) writeText(text string) error { r.text = text; return nil }
func (r *recordClip) writeImage(b []byte) error   { r.png = b; return nil }

// clipShelf returns a shelf showing texts as clipboard captures (newest
// first, matching store order) plus a headless view and the copy recorder.
func clipShelf(t *testing.T, texts ...string) (*shelf, *ui.Tester, *recordClip) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	// 倒序加入，使 texts[0] 最终在列表最前（newest first）。
	for i := len(texts) - 1; i >= 0; i-- {
		if err := s.AddClipText(texts[i], "com.example.Source"); err != nil {
			t.Fatal(err)
		}
	}
	rec := &recordClip{}
	a := &shelf{
		store: s, dataDir: dir, visible: true, settings: settings.Default(),
		thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{},
		appNames: map[string]string{}, write: rec,
	}
	return a, ui.NewTester(a.view, panelWidth, panelHeight), rec
}

func clipTexts(a *shelf) []string {
	var out []string
	for _, it := range a.visibleItems() {
		out = append(out, it.Text)
	}
	return out
}

func TestClipboardExcluded(t *testing.T) {
	if clipboardExcluded("", []string{"com.example.App"}) {
		t.Error("an unknown source is never excluded")
	}
	if !clipboardExcluded("com.example.App", []string{"com.example.app"}) {
		t.Error("exclusion matches the bundle id without case")
	}
	if !clipboardExcluded("com.example.App", []string{"  com.example.App "}) {
		t.Error("exclusion ignores padding in the setting")
	}
	if clipboardExcluded("com.example.App", nil) || clipboardExcluded("com.example.Other", []string{"com.example.App"}) {
		t.Error("unlisted apps must not be excluded")
	}
}

func TestClipCaptureHonoursExclusions(t *testing.T) {
	types := []string{"public.utf8-plain-text"}
	if got := clipCapture(types, "com.example.App", []string{"com.example.app"}); got != clipSkip {
		t.Fatalf("excluded app still recorded: %v", got)
	}
	if got := clipCapture(types, "com.example.Other", []string{"com.example.app"}); got != clipText {
		t.Fatalf("unlisted app was skipped: %v", got)
	}
	if got := clipCapture([]string{"public.png"}, "", nil); got != clipImage {
		t.Fatalf("unknown source image was skipped: %v", got)
	}
}

// 剪贴板标签只显示 Source 为 clipboard 的条目；托盘标签显示全部。
func TestClipboardTabFiltersToCaptures(t *testing.T) {
	a, view, _ := clipShelf(t, "从剪贴板来的")
	if err := a.store.AddText("拖进来的文字"); err != nil {
		t.Fatal(err)
	}
	view.Frame()
	if !view.HasText("拖进来的文字") || !view.HasText("从剪贴板来的") {
		t.Fatal("tray tab should show everything")
	}
	a.setTab(tabClipboard)
	view.Frame()
	if view.HasText("拖进来的文字") {
		t.Fatal("clipboard tab showed a non-clipboard item")
	}
	if !view.HasText("从剪贴板来的") {
		t.Fatal("clipboard tab missing a capture")
	}
	if !view.HasText("搜索剪贴板") {
		t.Fatal("clipboard tab has no search field")
	}
}

// 搜索是大小写不敏感的子串匹配：只筛文字项，图片在非空查询下不显示，
// 清空搜索后恢复。
func TestClipboardSearchFiltersBySubstring(t *testing.T) {
	a, view, _ := clipShelf(t, "需要退回的购物清单", "Dentist on Tuesday")
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := a.store.AddClipImage(buf.Bytes(), "com.apple.screenshot"); err != nil {
		t.Fatal(err)
	}
	a.setTab(tabClipboard)
	view.Frame()
	if len(a.visibleItems()) != 3 {
		t.Fatalf("want 3 clipboard rows, got %d", len(a.visibleItems()))
	}
	a.search = "dentist"
	view.Frame()
	if got := clipTexts(a); !slices.Equal(got, []string{"Dentist on Tuesday"}) {
		t.Fatalf("search by substring+case failed: %v", got)
	}
	if view.HasText("购物清单") {
		t.Fatal("search left a non-matching row")
	}
	a.search = "清单"
	view.Frame()
	if got := clipTexts(a); !slices.Equal(got, []string{"需要退回的购物清单"}) {
		t.Fatalf("CJK substring search failed: %v", got)
	}
	a.search = ""
	view.Frame()
	if len(a.visibleItems()) != 3 {
		t.Fatalf("empty search must restore images too, got %v", clipTexts(a))
	}
}

// 置顶行排在列表最前，带小图钉；取消置顶后回到原来的位置。
func TestClipboardPinnedFirstAndPinMenu(t *testing.T) {
	a, view, _ := clipShelf(t, "alpha top", "beta mid", "gamma bottom")
	a.setTab(tabClipboard)
	view.Frame()

	// Pin the last item; it must move to the top of the visible list.
	if err := view.RightClick("gamma bottom"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(view.Menu(), "置顶") {
		t.Fatalf("no pin item in %v", view.Menu())
	}
	if err := view.ChooseMenuItem("置顶"); err != nil {
		t.Fatal(err)
	}
	view.Frame()
	if got := clipTexts(a); !slices.Equal(got, []string{"gamma bottom", "alpha top", "beta mid"}) {
		t.Fatalf("pinned item did not come first: %v", got)
	}
	if !view.HasText("已置顶") {
		t.Fatal("pinned row shows no pin mark")
	}

	// Unpin it again from the context menu.
	if err := view.RightClick("gamma bottom"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("取消置顶"); err != nil {
		t.Fatalf("unpin item: %v (%v)", err, view.Menu())
	}
	view.Frame()
	if got := clipTexts(a); !slices.Equal(got, []string{"alpha top", "beta mid", "gamma bottom"}) {
		t.Fatalf("unpinned item did not go back: %v", got)
	}
}

// 右键剪贴板条目可以把来源应用加入排除列表并写入 settings.json。
func TestExcludeAppMenuWritesSettings(t *testing.T) {
	a, view, _ := clipShelf(t, "from the recorder")
	a.setTab(tabClipboard)
	view.Frame()
	a.appNames["com.example.Source"] = "Recorder" // 模拟已解析的显示名
	if err := view.RightClick("from the recorder"); err != nil {
		t.Fatal(err)
	}
	want := "不再记录来自「Recorder」的内容"
	if !slices.Contains(view.Menu(), want) {
		t.Fatalf("no exclude item in %v", view.Menu())
	}
	if err := view.ChooseMenuItem(want); err != nil {
		t.Fatal(err)
	}
	prefs, err := settings.Load(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(prefs.ExcludedApps, "com.example.Source") {
		t.Fatalf("settings.json lacks the excluded app: %+v", prefs.ExcludedApps)
	}
	// 菜单不再重复显示同一应用的条目。
	if err := view.RightClick("from the recorder"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(view.Menu(), want) {
		t.Fatalf("exclude item missing on second open: %v", view.Menu())
	}
	view.CloseMenu()
	// 已排除后来源仍显示同样的标签（选择器按位置+标签匹配）；真正跳过
	// 由 clipCapture 负责。
	a.settingsBroken = true
	if err := view.RightClick("from the recorder"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem(want); err == nil {
		t.Fatal("a broken settings file must disable the exclude item")
	}
	view.CloseMenu()
}

// ↑/↓ 在剪贴板标签移动选中行，⏎ 把它复制到剪贴板并收起托盘。
func TestClipboardKeyboardFlow(t *testing.T) {
	a, view, rec := clipShelf(t, "first clip", "second clip", "third clip")
	a.setTab(tabClipboard)
	a.focusMode = true
	view.Frame()

	if !view.Focused(searchLabel) {
		t.Fatal("search field did not take the keyboard")
	}
	view.Key(0, ui.KeyDown)
	if a.selection.anchor != a.visibleItems()[0].ID {
		t.Fatal("↓ did not select the first row")
	}
	view.Key(0, ui.KeyDown)
	if got, want := a.selection.anchor, a.visibleItems()[1].ID; got != want {
		t.Fatalf("second ↓ landed on %q, want %q", got, want)
	}
	view.Key(0, ui.KeyUp)
	if got, want := a.selection.anchor, a.visibleItems()[0].ID; got != want {
		t.Fatalf("↑ landed on %q, want %q", got, want)
	}
	view.Key(0, ui.KeyEnter)
	if rec.text != "first clip" {
		t.Fatalf("⏎ copied %q, want %q", rec.text, "first clip")
	}
	if a.visible || a.tab != tabTray || a.search != "" {
		t.Fatal("⏎ did not collapse the shelf back to the tray tab")
	}
}

// 搜索框里的 Esc 先清空搜索，再收一次才收起托盘。
func TestEscapeClearsSearchBeforeHiding(t *testing.T) {
	a, view, _ := clipShelf(t, "find me")
	a.setTab(tabClipboard)
	a.focusMode = true
	a.search = "find"
	view.Frame()

	view.Key(0, ui.KeyEscape)
	if !a.visible {
		t.Fatal("Esc hid the shelf while the search still held text")
	}
	if a.search != "" {
		t.Fatal("Esc did not clear the search")
	}
	if !view.Focused(searchLabel) {
		t.Fatal("clearing the search lost the field's focus")
	}
	view.Key(0, ui.KeyEscape)
	if a.visible {
		t.Fatal("a second Esc did not collapse the shelf")
	}
	if !slices.Equal(clipTexts(a), []string{"find me"}) {
		t.Fatal("clearing the search dropped the capture from the list")
	}
}

// 切换到剪贴板标签会把不在其中的选择丢掉；⇧ 选择范围跟着可见列表走。
func TestSelectionFollowsTheVisibleList(t *testing.T) {
	a, view, _ := clipShelf(t, "clip a", "clip b")
	if err := a.store.AddText("tray only"); err != nil {
		t.Fatal(err)
	}
	view.Frame()
	if err := view.Click("tray only"); err != nil {
		t.Fatal(err)
	}
	if a.selection.count() != 1 {
		t.Fatal("click did not select")
	}
	a.setTab(tabClipboard)
	view.Frame()
	if a.selection.count() != 0 {
		t.Fatal("the tray selection survived into the clipboard tab")
	}
	// ⇧ on the clipboard tab spans the visible (pinned-first) order.
	if err := view.Click("clip a"); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickWith(ui.Shift, "clip b"); err != nil {
		t.Fatal(err)
	}
	if a.selection.count() != 2 {
		t.Fatalf("⇧ over the visible list selected %d, want 2", a.selection.count())
	}
}

// 剪贴板图片可以经 ⏎ 以 PNG 形式复制。
func TestEnterCopiesImageAsPNG(t *testing.T) {
	a, _, _ := clipShelf(t)
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := a.store.AddClipImage(buf.Bytes(), "com.apple.screenshot"); err != nil {
		t.Fatal(err)
	}
	a.setTab(tabClipboard)
	a.selection.only(a.visibleItems()[0].ID)
	a.copyClipSelection()
	got := a.write.(*recordClip).png
	if len(got) == 0 {
		t.Fatal("⏎ on an image wrote no PNG")
	}
	if _, err := png.Decode(bytes.NewReader(got)); err != nil {
		t.Fatalf("⏎ on an image did not write a PNG: %v", err)
	}
	if a.visible {
		t.Fatal("copying an image did not collapse the shelf")
	}
}

// hide() 的键盘模式清理：回到托盘标签并交还焦点标记。
func TestHideLeavesKeyboardMode(t *testing.T) {
	a, _, _ := clipShelf(t, "x")
	a.tab, a.search, a.focusMode, a.prevAppPID = tabClipboard, "q", true, 999999
	a.hide()
	if a.tab != tabTray || a.search != "" || a.focusMode || a.prevAppPID != 0 {
		t.Fatalf("hide left keyboard state: tab=%d search=%q focus=%v", a.tab, a.search, a.focusMode)
	}
}

// 菜单的「保留 N 条」与开关写进设置并立即生效。
func TestClipboardMenuSettings(t *testing.T) {
	a, _, _ := clipShelf(t)
	a.setClipboardLimit(100)
	if a.settings.ClipboardLimit != 100 {
		t.Fatal("limit not saved")
	}
	prefs, err := settings.Load(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if prefs.ClipboardLimit != 100 {
		t.Fatalf("settings.json kept %d, want 100", prefs.ClipboardLimit)
	}
	a.setClipboardHistory(false)
	if a.clip != nil || len(a.modules) != 0 {
		t.Fatal("turning history off did not remove the module")
	}
	a.setClipboardHistory(true)
	if a.clip == nil || len(a.modules) != 1 {
		t.Fatal("turning history on did not add the module")
	}
	prefs, err = settings.Load(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !prefs.ClipboardHistory {
		t.Fatal("settings.json did not record history back on")
	}
	a.settingsBroken = true
	a.setClipboardHistory(false)
	if !a.settings.ClipboardHistory {
		t.Fatal("a broken settings file must not be overwritten")
	}
}

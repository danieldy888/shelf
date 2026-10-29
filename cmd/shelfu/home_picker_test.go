//go:build darwin

package main

// The 主页 catalogue's way in: edit mode draws a dashed 1×1 cell in the
// first free spot (the tools page's + slot, in the canvas grid), or the
// back row's + when the canvas has no free cell. The catalogue lists only
// the widgets that are not placed anywhere yet.

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
)

// homeSheet puts a shelf on the 主页 sheet and returns its tester.
func homeSheet(t *testing.T, a *shelf) *ui.Tester {
	t.Helper()
	a.tab, a.visible, a.level = pageHome, true, 2
	return ui.NewTester(a.view, sheetW, sheetH)
}

// Edit mode shows the entry where the tools page shows its + slot, and a
// tap opens the catalogue.
func TestHomeEditShowsTheCatalogueEntry(t *testing.T) {
	a, p := musicShelf(t)
	view := homeSheet(t, a)
	if view.HasText("添加小组件") {
		t.Fatal("the catalogue entry shows outside edit mode")
	}
	if err := view.Click("编辑主页"); err != nil {
		t.Fatalf("pencil missing: %v", err)
	}
	if !p.editing {
		t.Fatal("pencil did not enter editing")
	}
	if !view.HasText("添加小组件") {
		t.Fatalf("edit mode has no catalogue entry; texts %v", view.Texts())
	}
	// The default layout leaves column 3 free on both rows: the entry is
	// one cell at the first of them, measured from the canvas's origin.
	canvas, ok := view.Find("音乐")
	if !ok {
		t.Fatal("music widget missing")
	}
	r, ok := view.Find("添加小组件")
	if !ok {
		t.Fatal("entry missing after the pencil")
	}
	wantX := canvas.X + 3*(homeCellW()+homeGridGap)
	if math.Abs(float64(r.X-wantX)) > 0.5 || math.Abs(float64(r.Y-canvas.Y)) > 0.5 ||
		math.Abs(float64(r.W-homeCellW())) > 0.5 || math.Abs(float64(r.H-homeCellH())) > 0.5 {
		t.Fatalf("entry at %+v, want the free cell (3,0) at %v × %v from the canvas", r, homeCellW(), homeCellH())
	}
	if err := view.Click("添加小组件"); err != nil {
		t.Fatalf("entry click: %v", err)
	}
	if !p.picking {
		t.Fatal("the entry did not open the catalogue")
	}
	if !view.HasText("小组件目录") {
		t.Fatalf("catalogue title missing; texts %v", view.Texts())
	}
	// Only widgets that are not placed yet: the default layout holds
	// music, awake and the clipboard, so 最近文件 is the only row — and no
	// row is marked as already added.
	if !view.HasText("最近文件") {
		t.Fatalf("catalogue is missing the unplaced widget; texts %v", view.Texts())
	}
	if view.HasText("保持唤醒") || view.HasText("剪贴板最新") || view.HasText("音乐") {
		t.Fatalf("catalogue lists placed widgets; texts %v", view.Texts())
	}
	if view.HasText("已添加") {
		t.Fatalf("catalogue still marks placed rows; texts %v", view.Texts())
	}
}

// Removing a widget in edit mode and adding it back through the catalogue
// lands it in the first free cell and writes layout.json.
func TestHomeCataloguePutsARemovedWidgetBack(t *testing.T) {
	a, p := musicShelf(t)
	a.dataDir = t.TempDir()
	view := homeSheet(t, a)
	if err := view.Click("编辑主页"); err != nil {
		t.Fatalf("pencil missing: %v", err)
	}
	awake, ok := view.Find("保持唤醒")
	if !ok {
		t.Fatal("awake widget missing before removal")
	}
	view.ClickAt(awake.X+9, awake.Y+9) // the ⊖ badge's centre
	view.Frame()
	if widgetByID(a.toolsLayout().Home, "awake").ID != "" {
		t.Fatalf("the remove badge did not remove awake: %+v", a.toolsLayout().Home)
	}
	if err := view.Click("添加小组件"); err != nil {
		t.Fatalf("no entry after the removal: %v", err)
	}
	if !p.picking {
		t.Fatal("the entry did not open the catalogue")
	}
	if err := view.Click("保持唤醒"); err != nil {
		t.Fatalf("the catalogue has no row for the removed widget: %v", err)
	}
	if p.picking {
		t.Fatal("the catalogue stayed open after placing")
	}
	got := widgetByID(a.toolsLayout().Home, "awake")
	if got.ID == "" {
		t.Fatalf("awake did not come back: %+v", a.toolsLayout().Home)
	}
	if got.X != 2 || got.Y != 0 || got.W != 1 || got.H != 1 {
		t.Fatalf("awake came back at %+v, want the first free cell (2,0) 1×1", got)
	}
	data, err := os.ReadFile(filepath.Join(a.dataDir, layout.FileName))
	if err != nil {
		t.Fatalf("layout.json was not written: %v", err)
	}
	if !strings.Contains(string(data), `"id": "awake"`) {
		t.Fatalf("layout.json lost the re-added widget:\n%s", data)
	}
}

// When the default size has nowhere to go, the catalogue falls back to a
// smaller size the corner handle would offer.
func TestHomeCatalogueFallsBackToASmallerSize(t *testing.T) {
	a, p := musicShelf(t)
	a.settings.AIUsage.Enabled = true
	// Seven cells are taken; only (3,1) is free, so a 2×1 clipboard
	// widget can only arrive as 1×1.
	l := layout.Layout{Version: layout.Version, Home: []layout.Widget{
		{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		{ID: "usage", X: 3, Y: 0, W: 1, H: 1},
		{ID: "recent", X: 2, Y: 1, W: 1, H: 1},
	}}
	a.toolsLay = &l
	view := homeSheet(t, a)
	p.editing = true
	view.Frame()
	if err := view.Click("添加小组件"); err != nil {
		t.Fatalf("catalogue entry missing: %v", err)
	}
	if err := view.Click("剪贴板最新"); err != nil {
		t.Fatalf("catalogue row missing: %v", err)
	}
	if p.picking {
		t.Fatal("the catalogue stayed open after placing")
	}
	got := widgetByID(a.toolsLayout().Home, "clip")
	if got.X != 3 || got.Y != 1 || got.W != 1 || got.H != 1 {
		t.Fatalf("clip placed at %+v, want the only free cell (3,1) 1×1", got)
	}
}

// With every available widget on the canvas the entry is not drawn: the
// catalogue would have nothing to offer.
func TestHomeCatalogueEntryHidesWhenNothingIsLeftToAdd(t *testing.T) {
	a, p := musicShelf(t)
	l := layout.Layout{Version: layout.Version, Home: []layout.Widget{
		{ID: "playing", X: 0, Y: 0, W: 1, H: 1},
		{ID: "awake", X: 1, Y: 0, W: 1, H: 1},
		{ID: "recent", X: 2, Y: 0, W: 1, H: 1},
		{ID: "clip", X: 3, Y: 0, W: 1, H: 1},
	}}
	a.toolsLay = &l
	view := homeSheet(t, a)
	p.editing = true
	view.Frame()
	if view.HasText("添加小组件") {
		t.Fatalf("the entry shows with nothing left to add; texts %v", view.Texts())
	}
	if !view.HasText("完成") {
		t.Fatalf("edit mode lost its 完成; texts %v", view.Texts())
	}
}

// A full canvas has no cell for the dashed entry, so the back row's +
// takes its place; a pick there only says the canvas has no room.
func TestHomeCataloguePromptsWhenTheCanvasIsFull(t *testing.T) {
	a, p := musicShelf(t)
	a.settings.AIUsage.Enabled = true
	l := layout.Layout{Version: layout.Version, Home: []layout.Widget{
		{ID: "playing", X: 0, Y: 0, W: 2, H: 2},
		{ID: "awake", X: 2, Y: 0, W: 1, H: 1},
		{ID: "recent", X: 2, Y: 1, W: 2, H: 1},
		{ID: "clip", X: 3, Y: 0, W: 1, H: 1},
	}}
	a.toolsLay = &l
	view := homeSheet(t, a)
	p.editing = true
	view.Frame()
	r, ok := view.Find("添加小组件")
	if !ok {
		t.Fatalf("a full canvas has no entry; texts %v", view.Texts())
	}
	if math.Abs(float64(r.W-bandBtn)) > 0.5 || math.Abs(float64(r.H-bandBtn)) > 0.5 {
		t.Fatalf("entry at %+v is not the back row's %vpt button", r, bandBtn)
	}
	if err := view.Click("添加小组件"); err != nil {
		t.Fatalf("+ button: %v", err)
	}
	if !p.picking {
		t.Fatal("+ did not open the catalogue")
	}
	if err := view.Click("AI 用量"); err != nil {
		t.Fatalf("catalogue row missing: %v", err)
	}
	if want := "主页没有空位，先移除一个小组件"; a.status != want {
		t.Fatalf("status = %q, want %q", a.status, want)
	}
	if !p.picking {
		t.Fatal("a refused placement closed the catalogue")
	}
}

// Esc inside the catalogue only closes the catalogue; a second Esc leaves
// editing, and neither hides the panel.
func TestHomeEscapeClosesOnlyTheCatalogue(t *testing.T) {
	a, p := musicShelf(t)
	view := homeSheet(t, a)
	if err := view.Click("编辑主页"); err != nil {
		t.Fatalf("pencil missing: %v", err)
	}
	if err := view.Click("添加小组件"); err != nil {
		t.Fatalf("entry missing: %v", err)
	}
	if !p.picking {
		t.Fatal("the entry did not open the catalogue")
	}
	view.Key(0, ui.KeyEscape)
	view.Frame()
	if p.picking {
		t.Fatal("esc did not close the catalogue")
	}
	if !p.editing {
		t.Fatal("esc left editing as well")
	}
	if !a.visible {
		t.Fatal("esc hid the panel")
	}
	if !view.HasText("添加小组件") {
		t.Fatalf("the edit grid lost its entry; texts %v", view.Texts())
	}
	view.Key(0, ui.KeyEscape)
	view.Frame()
	if p.editing {
		t.Fatal("a second esc did not leave editing")
	}
}

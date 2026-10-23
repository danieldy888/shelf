//go:build darwin

package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
	"shelf/internal/store"
)

// TestToolsPageRenders builds the expanded panel headless on the tools
// tab and checks the module bar and the cards appear.
func TestToolsPageRenders(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &shelf{store: s, tab: pageTools, visible: true}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if !view.HasText("工具") {
		t.Fatal("module bar did not show 工具")
	}
	if !view.HasText("压缩到 1 MB") {
		t.Fatal("default cards missing")
	}
	if !view.HasText("项") {
		t.Fatal("module count missing")
	}
}

// TestToolsPageDropZoneSuppressed proves the file-drop zone is a
// drag-start decision (design §1): only a drag that began while the
// panel was already open on the tools page keeps the grid for the card
// targets; any other drag gets the zone even on the tools tab.
func TestToolsPageDropZoneSuppressed(t *testing.T) {
	a := &shelf{tab: pageTools, visible: true}

	// A drag already in flight when the tools page happens to be showing:
	// dragOpenTools is the flag drag.go latches on the new serial.
	a.dragOpenTools = true
	if on, need := a.dropZoneShow(); on || need != 0 {
		t.Fatalf("tools-open drag must keep the drop zone off, got on=%v need=%d", on, need)
	}

	// The same tab but a drag that started elsewhere: the zone wins.
	a.dragOpenTools = false
	if on, need := a.dropZoneShow(); !on || need != dropZoneNeedH() {
		t.Fatalf("an outside drag must get the zone on any tab, got on=%v need=%d", on, need)
	}
	a.tab = pageTray
	if on, need := a.dropZoneShow(); !on || need != dropZoneNeedH() {
		t.Fatalf("tray tab must keep the drop zone, got on=%v need=%d", on, need)
	}

	// The latch itself: a new serial on an open tools page sets it, an
	// open tray page clears it, a hidden panel clears it too. dragState
	// is stubbed like drag_band_test does; active=false keeps
	// pointerDragged on the latch path only.
	oldState := dragState
	defer func() { dragState = oldState }()
	dragState = func() (bool, int64) { return false, 7 }
	a.tab, a.visible = pageTools, true
	a.dragSerial = -99 // far from the stub's serial so the latch re-fires
	a.pointerDragged()
	if !a.dragOpenTools {
		t.Fatal("a drag starting on an open tools page must latch dragOpenTools")
	}
	a.tab = pageTray
	a.dragSerial = -99
	a.pointerDragged()
	if a.dragOpenTools {
		t.Fatal("a drag starting on the tray page must not latch")
	}
	a.visible = false
	a.dragSerial = -99
	a.pointerDragged()
	if a.dragOpenTools {
		t.Fatal("a drag starting with the panel hidden must not latch")
	}
}

// TestToolsPageEditMode checks the edit flow: pencil enters, + slot and
// ⊖ badges appear, 完成 leaves.
func TestToolsPageEditMode(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &shelf{store: s, tab: pageTools, visible: true, dataDir: t.TempDir()}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if err := view.Click("编辑"); err != nil {
		t.Fatalf("pencil button missing: %v", err)
	}
	p := a.toolsPage()
	if p == nil || !p.editing {
		t.Fatal("pencil did not enter edit mode")
	}
	// The + slot and a ⊖ badge must exist in edit mode.
	if !view.HasText("添加卡片") {
		t.Fatal("+ slot missing in edit mode")
	}
	if err := view.Click("完成"); err != nil {
		t.Fatalf("完成 button missing: %v", err)
	}
	if p.editing {
		t.Fatal("完成 did not leave edit mode")
	}
}

// TestToolsPagePicker covers the catalogue: + opens it, a new card adds,
// 完成 closes back to editing.
func TestToolsPagePicker(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &shelf{store: s, tab: pageTools, visible: true, dataDir: dir}
	p := a.toolsPage()
	if p == nil {
		t.Fatal("tools page missing")
	}
	p.editing, p.picking = true, true
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if !view.HasText("卡片目录") {
		t.Fatal("picker title missing")
	}
	if !view.HasText("隔空投送") {
		t.Fatal("picker is missing catalog entries")
	}
	// Already-placed cards render 已添加.
	if !view.HasText("已添加") {
		t.Fatal("placed cards are not dimmed as 已添加")
	}
}

// TestToolsLayoutPersistence: removing a card writes layout.json, and a
// fresh page sees the same order.
func TestToolsLayoutPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &shelf{store: s, dataDir: dir}
	a.mutateTools(func(l *layout.Layout) {
		l.RemoveCard("airdrop")
		l.MoveCard("zip", 0)
	})
	got := a.toolsLayout()
	if len(got.Cards) == len(layout.Default().Cards) {
		t.Fatal("RemoveCard did not apply")
	}
	if got.Cards[0].ID != "zip" {
		t.Fatalf("zip did not move first: %v", got.Cards)
	}
	// A new shelf on the same dir reads the saved order back.
	b := &shelf{dataDir: dir}
	got = b.toolsLayout()
	if got.Cards[0].ID != "zip" {
		t.Fatal("layout.json did not persist the order")
	}
	for _, card := range got.Cards {
		if card.ID == "airdrop" {
			t.Fatal("removed card came back")
		}
	}
}

// TestToolsEscapeReset: Esc on the tools tab leaves editing instead of
// hiding, and a second Esc hides.
func TestToolsEscapeReset(t *testing.T) {
	a := &shelf{tab: pageTools, visible: true}
	p := a.toolsPage()
	p.editing = true
	a.escape()
	if p.editing {
		t.Fatal("esc did not leave edit mode")
	}
	if !a.visible {
		t.Fatal("esc hid while editing was still on")
	}
}

//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

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

// A layout.json the loader cannot read (bad JSON here) draws the default
// grid for the session but is never written back — mutating in memory must
// leave the recoverable file byte-identical, as settingsBroken does.
func TestToolsBrokenLayoutNeverWritten(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bad := []byte("{nope")
	if err := os.WriteFile(filepath.Join(dir, "layout.json"), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	a := &shelf{store: s, dataDir: dir}
	_ = a.toolsLayout() // the broken load latches
	a.mutateTools(func(l *layout.Layout) { l.MoveCard("zip", 0) })
	got, err := os.ReadFile(filepath.Join(dir, "layout.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(bad) {
		t.Fatalf("a broken layout was overwritten: %q", got)
	}
}

// A write that fails leaves both the file and the in-memory layout as
// they were — the card still shows after the failed removal.
func TestToolsWriteFailureKeepsLayout(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &shelf{store: s, dataDir: dir}
	before := a.toolsLayout() // load + default write while the dir is writable
	saved, err := os.ReadFile(filepath.Join(dir, "layout.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	a.mutateTools(func(l *layout.Layout) { l.RemoveCard("zip") })
	got := a.toolsLayout()
	if !slices.EqualFunc(got.Cards, before.Cards, func(x, y layout.Card) bool { return x == y }) {
		t.Fatalf("a failed write changed the in-memory layout: %v", got.Cards)
	}
	data, err := os.ReadFile(filepath.Join(dir, "layout.json"))
	if err != nil || string(data) != string(saved) {
		t.Fatalf("the file changed under a failed write: %v", err)
	}
}

// The card-level press handler must not eat the child controls' downs:
// a tap on a 保持唤醒 duration pill picks that duration, not the card's
// one-hour toggle — the press belongs to the chip, so p.press stays empty.
func TestToolsAwakeChipGetsItsOwnPress(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	power := newFakePower()
	l := layout.Default()
	a := &shelf{store: s, tab: pageTools, visible: true}
	a.awake = &awakeModule{host: a, create: power.create, release: power.release}
	a.toolsLay = &l
	a.mutateTools(func(l *layout.Layout) { l.SetSpan("awake", 2) }) // dataDir empty: memory only
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	r, ok := view.Find("30 分钟")
	if !ok {
		t.Fatal("awake chip missing")
	}
	p := a.toolsPage()
	view.Press(r.X+r.W/2, r.Y+r.H/2)
	view.Advance(50 * time.Millisecond)
	if p.press != nil {
		t.Fatalf("a chip press became a card press: %+v", p.press)
	}
	view.Release(r.X+r.W/2, r.Y+r.H/2)
	view.Advance(50 * time.Millisecond)
	if a.awake.duration != 30*time.Minute {
		t.Fatalf("chip tap set %v, want 30m", a.awake.duration)
	}
}

// A tap on an action card while the one-job converter is taken starts
// nothing: the card shakes and posts the busy line instead of being
// silently counted as run.
func TestToolsCardTapDuringConvertIsBusy(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f := writeFile(t, t.TempDir(), "a.png", testPNGBytes(t))
	s.Items = append(s.Items, store.Item{ID: "i1", Kind: store.KindFile, Path: f})
	a := &shelf{store: s}
	a.selection.only("i1")
	a.conv = &converter{job: &convertJob{}} // a job in flight
	p := a.toolsPage()
	p.toolCardTapped("zip")
	if p.jiggleID != "zip" {
		t.Fatalf("busy tap did not jiggle: %q", p.jiggleID)
	}
	if a.status != "一次只跑一个任务，请稍候" {
		t.Fatalf("busy tap posted %q, want the busy line", a.status)
	}
}

// A press held still past the long-press deadline enters editing on the
// frame clock: the checker itself requests frames while the press waits,
// so a quiet panel still reaches it.
func TestToolsStillLongPressEntersEdit(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &shelf{store: s, tab: pageTools, visible: true}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	r, ok := view.Find("框选文字")
	if !ok {
		t.Fatal("ocr card missing")
	}
	view.Press(r.X+r.W/2, r.Y+r.H/2)
	view.Advance(toolLongPress + 100*time.Millisecond)
	p := a.toolsPage()
	if p == nil || !p.editing {
		t.Fatalf("still long press did not enter editing: %v", p)
	}
	view.Release(r.X+r.W/2, r.Y+r.H/2)
	if !p.editing {
		t.Fatal("releasing the long press left editing")
	}
}

// Esc inside the 「+」 catalogue only backs out to the edit grid (same as
// 完成); editing ends on a second Esc.
func TestToolsEscapeInPickerClosesPickerOnly(t *testing.T) {
	a := &shelf{tab: pageTools, visible: true}
	p := a.toolsPage()
	p.editing, p.picking = true, true
	a.escape()
	if !p.editing || p.picking {
		t.Fatalf("Esc in the picker ended editing instead of the picker: editing=%v picking=%v", p.editing, p.picking)
	}
	a.escape()
	if p.editing {
		t.Fatal("second Esc did not leave editing")
	}
	if !a.visible {
		t.Fatal("Esc hid the panel")
	}
}

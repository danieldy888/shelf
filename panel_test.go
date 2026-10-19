//go:build darwin

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/store"
)

func TestLongTextPreviewKeepsFullPayload(t *testing.T) {
	text := "开头 😀\n\t" + strings.Repeat("正文🙂 ", 400) + "不可丢失的末尾"
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AddText(text); err != nil {
		t.Fatal(err)
	}
	a := &shelf{store: s}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if !view.HasText("开头 😀") {
		t.Fatal("text preview lost its readable beginning")
	}
	for _, label := range view.Texts() {
		if !utf8.ValidString(label) {
			t.Fatal("text preview split a UTF-8 character")
		}
		if utf8.RuneCountInString(label) > 200 {
			t.Fatal("long clipboard text was exposed to layout instead of a bounded preview")
		}
		if strings.Contains(label, "不可丢失的末尾") {
			t.Fatal("preview rendered the distant tail of the full text")
		}
	}
	payload, err := a.dragData(s.Items[0]).Read(transfer.Text)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != text || s.Items[0].Text != text {
		t.Fatal("shortening the preview truncated the stored or exported text")
	}
}

// 有内容时头部出现"清空"，每项仍保留独立移除按钮；单项移除只删该项，
// 清空则删除全部、丢掉缩略图缓存、回到空态且不再显示"清空"。
func TestClearAllButtonAndPerItemRemove(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dir := t.TempDir()
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(note, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.AddFiles([]string{note}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddText("一段文字"); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := s.AddImage(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	a := &shelf{store: s, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if !view.HasText("清空") {
		t.Fatal("clear-all button missing with items present")
	}
	// 移除入口在右键菜单里（设计去掉了行内按钮）。
	for _, it := range s.Items {
		title := itemTitle(it)
		if err := view.RightClick(title); err != nil {
			t.Fatalf("right-click %q: %v", title, err)
		}
		if !slices.Contains(view.Menu(), "移除") {
			t.Fatalf("per-item menu for %q lacks 移除: %v", title, view.Menu())
		}
		view.CloseMenu()
	}
	// 缩略图在途解码必须在清空时作废。
	if len(a.thumbPending) == 0 {
		t.Fatal("expected in-flight thumbnail decodes for file and image items")
	}

	if err := view.RightClick("note.txt"); err != nil {
		t.Fatalf("right-click note.txt: %v", err)
	}
	if err := view.ChooseMenuItem("移除"); err != nil {
		t.Fatalf("choose per-item remove: %v (%v)", err, view.Menu())
	}
	if len(s.Items) != 2 {
		t.Fatalf("per-item remove should keep the rest, got %d items", len(s.Items))
	}
	for _, it := range s.Items {
		if it.Kind == store.KindFile {
			t.Fatal("per-item remove deleted the wrong item")
		}
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("original file deleted by remove: %v", err)
	}
	if !view.HasText("清空") {
		t.Fatal("clear-all button disappeared while items remain")
	}

	if err := view.Click("清空"); err != nil {
		t.Fatalf("click clear-all: %v", err)
	}
	if len(s.Items) != 0 {
		t.Fatalf("clear-all left %d items", len(s.Items))
	}
	if !view.HasText("把文件拖到这里") {
		t.Fatal("empty state not shown after clear-all")
	}
	if view.HasText("清空") {
		t.Fatal("clear-all button still rendered on empty shelf")
	}
	for id := range a.thumbPending {
		if !a.thumbDiscard[id] {
			t.Fatalf("in-flight thumbnail %s not marked for discard", id)
		}
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("original file deleted by clear-all: %v", err)
	}
}

// Each page's 清空 removes only what that page shows: clearing the tray
// keeps the clipboard history, and the other way round. Pinned rows stay.
func TestClearOnlyEmptiesItsOwnPage(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dir := t.TempDir()
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(note, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.AddFiles([]string{note}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddClipText("复制的文字", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AddClipText("置顶的文字", ""); err != nil {
		t.Fatal(err)
	}
	var pinned string
	for _, it := range s.Items {
		if it.Text == "置顶的文字" {
			pinned = it.ID
		}
	}
	if err := s.SetPinned(pinned, true); err != nil {
		t.Fatal(err)
	}
	a := &shelf{store: s, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if err := view.Click("清空"); err != nil {
		t.Fatalf("tray clear: %v", err)
	}
	var left []string
	for _, it := range s.Items {
		left = append(left, it.Text)
	}
	if len(left) != 2 || !slices.Contains(left, "复制的文字") || !slices.Contains(left, "置顶的文字") {
		t.Fatalf("the tray's 清空 must remove only the file and keep the clipboard history: %q", left)
	}
	a.setTab(pageClipboard)
	a.search = "复制"
	view.Frame()
	if view.HasText("清空") {
		t.Fatal("a narrowed clipboard search must not offer 清空")
	}
	a.search = ""
	view.Frame()
	if err := view.Click("清空"); err != nil {
		t.Fatalf("clipboard clear: %v", err)
	}
	if len(s.Items) != 1 || s.Items[0].ID != pinned {
		t.Fatalf("the clipboard's 清空 must keep only the pinned row, left %+v", s.Items)
	}
}

// On a notched display the ear bar sits in the notch band beside the camera
// housing — never over it — and the page content starts below the notch.
// While the clipboard search holds the keyboard the window drops under the
// menu bar, so the bar moves below it.
func TestEarBarFlanksTheNotch(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AddText("有内容"); err != nil {
		t.Fatal(err)
	}
	const notchW, notchH = 180, 32
	a := &shelf{store: s, topInset: notchH, notchW: notchW, notchH: notchH, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	housingL := float32(panelWidth-notchW) / 2
	housingR := housingL + notchW
	for _, label := range []string{"托盘", "剪贴板", "设置", "收起"} {
		r, ok := view.Find(label)
		if !ok {
			t.Fatalf("ear button %q missing", label)
		}
		if r.Y+r.H > notchH {
			t.Fatalf("ear button %q at %+v leaves the notch band", label, r)
		}
		if r.X+r.W > housingL && r.X < housingR {
			t.Fatalf("ear button %q at %+v sits under the camera housing [%v, %v]", label, r, housingL, housingR)
		}
	}
	if r, ok := view.Find("清空"); !ok || r.Y < notchH {
		t.Fatalf("the module bar's 清空 must sit below the notch: %+v %v", r, ok)
	}
	if err := view.Click("剪贴板"); err != nil || a.tab != pageClipboard {
		t.Fatalf("the ear icon must switch pages: %v tab=%s", err, a.tab)
	}
	a.focusMode = true
	view.Frame()
	for _, label := range []string{"托盘", "收起"} {
		if r, ok := view.Find(label); !ok || r.Y < notchH {
			t.Fatalf("with the keyboard taken, %q must move below the menu bar: %+v %v", label, r, ok)
		}
	}
	if err := view.Click("收起"); err != nil {
		t.Fatalf("collapse button not clickable: %v", err)
	}
}

// The activity strip holds the first liveMax modules with something under
// way, on every page; sharing the strip asks each for its compact form.
func TestActivityStripShowsUpToTwoLiveModules(t *testing.T) {
	idle := &fakeActivity{label: "空闲"}
	first := &fakeActivity{label: "第一个活动", isLive: true}
	second := &fakeActivity{label: "第二个活动", isLive: true}
	third := &fakeActivity{label: "第三个活动", isLive: true}
	a := &shelf{tab: pageTray}
	a.modules = []module{idle, first, second, third}
	for _, tab := range []pageID{pageTray, pageClipboard} {
		a.tab = tab
		first.compact, second.compact = false, false
		view := ui.NewTester(a.view, panelWidth, panelHeight)
		if view.HasText("空闲") || view.HasText("第三个活动") {
			t.Fatalf("page %s: an idle activity and a third live one must not show", tab)
		}
		if !view.HasText("第一个活动") || !view.HasText("第二个活动") {
			t.Fatalf("page %s: the first two live activities must show", tab)
		}
		if !first.compact || !second.compact {
			t.Fatalf("page %s: two activities side by side must both be compact", tab)
		}
	}
	a.modules = []module{first}
	ui.NewTester(a.view, panelWidth, panelHeight)
	if first.compact {
		t.Fatal("a lone activity must get the full strip")
	}
}

type fakeActivity struct {
	label   string
	isLive  bool
	compact bool
}

func (f *fakeActivity) tick(time.Time) {}
func (f *fakeActivity) live() bool     { return f.isLive }

func (f *fakeActivity) activity(c *ui.Context, compact bool) {
	f.compact = compact
	ui.Text(c, f.label).FontSize(12).TextColor(ink)
}

// Resting on an ear icon shows its name in a tip the user can read: text
// that stands out from the tip's own fill (regression: the panel theme's
// transparent background left the text invisible on a blank white chip).
func TestEarIconTooltipIsReadable(t *testing.T) {
	a := &shelf{topInset: 32, notchW: 180, notchH: 32}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	r, ok := view.Find("剪贴板")
	if !ok {
		t.Fatal("clipboard ear icon missing")
	}
	before := view.Image()
	view.Move(r.X+r.W/2, r.Y+r.H/2)
	view.Advance(time.Second)
	after := view.Image()
	var changed, bright int
	b := after.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r0, g0, b0, _ := before.At(x, y).RGBA()
			r1, g1, b1, _ := after.At(x, y).RGBA()
			if r0 == r1 && g0 == g1 && b0 == b1 {
				continue
			}
			changed++
			if (r1+g1+b1)/3 > 0xB000 {
				bright++
			}
		}
	}
	if changed < 200 {
		t.Fatalf("no tooltip appeared after resting on the icon (%d pixels changed)", changed)
	}
	// Readable text: some bright glyph pixels on a fill that is mostly not
	// the glyphs' colour.
	if bright < 20 || bright > changed/2 {
		t.Fatalf("tooltip text not readable: %d bright of %d changed pixels", bright, changed)
	}
}

// 探头态（peeking）下刘海胶囊显示展开提示，单击后托盘标记为展开；
// 无窗口环境下窗口相关操作安全跳过。
func TestNotchPeekClickExpandsShelf(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &shelf{store: s, peeking: true, peekProgress: 1, notchW: 182, notchH: 32, topInset: 32}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if _, ok := view.Find("展开 Shelf"); !ok {
		t.Fatal("peek pill missing its expand affordance (chevron)")
	}
	if view.HasText("收起") {
		t.Fatal("peek state should not render the expanded header")
	}
	if err := view.Click("展开 Shelf"); err != nil {
		t.Fatalf("peek pill not clickable: %v", err)
	}
	if !a.visible {
		t.Fatal("clicking the peek pill did not expand the shelf")
	}
	a.progress = 1 // 展开动画在无窗口测试里不推进，直接到位。
	view.Frame()
	if _, ok := view.Find("收起"); !ok {
		t.Fatal("expanded shelf missing the collapse button after peek click")
	}
}

// 刘海几何无效（notchW==0，NotchRect 未匹配到显示器）时悬停与拖拽触发区
// 都必须关闭，否则 (0,0) 附近会凭空出现探头/展开热区，托盘自行弹出。
func TestNoNotchGeometryDisablesTriggers(t *testing.T) {
	a := &shelf{}
	for _, p := range []mygo.Point{{X: 0, Y: 0}, {X: 100, Y: 20}, {X: 646, Y: 30}} {
		if a.overNotch(p) {
			t.Fatalf("overNotch(%v) true with no notch geometry", p)
		}
		if a.nearTrigger(p) {
			t.Fatalf("nearTrigger(%v) true with no notch geometry", p)
		}
	}
}

// 有效刘海几何时：悬停区紧贴刘海（含少量余量），拖拽触发区以其中心为准；
// 屏幕左上角和远离刘海的位置都不得命中。
func TestNotchTriggersBoundToRealRect(t *testing.T) {
	a := &shelf{notchX: 645, notchY: 0, notchW: 180, notchH: 32}
	if !a.overNotch(mygo.Point{X: 735, Y: 20}) {
		t.Fatal("cursor at notch center should hover")
	}
	if a.overNotch(mygo.Point{X: 100, Y: 20}) {
		t.Fatal("cursor far left of notch must not hover")
	}
	if a.overNotch(mygo.Point{X: 735, Y: 60}) {
		t.Fatal("cursor below hover pad must not hover")
	}
	if !a.nearTrigger(mygo.Point{X: 735, Y: 40}) {
		t.Fatal("cursor in top-center trigger zone should activate drag trigger")
	}
	if a.nearTrigger(mygo.Point{X: 100, Y: 40}) {
		t.Fatal("cursor far from notch center must not activate drag trigger")
	}
}

// 拖拽会话一旦激活，整块 overlay 都必须接收事件。空闲时 ignoringMouse 表示
// 「刘海以下穿透」，刘海条本身仍然命中，否则文件拖到 Y=0 会打开桌面栏。
func TestDragSessionDisablesPassThrough(t *testing.T) {
	a := &shelf{dragging: true, ignoringMouse: true}
	a.syncMousePassThrough()
	if a.ignoringMouse {
		t.Fatal("active drag must clear ignoringMouse unconditionally")
	}
	b := &shelf{peeking: true, ignoringMouse: true, notchX: 600, notchW: 180, notchH: 32}
	b.syncMousePassThrough()
	if b.ignoringMouse {
		t.Fatal("peeking shelf must accept mouse events so the pill is clickable")
	}
	d := &shelf{pointerDrag: true, ignoringMouse: true, notchX: 600, notchW: 180, notchH: 32}
	d.syncMousePassThrough()
	if d.ignoringMouse {
		t.Fatal("the first mouse-dragged event of a file drag must make the overlay a drop target")
	}
	c := &shelf{notchH: 32}
	c.syncMousePassThrough()
	if !c.ignoringMouse {
		t.Fatal("idle collapsed shelf must stay click-through below the notch")
	}
}

func TestPeekPillCoversExpandHint(t *testing.T) {
	a := &shelf{notchX: 645, notchY: 0, notchW: 180, notchH: 32, y: 0}
	// Hint is bottom-aligned in the capsule (padding 7, 11pt type).
	hint := mygo.Point{X: a.notchX + a.notchW/2, Y: max(32, a.notchH) + 18 - 8}
	if !a.overPeekPill(hint) {
		t.Fatalf("expand hint at %v is outside peek pill %+v", hint, a.peekPillRect())
	}
	if a.overPeekPill(mygo.Point{X: 80, Y: 20}) {
		t.Fatal("far-left cursor must not count as a pill click")
	}
}

func TestMouseDownOnPeekPillExpandsEvenIfEventWentElsewhere(t *testing.T) {
	a := &shelf{peeking: true, peekProgress: 1, notchX: 645, notchY: 0, notchW: 180, notchH: 32, y: 0}
	hint := mygo.Point{X: a.notchX + a.notchW/2, Y: max(32, a.notchH) + 10}
	a.mouseDown(false, hint)
	if !a.visible {
		t.Fatal("clicking the visible peek pill must expand even when WindowServer gave the press to another app")
	}
}

func TestMouseDownAwayFromPeekDoesNotExpand(t *testing.T) {
	a := &shelf{peeking: true, peekProgress: 1, notchX: 645, notchY: 0, notchW: 180, notchH: 32, y: 0}
	a.mouseDown(false, mygo.Point{X: 80, Y: 40})
	if a.visible {
		t.Fatal("a click far from the pill must not expand the shelf")
	}
}

// Hover must not move or resize the native surface; only the drawing changes.
// Keeping the strip anchored also prevents it from covering the Dock.
func TestHoverKeepsWindowAnchored(t *testing.T) {
	a := &shelf{x: 470, y: 0, h: 380, notchX: 600, notchW: 180, notchH: 32}
	idle := a.desiredFrame()
	for _, progress := range []float32{0, 0.2, 0.8, 1, 0.4, 0} {
		a.peeking = progress > 0
		a.peekProgress = progress
		if got := a.desiredFrame(); got != idle {
			t.Fatalf("hover remapped the surface: %+v -> %+v", idle, got)
		}
	}
	if idle.Y != 0 || idle.Y+idle.Height >= 380 {
		t.Fatalf("collapsed surface must stay at the top, far above the Dock: %+v", idle)
	}
	a.dragging = true
	if got := a.desiredFrame(); got != idle {
		t.Fatalf("starting a drag must keep the same approach surface: %+v", got)
	}
	a.visible = true
	if got := a.desiredFrame(); got.Y != idle.Y || got.Height <= idle.Height {
		t.Fatalf("opening must grow downwards without moving the top edge: %+v", got)
	}
}

// The event filter's landing line has to sit inside the collapsed drop
// strip. Otherwise clamped drags miss the shelf and still hit the screen
// edge that opens Mission Control.
func TestDragGuardLandsInsideCollapsedStrip(t *testing.T) {
	a := &shelf{x: 470, y: 0, h: 380, notchX: 600, notchW: 180, notchH: 32}
	strip := a.desiredFrame()
	g := a.dragGuardRegion()
	if g.Y != strip.Y {
		t.Fatalf("guard top %+v must share the strip top %+v", g, strip)
	}
	if g.Y+g.Height > strip.Y+strip.Height {
		t.Fatalf("guard landing %+v misses drop strip %+v", g, strip)
	}
	if g.X < strip.X || g.X+g.Width > strip.X+strip.Width {
		t.Fatalf("guard corridor %+v is wider than drop strip %+v", g, strip)
	}
	if g.Height <= 38 {
		t.Fatalf("guard landing at %dpt is still inside the menu bar; Spaces would fire", g.Height)
	}
}

// 探头/拖拽态的黑壳必须与窗口顶边（Y=0）齐平：光标落在窗口第一行时事件才
// 会打在托盘上，守卫放行段交出去的拖放才有地方落下。壳只在水平方向居中，
// 窗口顶边其余部分不属于托盘——那一段由守卫夹取，拖放交给桌面与下层应用。
// 测试环境没有真实窗口，tester 尺寸即窗口帧。
func TestPeekShellFlushWithWindowTop(t *testing.T) {
	a := &shelf{peeking: true, peekProgress: 1, notchW: 180, notchH: 32}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	img := view.Image()
	b := img.Bounds()
	first, last := -1, -1
	for x := range b.Dx() {
		if _, _, _, alpha := img.At(x, 0).RGBA(); alpha > 0 {
			if first < 0 {
				first = x
			}
			last = x
		}
	}
	if first < 0 {
		t.Fatal("peek shell does not touch the window's top edge: a drop at Y=0 would miss the shelf")
	}
	if first == 0 || last == b.Dx()-1 {
		t.Fatalf("peek shell spans the whole top edge (%d..%d): the shelf would swallow drops meant for the desktop", first, last)
	}
}

func TestPeekLeavesNoPaintWhenPointerLeaves(t *testing.T) {
	// A non-nil window selects the production rendering branch; the tester
	// supplies the surface and never invokes a native window operation here.
	a := &shelf{win: &mygo.Window{}, notchW: 180, notchH: 32}
	view := ui.NewTester(a.view, 280, 72)
	for range 12 {
		a.peeking, a.peekProgress = true, 1
		view.Frame()
		img := view.Image()
		if _, _, _, alpha := img.At(140, 2).RGBA(); alpha == 0 {
			t.Fatal("peek did not paint at the top")
		}
		for x := range img.Bounds().Dx() {
			if _, _, _, alpha := img.At(x, img.Bounds().Dy()-1).RGBA(); alpha != 0 {
				t.Fatal("peek painted beyond its bottom boundary")
			}
		}
		a.peeking, a.peekProgress = false, 0
		view.Frame()
		img = view.Image()
		for y := range img.Bounds().Dy() {
			for x := range img.Bounds().Dx() {
				if _, _, _, alpha := img.At(x, y).RGBA(); alpha != 0 {
					t.Fatalf("collapsed surface retained paint at %d,%d", x, y)
				}
			}
		}
		if a.visible {
			t.Fatal("hover must never expand the shelf")
		}
	}
}

// 置顶角标必须落在各自格子的右上角，而不是叠在网格层（回归：角标曾是
// 网格的 Absolute 子元素，全部挤在网格右上角）。
func TestPinBadgesStayInsideTheirTiles(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AddText("pinned one"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddText("pinned two"); err != nil {
		t.Fatal(err)
	}
	for _, it := range s.Items {
		if err := s.SetPinned(it.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	a := &shelf{store: s, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	tile, ok := view.Find("pinned two") // 最新置顶项排第一格
	if !ok {
		t.Fatal("pinned tile missing")
	}
	badge, ok := view.Find("已置顶")
	if !ok {
		t.Fatal("pin badge missing")
	}
	if badge.X < tile.X || badge.X+badge.W > tile.X+tile.W ||
		badge.Y < tile.Y || badge.Y+badge.H > tile.Y+tile.H {
		t.Fatalf("pin badge %+v outside its tile %+v", badge, tile)
	}
}

// The panel's height follows the page: it grows with the tray's rows until
// the stage reaches stageMaxH (then the stage scrolls), and an activity
// adds exactly its strip. desiredFrame is what syncWindowFrame applies on
// every tick.
func TestPanelHeightFollowsVisibleItems(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &shelf{store: s, visible: true, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	// place() 需要真实显示器（AppKit）；无窗口测试直接给定几何。
	a.dispBounds = mygo.Rectangle{X: 0, Y: 0, Width: 1470, Height: 956}
	a.x, a.y, a.h = 455, 0, 900
	add := func(n int) {
		for range n {
			if err := s.AddText(fmt.Sprintf("row %d", len(s.Items))); err != nil {
				t.Fatal(err)
			}
		}
	}
	empty := a.desiredFrame().Height
	add(gridCols + 1) // two rows: past the empty stage's minimum
	twoRows := a.desiredFrame().Height
	if twoRows <= empty {
		t.Fatalf("panel did not grow with items: %d -> %d", empty, twoRows)
	}
	add(gridCols * 4) // six rows: past stageMaxH
	full := a.desiredFrame().Height
	add(gridCols)
	if got := a.desiredFrame().Height; got != full || full-twoRows != stageMaxH-(2*tileH+gridGap) {
		t.Fatalf("a full stage must stop growing at stageMaxH: two rows %d, full %d, more %d", twoRows, full, got)
	}
	a.settings.IslandNowPlaying = true
	a.np = &nowPlayingModule{host: a, have: true, last: mac.NowPlayingState{Title: "T", Artist: "A"}}
	a.modules = []module{a.np}
	if got := a.desiredFrame().Height; got != full+liveH+liveGap {
		t.Fatalf("the activity strip must add exactly its own height: %d -> %d", full, got)
	}
}

// Switching pages must not resize the panel: the stage is sized for the
// taller page, so a short tray and a long clipboard history share one
// height and the switch is only a change of content.
func TestPageSwitchKeepsPanelHeight(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AddText("托盘里一条"); err != nil {
		t.Fatal(err)
	}
	for i := range 4 { // 4 rows: taller than the tray's one row, under stageMaxH
		if err := s.AddClipText(fmt.Sprintf("剪贴板 %d", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	a := &shelf{store: s, visible: true, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	a.dispBounds = mygo.Rectangle{X: 0, Y: 0, Width: 1470, Height: 956}
	a.x, a.y, a.h = 455, 0, 900
	tray := a.desiredFrame().Height
	a.setTab(pageClipboard)
	if clip := a.desiredFrame().Height; clip != tray {
		t.Fatalf("switching to the clipboard resized the panel: %d -> %d", tray, clip)
	}
	// Content coming in still grows it.
	if err := s.AddClipText("再来一条", ""); err != nil {
		t.Fatal(err)
	}
	if got := a.desiredFrame().Height; got <= tray {
		t.Fatalf("a longer clipboard list must still grow the panel: %d -> %d", tray, got)
	}
}

// fakePage is a registered page outside the height-sharing group: the
// panel must follow its own stage height instead of the tray/clipboard
// group max. on flips enabled().
type fakePage struct {
	on     bool
	height int
}

func (p *fakePage) id() pageID    { return "fake" }
func (p *fakePage) label() string { return "假页面" }
func (p *fakePage) icon(c *ui.Context, _ bool) ui.Element {
	return ui.Icon(c, textSymbol)
}
func (p *fakePage) enabled() bool      { return p.on }
func (p *fakePage) sharesHeight() bool { return false }
func (p *fakePage) stageH() int        { return p.height }
func (p *fakePage) body(c *ui.Context, _ bool, _ bool) ui.Element {
	return ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		ui.Text(c, "假页面内容").FontSize(13).TextColor(ink)
	})
}
func (p *fakePage) moduleTitle() (title, count string) { return "假页面", "" }
func (p *fakePage) moduleTrailing(c *ui.Context, _ bool) []ui.Element {
	return nil
}

// A page that opts out of height sharing owns the stage height: switching
// to it eases the panel to its own measure, and switching back to a
// sharing page restores the group height — while tray↔clipboard stays
// constant (TestPageSwitchKeepsPanelHeight).
func TestNonSharingPageDrivesPanelHeight(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AddText("托盘里一条"); err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		if err := s.AddClipText(fmt.Sprintf("剪贴板 %d", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	a := &shelf{store: s, visible: true, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	a.dispBounds = mygo.Rectangle{X: 0, Y: 0, Width: 1470, Height: 956}
	a.x, a.y, a.h = 455, 0, 900

	fake := &fakePage{on: true, height: 260}
	a.registerPage(fake)
	if got := a.pageIndex("fake"); got != 2 {
		t.Fatalf("the fake page must register after the two built-ins, got index %d", got)
	}

	shared := a.desiredFrame().Height // tray: the group's taller stage (clipboard)
	a.setTab(pageClipboard)
	if got := a.desiredFrame().Height; got != shared {
		t.Fatalf("tray→clipboard must keep the group height: %d -> %d", shared, got)
	}
	a.setTab("fake")
	fixed := spacerH() + min(max(fake.stageH(), stageMinH), stageMaxH)
	if got := a.desiredFrame().Height; got != fixed {
		t.Fatalf("a non-sharing page must own the stage height: want %d, got %d", fixed, got)
	}
	fake.height = 190
	if got := a.desiredFrame().Height; got != spacerH()+190 {
		t.Fatalf("the panel must follow the page's own stageH: want %d, got %d", spacerH()+190, got)
	}
	a.setTab(pageTray)
	if got := a.desiredFrame().Height; got != shared {
		t.Fatalf("switching back must restore the group height: %d -> %d", fake.stageH(), got)
	}
}

// spacerH is panelContentH without its stage: ears, module bar and the
// bottom padding (no activity, no search hint).
func spacerH() int {
	a := &shelf{}
	spacer, rowH, _ := a.earLayout()
	return spacer + rowH + earBottomGap + moduleBarH + stageGap + panelPadBottom
}

// Switching off the current page (e.g. AI 用量's setting) folds the panel
// back to the tray on the next frame; its ear icon also disappears.
func TestDisabledCurrentPageFallsBackToTray(t *testing.T) {
	a := &shelf{visible: true, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	fake := &fakePage{on: true, height: 200}
	a.registerPage(fake)
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	a.setTab("fake")
	view.Frame()
	if !view.HasText("假页面内容") {
		t.Fatal("the fake page did not render")
	}
	fake.on = false
	view.Frame()
	if a.tab != pageTray || a.currentPage().id() != pageTray {
		t.Fatalf("a disabled current page must fall back to the tray, tab=%s", a.tab)
	}
	if view.HasText("假页面") {
		t.Fatal("a disabled page kept its ear icon and module bar")
	}
	// Re-enabling brings the icon back but the panel stays on the tray.
	fake.on = true
	view.Frame()
	if a.tab != pageTray {
		t.Fatal("the fallback must not be undone by re-enabling the page")
	}
	if _, ok := view.Find("假页面"); !ok {
		t.Fatal("the re-enabled page's ear icon did not come back")
	}
}

// While the shell eases down to a shorter panel (a tab switch), the window
// must keep the drawn height, or the shell's bottom is cut off; once the
// drawn height arrives, the frame follows the content again.
func TestPanelFrameHoldsWhileShrinking(t *testing.T) {
	a := &shelf{visible: true}
	a.x, a.y, a.h = 475, 0, 560
	target := a.panelH()
	a.shownH = float32(target + 120)
	if got := a.desiredFrame().Height; got != target+120 {
		t.Fatalf("frame %d cuts the shell drawn at %d", got, target+120)
	}
	a.shownH = float32(target)
	if got := a.desiredFrame().Height; got != target {
		t.Fatalf("frame %d did not settle to the content height %d", got, target)
	}
}

// 托盘页的「清空」对剪贴板收录只做隐藏：截图从托盘消失但留在剪贴板页，
// 受管 PNG 不删；被拖入的文件仍被真正删除。再到剪贴板页点「清空」才把
// 截图连图一起删掉。
func TestTrayClearHidesClipboardCaptureInsteadOfDeleting(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dir := t.TempDir()
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(note, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.AddFiles([]string{note}); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := s.AddClipImage(buf.Bytes(), "com.example.Shot"); err != nil {
		t.Fatal(err)
	}
	var clip store.Item
	for _, it := range s.Items {
		if it.Kind == store.KindImage {
			clip = it
		}
	}
	managed := clip.Path
	a := &shelf{store: s, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	view := ui.NewTester(a.view, panelWidth, panelHeight)

	if err := view.Click("清空"); err != nil {
		t.Fatalf("tray clear: %v", err)
	}
	// 文件引用真的被删了，但剪贴板截图只是移出托盘。
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("original file deleted by tray clear: %v", err)
	}
	if _, err := os.Stat(managed); err != nil {
		t.Fatalf("managed png deleted by tray clear: %v", err)
	}
	for _, it := range s.Items {
		if it.Kind == store.KindFile {
			t.Fatal("tray clear kept the file item")
		}
	}
	if len(s.Items) != 1 || s.Items[0].ID != clip.ID {
		t.Fatalf("tray clear must keep the clipboard capture in the store: %+v", s.Items)
	}
	if !view.HasText("把文件拖到这里") {
		t.Fatal("tray still shows the hidden capture")
	}
	a.setTab(pageClipboard)
	view.Frame()
	if !view.HasText(itemTitle(clip)) {
		t.Fatal("hidden capture missing from the clipboard page")
	}
	// 剪贴板页的「清空」才是真正的删除。
	if err := view.Click("清空"); err != nil {
		t.Fatalf("clipboard clear: %v", err)
	}
	if len(s.Items) != 0 {
		t.Fatalf("clipboard clear left %d items", len(s.Items))
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatalf("managed png should be deleted by clipboard clear: %v", err)
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("original file deleted by clipboard clear: %v", err)
	}
}

// 托盘页右键「移除」剪贴板截图同样只是把它移出托盘：项目保留、PNG 保留、
// 剪贴板页仍可见；之后重新复制同一张图会把它带回托盘。
func TestTrayMenuRemoveHidesClipboardCapture(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	pngA := buf.Bytes()
	if err := s.AddClipImage(pngA, "com.example.Shot"); err != nil {
		t.Fatal(err)
	}
	clip := s.Items[0]
	managed := clip.Path
	a := &shelf{store: s, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	view := ui.NewTester(a.view, panelWidth, panelHeight)

	if err := view.RightClick(itemTitle(clip)); err != nil {
		t.Fatalf("right-click clip: %v", err)
	}
	if err := view.ChooseMenuItem("移除"); err != nil {
		t.Fatalf("choose 移除: %v (%v)", err, view.Menu())
	}
	if len(s.Items) != 1 || s.Items[0].ID != clip.ID {
		t.Fatalf("tray 移除 deleted the capture: %+v", s.Items)
	}
	if _, err := os.Stat(managed); err != nil {
		t.Fatalf("managed png deleted by tray 移除: %v", err)
	}
	if !view.HasText("把文件拖到这里") {
		t.Fatal("tray still shows the hidden capture")
	}
	a.setTab(pageClipboard)
	view.Frame()
	if !view.HasText(itemTitle(clip)) {
		t.Fatal("hidden capture missing from the clipboard page")
	}
	// 再次复制同一张图：去重命中，回到托盘最前。
	a.setTab(pageTray)
	if err := s.AddClipImage(pngA, "com.example.Shot"); err != nil {
		t.Fatal(err)
	}
	view.Frame()
	if len(s.Items) != 1 || !view.HasText(itemTitle(clip)) {
		t.Fatal("re-capture did not bring the item back to the tray")
	}
}

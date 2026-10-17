//go:build darwin

package main

import (
	"bytes"
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
	// 缩略图在途解码与缓存命中都必须在清空时失效。
	if len(a.thumbPending) == 0 {
		t.Fatal("expected in-flight thumbnail decodes for file and image items")
	}
	a.thumbnails["cached"] = &ui.Bitmap{}

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
	if len(a.thumbnails) != 0 {
		t.Fatal("cached thumbnails not invalidated by clear-all")
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

// 刘海屏 topInset>0 时顶部插入留白，把顶栏整体推到刘海下沿以下，
// 顶栏文字与按钮（收起/清空）仍然正常渲染且可点击。
func TestNotchTopInsetPushesHeaderBelowNotch(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AddText("有内容"); err != nil {
		t.Fatal(err)
	}
	a := &shelf{store: s, topInset: 32, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	for _, label := range []string{"托盘", "清空", "收起"} {
		r, ok := view.Find(label)
		if !ok {
			t.Fatalf("header element %q missing with topInset=32", label)
		}
		if r.Y < 32 {
			t.Fatalf("header element %q at y=%v overlaps the 32pt notch", label, r.Y)
		}
	}
	if err := view.Click("清空"); err != nil {
		t.Fatalf("clear-all button not clickable below notch: %v", err)
	}
	if len(s.Items) != 0 {
		t.Fatal("clear-all did not clear items")
	}
	if !view.HasText("把文件拖到这里") {
		t.Fatal("empty state not shown after clear-all below notch")
	}
	if err := view.Click("收起"); err != nil {
		t.Fatalf("collapse button not clickable below notch: %v", err)
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

// The first module whose panelCard reports "drawn" gets the tray tab's
// slot above the list; later cards and the clipboard tab are untouched.
func TestPanelCardCallsFirstDrawnModule(t *testing.T) {
	first := &fakeCard{label: "第一个卡片", drawn: false}
	second := &fakeCard{label: "第二个卡片", drawn: true}
	third := &fakeCard{label: "第三个卡片", drawn: true}
	a := &shelf{tab: tabTray}
	a.modules = []module{first, second, third}

	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if view.HasText("第一个卡片") || view.HasText("第三个卡片") {
		t.Fatal("a card that reports not-drawn must not show, and later ones must not be asked")
	}
	if !view.HasText("第二个卡片") {
		t.Fatal("the first card that draws must show above the list")
	}
	if !first.asked || !second.asked || third.asked {
		t.Fatalf("cards must be asked in order until one draws: %v %v %v", first.asked, second.asked, third.asked)
	}
	// The clipboard tab keeps its list without any card.
	second.asked = false
	a.tab = tabClipboard
	view = ui.NewTester(a.view, panelWidth, panelHeight)
	if second.asked || view.HasText("第二个卡片") {
		t.Fatal("the clipboard tab must not draw panel cards")
	}
}

type fakeCard struct {
	label string
	drawn bool
	asked bool
}

func (c *fakeCard) tick(time.Time) {}

func (c *fakeCard) card(view *ui.Context) bool {
	c.asked = true
	if !c.drawn {
		return false
	}
	ui.Text(view, c.label).FontSize(12).TextColor(textPrimary)
	return true
}

//go:build darwin

package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	for _, it := range s.Items {
		label := "移除 " + itemTitle(it)
		if !view.HasText(label) {
			t.Fatalf("per-item remove button %q missing", label)
		}
	}
	// 缩略图在途解码与缓存命中都必须在清空时失效。
	if len(a.thumbPending) == 0 {
		t.Fatal("expected in-flight thumbnail decodes for file and image items")
	}
	a.thumbnails["cached"] = &ui.Bitmap{}

	if err := view.Click("移除 note.txt"); err != nil {
		t.Fatalf("click per-item remove: %v", err)
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
	for _, label := range []string{"Shelf", "清空托盘", "收起托盘"} {
		r, ok := view.Find(label)
		if !ok {
			t.Fatalf("header element %q missing with topInset=32", label)
		}
		if r.Y < 32 {
			t.Fatalf("header element %q at y=%v overlaps the 32pt notch", label, r.Y)
		}
	}
	if err := view.Click("清空托盘"); err != nil {
		t.Fatalf("clear-all button not clickable below notch: %v", err)
	}
	if len(s.Items) != 0 {
		t.Fatal("clear-all did not clear items")
	}
	if !view.HasText("把文件拖到这里") {
		t.Fatal("empty state not shown after clear-all below notch")
	}
	if err := view.Click("收起托盘"); err != nil {
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
	if !view.HasText("点击展开 Shelf") {
		t.Fatal("peek pill missing its expand hint")
	}
	if view.HasText("收起托盘") {
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
	if !view.HasText("收起托盘") {
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

// 拖拽会话一旦激活，常驻窗口必须立刻无条件接收鼠标事件：窗口缩成刘海顶边
// 窄条（Y=0），若等光标进入触发区再取消穿透，轮询间隙足以让 Mission Control
// 抢走顶部拖拽。无窗口环境下只验证 ignoringMouse 标记。
func TestDragSessionDisablesPassThrough(t *testing.T) {
	a := &shelf{dragging: true, ignoringMouse: true}
	a.syncMousePassThrough()
	if a.ignoringMouse {
		t.Fatal("active drag must clear ignoringMouse unconditionally")
	}
	// 探头态在无窗口环境下也接收；空闲收起态恢复穿透。
	b := &shelf{peeking: true, ignoringMouse: true}
	b.syncMousePassThrough()
	if b.ignoringMouse {
		t.Fatal("peeking shelf must accept mouse events so the pill is clickable")
	}
	c := &shelf{}
	c.syncMousePassThrough()
	if !c.ignoringMouse {
		t.Fatal("idle collapsed shelf must stay mouse-transparent")
	}
}

// 常驻窗口的映射帧必须跟随三态：展开=完整面板，拖拽/探头=刘海顶边窄条，
// 空闲=刘海壳大小。拖拽时只有窄条拦截事件，其余屏幕留给下层应用；
// 刘海几何无效时窄条退回面板/显示器顶边中心而不是 (0,0)。
func TestWindowFrameTracksState(t *testing.T) {
	a := &shelf{
		x: 470, y: 0, h: 380,
		notchX: 600, notchY: 0, notchW: 180, notchH: 32,
	}
	if r := a.desiredFrame(); r != (mygo.Rectangle{X: 600, Y: 0, Width: 180, Height: 32}) {
		t.Fatalf("idle frame should collapse to the notch housing, got %+v", r)
	}
	a.dragging = true
	wantStrip := mygo.Rectangle{X: 550, Y: 0, Width: 280, Height: 72}
	if r := a.desiredFrame(); r != wantStrip {
		t.Fatalf("drag frame should be the top-center strip %+v, got %+v", wantStrip, r)
	}
	a.dragging, a.peeking = false, true
	if r := a.desiredFrame(); r != wantStrip {
		t.Fatalf("peek frame should match the drag strip %+v, got %+v", wantStrip, r)
	}
	a.visible = true
	if r := a.desiredFrame(); r != (mygo.Rectangle{X: 470, Y: 0, Width: 440, Height: 380}) {
		t.Fatalf("expanded frame should be the full panel, got %+v", r)
	}
	// 无窗口状态也要安全地走一遍帧同步路径（a.frame 未被应用）。
	a.syncWindowFrame()
	if a.frame != (mygo.Rectangle{}) {
		t.Fatal("frame must not be tracked before a window exists")
	}

	// 刘海几何无效：窄条居中于面板（即显示器）顶边中心，而非 (0,0)。
	b := &shelf{x: 470, dragging: true}
	want := mygo.Rectangle{X: 550, Y: 0, Width: 280, Height: 72}
	if r := b.desiredFrame(); r != want {
		t.Fatalf("no-notch drag frame should center on the display top, got %+v", r)
	}
}

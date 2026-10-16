//go:build darwin

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/store"
)

// UI、历史与本机 API 由 mygo 主线程串行拥有。
type shelf struct {
	win                                       *mygo.Window
	tray                                      *mygo.Tray
	store                                     *store.Store
	stop                                      chan struct{}
	quitting, visible, edgeOpened, received   bool
	status                                    string
	statusUntil                               time.Time
	x, y                                      int
	dispBounds                                mygo.Rectangle
	frame                                     mygo.Rectangle // 常驻窗口当前实际映射的屏幕矩形（desiredFrame 最近一次应用值）
	settingFrame                              bool
	animationStart                            time.Time
	animationFrom, progress                   float32
	clipboardCount                            int64
	lastClipboard                             time.Time
	dragSerial                                int64
	dragging, suppressed, pointerDrag         bool
	guard                                     dragGuard
	escHeld                                   bool
	stopMouseObservation, stopDragObservation func()
	leftSince                                 time.Time
	h, topInset                               int
	notchX, notchY, notchW, notchH            int
	hasNotch                                  bool
	peeking                                   bool
	peekProgress, peekFrom                    float32
	peekAnimStart                             time.Time
	ignoringMouse                             bool
	thumbnails                                map[string]*ui.Bitmap
	thumbPending                              map[string]bool
	thumbDiscard                              map[string]bool
	thumbActive                               int
}

// dragGuard owns the current filter, including a failed setup attempt so that
// the same error is not retried on every monitoring tick. A failure while
// untrusted is retried once Accessibility is granted, without a restart.
type dragGuard struct {
	stop      func()
	region    mac.DragGuardRegion
	attempted bool
	trusted   bool // Accessibility state at the last attempt
}

func (a *shelf) toggle() {
	if a.visible {
		a.hide()
	} else {
		a.show(false)
	}
}

func (a *shelf) place(p mygo.Point) {
	d := mygo.Screen.DisplayNearestPoint(p)
	a.dispBounds = d.Bounds
	nx, ny, nw, nh, hasNotch := mac.NotchRect(d.ID)
	a.notchX, a.notchY, a.notchW, a.notchH, a.hasNotch = nx, ny, nw, nh, hasNotch
	a.topInset = nh
	a.h = min(panelHeight, max(240, d.WorkArea.Height-24))
	a.x = d.Bounds.X + (d.Bounds.Width-panelWidth)/2
	a.y = d.Bounds.Y
	if a.win != nil {
		a.syncWindowFrame()
		a.win.SetOpacity(1)
	}
	a.syncDragGuard()
}

// overNotch 只认实测刘海几何：光标真正进入刘海热区（notchX/notchY 为原点，
// 四周留 hover 余量）才为真。显示器未匹配时 notchW 为 0，刘海交互整体禁用，
// 不会在屏幕左上角凭空出现一个悬停/触发区。
func (a *shelf) overNotch(p mygo.Point) bool {
	return a.notchW > 0 && a.notchH > 0 &&
		p.X >= a.notchX-notchHoverPadX && p.X <= a.notchX+a.notchW+notchHoverPadX &&
		p.Y >= a.notchY && p.Y <= a.notchY+a.notchH+notchHoverPadY
}

// nearTrigger 是拖拽唤出托盘的顶部触发区（刘海中心 ± notchTriggerWidth/2、
// 顶部 notchTriggerHeight 高），同样要求刘海几何有效。
func (a *shelf) nearTrigger(p mygo.Point) bool {
	if a.notchW <= 0 {
		return false
	}
	centerX := a.notchX + a.notchW/2
	return p.X >= centerX-notchTriggerWidth/2 && p.X <= centerX+notchTriggerWidth/2 &&
		p.Y >= a.notchY && p.Y <= a.notchY+notchTriggerHeight
}

// Hover changes only the drawing, not the native window geometry. Keeping one
// stable transparent strip for idle/peek avoids resizing the backing surface
// every time the pointer crosses the notch. Only opening the panel changes it.
func (a *shelf) desiredFrame() mygo.Rectangle {
	if a.visible || a.progress > 0.01 {
		return mygo.Rectangle{X: a.x, Y: a.y, Width: panelWidth, Height: a.panelH()}
	}
	w := max(max(180, a.notchW)+2*peekStripPadX, notchTriggerWidth)
	h := max(max(32, a.notchH)+peekStripPadH, notchTriggerHeight)
	return mygo.Rectangle{X: a.stripCenter() - w/2, Y: a.y, Width: w, Height: h}
}

// Use a single native setFrame plus a top-left pin so AppKit cannot leave
// the overlay a menu-bar-height below the hardware notch.
func (a *shelf) syncWindowFrame() {
	if a.win == nil || a.settingFrame {
		return
	}
	r := a.desiredFrame()
	if r == a.frame {
		return
	}
	a.settingFrame = true
	defer func() { a.settingFrame = false }()
	mac.SetWindowFrame(a.win.NativeHandle(), r.X, r.Y, r.Width, r.Height)
	a.frame = r
	a.win.Invalidate()
}

// Native window movement can invalidate our cached frame independently of a
// state transition. Never let the cached target hide a displaced window.
func (a *shelf) windowGeometryChanged() {
	if a.settingFrame || a.win == nil {
		return
	}
	if actual := a.win.Bounds(); actual == a.desiredFrame() {
		a.frame = actual
		return
	}
	a.frame = mygo.Rectangle{}
	a.syncWindowFrame()
}

// peekPillRect is the on-screen box of the hovered notch capsule, matching
// the shell drawn in view(). Clicks on the "点击展开 Shelf" hint land at
// the bottom of this rect, below the hardware notch.
func (a *shelf) peekPillRect() mygo.Rectangle {
	w := max(180, a.notchW) + 56
	h := max(32, a.notchH) + 18
	return mygo.Rectangle{X: a.stripCenter() - w/2, Y: a.y, Width: w, Height: h}
}

func (a *shelf) overPeekPill(p mygo.Point) bool {
	r := a.peekPillRect()
	return r.Width > 0 && r.Height > 0 &&
		p.X >= r.X && p.X < r.X+r.Width &&
		p.Y >= r.Y && p.Y < r.Y+r.Height
}

// mouseDown is the AppKit mouse-down observer. inside means the event was
// delivered to our window; otherwise it went to another app or the menu bar.
func (a *shelf) mouseDown(inside bool, p mygo.Point) {
	if a.quitting {
		return
	}
	// The capsule is drawn over the menu bar's empty centre. WindowServer
	// often gives that press to the menu bar or the app underneath, so the
	// overlay paints a "click to expand" pill that never receives the click.
	// A press on the visible pill expands either way.
	if a.peeking && !a.visible && (inside || a.overPeekPill(p)) {
		a.show(false)
		return
	}
	if inside {
		a.holdEscape()
		return
	}
	a.releaseEscape()
	inMenuBar := p.Y >= a.notchY && p.Y <= a.notchY+max(36, a.notchH+4)
	if a.visible && !a.dragging && !inMenuBar {
		a.hide()
	}
}

// Idle clicks below the hardware notch pass through; the notch band itself
// never does. File drags to the top of the notch must hit a window, or
// WindowServer treats Y=0 as the Spaces edge. ignoresMouseEvents stays off
// so the overlay stays in the hit-test list for the whole drag.
func (a *shelf) syncMousePassThrough() {
	a.syncWindowFrame()
	acceptAll := a.visible || a.progress > 0.01 || a.dragging || a.pointerDrag || a.peeking || a.peekProgress > 0.01
	a.ignoringMouse = !acceptAll
	if a.win == nil {
		return
	}
	h := a.win.NativeHandle()
	mac.SetOverlayHitTest(h, acceptAll, max(8, a.notchH))
	mac.SetDragElevated(h, a.dragging || a.pointerDrag)
}

// setPeek animates the notch shell between its idle hardware shape and the
// peeked "I'm here" pill, and updates event pass-through to match.
func (a *shelf) setPeek(peek bool) {
	if a.peeking == peek {
		return
	}
	a.peeking = peek
	a.peekFrom, a.peekAnimStart = a.peekProgress, time.Now()
	a.syncMousePassThrough()
	if a.win != nil {
		a.win.Invalidate()
	}
}

// panelH returns the height place() computed, falling back to panelHeight
// before the first placement (e.g. in view tests with no real window).
func (a *shelf) panelH() int {
	if a.h > 0 {
		return a.h
	}
	return panelHeight
}

func (a *shelf) show(edge bool) {
	if a.visible {
		return
	}
	if a.win != nil {
		a.place(mygo.Screen.CursorScreenPoint())
	}
	if a.store != nil {
		a.store.Refresh()
	}
	a.visible, a.edgeOpened, a.received = true, edge, false
	a.animationFrom, a.animationStart = a.progress, time.Now()
	a.syncMousePassThrough()
	if a.win != nil {
		a.win.ShowInactive()
		// ShowInactive 不夺焦，面板收不到按键，只能临时注册全局 Esc。
		a.holdEscape()
		a.win.Invalidate()
	}
}

func (a *shelf) holdEscape() {
	if a.escHeld {
		return
	}
	if err := mygo.GlobalShortcut.Register("Escape", a.hide); err != nil {
		a.report(fmt.Errorf("Esc 被其他应用占用，请用快捷键或收起按钮：%w", err))
		return
	}
	a.escHeld = true
}

func (a *shelf) releaseEscape() {
	if a.escHeld {
		mygo.GlobalShortcut.Unregister("Escape")
		a.escHeld = false
	}
}

func (a *shelf) hide() {
	if !a.visible && !a.peeking {
		return
	}
	a.visible, a.edgeOpened, a.peeking = false, false, false
	a.animationFrom, a.animationStart = a.progress, time.Now()
	a.peekFrom, a.peekAnimStart = a.peekProgress, time.Now()
	a.releaseEscape()
	// 当前拖拽不再次触发；松开鼠标或出现新会话后解除。
	a.suppressed = true
	a.syncMousePassThrough()
	if a.win != nil {
		a.win.Invalidate()
	}
}

// animate updates the shell drawing; idle/peek retain one native surface.
func (a *shelf) animate(now time.Time) {
	finished := false
	if !a.animationStart.IsZero() {
		k := min(float32(now.Sub(a.animationStart))/float32(panelAnimation), 1)
		target := float32(0)
		if a.visible {
			target = 1
		}
		a.progress = a.animationFrom + (target-a.animationFrom)*ui.EaseOut(k)
		if k >= 1 {
			a.animationStart = time.Time{}
			finished = true
		}
	}
	if !a.peekAnimStart.IsZero() {
		k := min(float32(now.Sub(a.peekAnimStart))/float32(140*time.Millisecond), 1)
		target := float32(0)
		if a.peeking || a.visible {
			target = 1
		}
		a.peekProgress = a.peekFrom + (target-a.peekFrom)*ui.EaseOut(k)
		if k >= 1 {
			a.peekAnimStart = time.Time{}
			finished = true
		}
	}
	if a.win == nil {
		return
	}
	// 动画途中逐帧重绘；结束时补一次穿透状态同步与终帧。
	if !a.animationStart.IsZero() || !a.peekAnimStart.IsZero() || finished {
		if finished {
			a.syncMousePassThrough()
		}
		a.win.Invalidate()
	}
}

func (a *shelf) startMonitoring() {
	go func() {
		ticker := time.NewTicker(pointerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.stop:
				return
			case now := <-ticker.C:
				mygo.RunOnMain(func() {
					if a.quitting {
						return
					}
					a.animate(now)
					a.monitorDrag(now)
					if now.Sub(a.lastClipboard) >= clipboardInterval {
						a.lastClipboard = now
						a.monitorClipboard()
					}
					if !a.statusUntil.IsZero() && now.After(a.statusUntil) {
						a.status, a.statusUntil = "", time.Time{}
						if a.win != nil {
							a.win.Invalidate()
						}
					}
				})
			}
		}
	}()
}

func (a *shelf) monitorClipboard() {
	count := mac.ClipboardChangeCount()
	if count == a.clipboardCount {
		return
	}
	a.clipboardCount = count
	if a.store == nil {
		return
	}
	// 每次变化只收一条：按来源声明的首选表示取文字或图片，避免双条目。
	switch classifyClipboard(mac.ClipboardTypes()) {
	case clipText:
		if text := mygo.Clipboard.ReadText(); text != "" {
			a.changed(a.store.AddText(text))
		}
	case clipImage:
		if png := mygo.Clipboard.ReadImage(); len(png) > 0 {
			a.changed(a.store.AddImage(png))
		}
	}
}

// stripCenter is shared by the drop surface and its protected approach region.
func (a *shelf) stripCenter() int {
	if a.notchW <= 0 {
		return a.x + panelWidth/2
	}
	return a.notchX + a.notchW/2
}

// dragGuardRegion is the notch corridor the event filter clamps, in the
// same coordinates as the resident drop strip.
func (a *shelf) dragGuardRegion() mac.DragGuardRegion {
	strip := a.desiredFrame()
	// Land below the menu bar's exclusive strip, still inside the drop
	// surface. A 40pt corridor on a 38pt notch sits inside the menu bar
	// and Spaces still fires.
	h := min(max(a.notchH+16, 48), strip.Height-1)
	if h < 8 {
		h = 8
	}
	return mac.DragGuardRegion{
		X: a.stripCenter() - notchTriggerWidth/2, Y: a.y,
		Width: notchTriggerWidth, Height: h,
	}
}

// Protect the notch's approach corridor, including its centre. The lower edge
// is below the camera housing but still inside the shelf's drag trigger.
// The event filter (Accessibility only) is armed once and stays up for file
// drags and window moves alike; the cursor pin needs no permission and runs
// only while a file drag is in progress.
func (a *shelf) syncDragGuard() {
	if a.win == nil {
		a.stopDragGuard()
		return
	}
	region := a.dragGuardRegion()
	mac.SetDragPin(region, a.dragging && !a.quitting)
	if a.guard.stop != nil {
		if a.guard.region != region {
			mac.SetDragGuardRegion(region)
			a.guard.region = region
		}
		return
	}
	if a.guard.attempted && (a.guard.trusted || !mac.AccessibilityTrusted()) {
		return
	}
	stop, err := mac.StartDragGuard(region)
	a.guard = dragGuard{stop: stop, region: region, attempted: true, trusted: mac.AccessibilityTrusted()}
	if err != nil {
		log.Printf("刘海拖拽保护未启用：%v", err)
	}
}

func (a *shelf) stopDragGuard() {
	mac.SetDragPin(mac.DragGuardRegion{}, false)
	if a.guard.stop != nil {
		a.guard.stop()
	}
	a.guard = dragGuard{}
}

// pointerDragged is the NSEvent mouse-dragged callback. Finder has already
// declared the drag pasteboard by the first pixel of movement; waiting for
// the 30 ms poll lets the cursor reach Y=0 first.
func (a *shelf) pointerDragged() {
	if a.quitting {
		return
	}
	active, serial := mac.DragState()
	if serial != a.dragSerial {
		a.dragSerial = serial
		a.suppressed = false
	}
	if !active {
		return
	}
	a.dragging, a.pointerDrag = true, true
	p := mygo.Screen.CursorScreenPoint()
	if !a.visible && !insideRect(p, a.dispBounds) {
		a.place(p)
	}
	if !a.suppressed {
		a.setPeek(true)
	}
	a.syncDragGuard()
	a.syncMousePassThrough()
	if !a.suppressed && !a.visible && a.nearTrigger(p) {
		a.show(true)
	}
}

// monitorDrag updates placement, drag protection and interaction on the main thread.
func (a *shelf) monitorDrag(now time.Time) {
	active, serial := mac.DragState()
	if serial != a.dragSerial {
		a.dragSerial = serial
		a.suppressed = false
	}
	a.dragging = active
	if !active {
		a.suppressed = false
		if !mac.LeftMouseDown() {
			a.pointerDrag = false
		}
	}
	p := mygo.Screen.CursorScreenPoint()
	if !a.visible && !insideRect(p, a.dispBounds) {
		a.place(p)
	}
	overNotch := a.overNotch(p)
	nearEdge := a.nearTrigger(p)
	// Dragging or hovering reveals the pill; only a click or a real incoming
	// drag in the trigger zone can open the full panel.
	if active || overNotch {
		if !a.suppressed || overNotch {
			a.setPeek(true)
		}
	}
	a.syncDragGuard()
	a.syncMousePassThrough()
	if active && !a.suppressed && !a.visible && nearEdge {
		// Incoming drags open immediately; empty-handed hover never does.
		a.show(true)
	}
	if !active && !a.visible && !overNotch {
		a.setPeek(false)
	}
	// 自动收起：仅拖拽唤出的托盘在未收到内容、光标离开面板与触发区
	// edgeLeaveDelay 后缩回刘海；点击探头/托盘唤出的托盘由用户显式收起。
	// 动画途中不判离开。
	if a.visible && a.edgeOpened && !a.received {
		inside := p.X >= a.x-24 && p.X < a.x+panelWidth+24 && p.Y >= a.y && p.Y < a.y+a.panelH()+24
		if !inside && !nearEdge && a.animationStart.IsZero() {
			if a.leftSince.IsZero() {
				a.leftSince = now
			}
			if now.Sub(a.leftSince) >= edgeLeaveDelay {
				a.hide()
			}
		} else {
			a.leftSince = time.Time{}
		}
	} else {
		a.leftSince = time.Time{}
	}
}

func insideRect(p mygo.Point, r mygo.Rectangle) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
}

func (a *shelf) receive(data transfer.Data) {
	if a.store == nil {
		return
	}
	var err error
	if paths, e := data.Files(); e == nil && len(paths) > 0 {
		err = a.store.AddFiles(paths)
	} else if b, e := data.Read(transfer.PNG); e == nil && len(b) > 0 {
		err = a.store.AddImage(b)
	} else if b, e := data.Read(transfer.Text); e == nil && len(b) > 0 {
		err = a.store.AddText(string(b))
	} else {
		a.report(fmt.Errorf("此内容不能放入托盘；请拖入本地文件、文字或图片"))
		return
	}
	if err == nil {
		a.received = true
		a.status = "已放入 · 拖出后仍会保留"
		a.statusUntil = time.Now().Add(3 * time.Second)
	}
	a.changed(err)
}

func (a *shelf) changed(err error) {
	if err != nil {
		a.report(err)
		return
	}
	if a.store != nil && len(a.store.Items) > 0 {
		a.invalidateThumbnail(a.store.Items[0].ID)
	}
	if a.win != nil {
		a.win.Invalidate()
	}
}
func (a *shelf) report(err error) {
	a.status = "操作失败：" + err.Error()
	a.statusUntil = time.Time{}
	log.Print(a.status)
	if a.win != nil {
		a.win.Invalidate()
	}
}
func (a *shelf) completed(r transfer.Result) {
	if r.Err != nil {
		a.report(r.Err)
		return
	}
	if !r.Canceled && r.Operation != transfer.None {
		a.hide()
	}
}

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
	win                                     *mygo.Window
	tray                                    *mygo.Tray
	store                                   *store.Store
	stop                                    chan struct{}
	quitting, visible, edgeOpened, received bool
	status                                  string
	statusUntil                             time.Time
	x, y                                    int
	dispBounds                              mygo.Rectangle
	frame                                   mygo.Rectangle // 常驻窗口当前实际映射的屏幕矩形（desiredFrame 最近一次应用值）
	animationStart                          time.Time
	animationFrom, progress                 float32
	clipboardCount                          int64
	lastClipboard                           time.Time
	dragSerial                              int64
	dragging, suppressed                    bool
	escHeld                                 bool
	stopMouseObservation                    func()
	edgeSince, leftSince                    time.Time
	h, topInset                             int
	notchX, notchY, notchW, notchH          int
	hasNotch                                bool
	peeking                                 bool
	peekProgress, peekFrom                  float32
	peekAnimStart                           time.Time
	ignoringMouse                           bool
	thumbnails                              map[string]*ui.Bitmap
	thumbPending                            map[string]bool
	thumbDiscard                            map[string]bool
	thumbActive                             int
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

// desiredFrame returns the screen rect the resident window should occupy for
// the current state, in MyGo top-left screen coordinates:
//
//   - expanded (visible or mid open/close animation): the full panel rect;
//   - peeking or mid-drag: a narrow strip flush with the screen's top edge
//     centered on the notch — wide and tall enough to cover the peeked pill,
//     the drag trigger zone and a margin around the hardware housing;
//   - fully idle: just the hardware housing rect (1×1 when no notch
//     geometry was measured), so the invisible window occupies as little
//     screen as possible.
//
// Shrinking the mapped frame while peeking/dragging is what keeps Mission
// Control away without stealing drops: the WindowServer sees a valid drop
// target at the top edge around the notch, while the rest of the old
// 440×panelHeight frame passes every event through to the apps underneath.
func (a *shelf) desiredFrame() mygo.Rectangle {
	if a.visible || a.progress > 0.01 {
		return mygo.Rectangle{X: a.x, Y: a.y, Width: panelWidth, Height: a.panelH()}
	}
	cx := a.notchX + a.notchW/2
	if a.notchW <= 0 {
		// 刘海几何无效时退回面板/显示器顶边中心，不会在屏幕左上角造假区域。
		cx = a.x + panelWidth/2
	}
	w, h := 1, 1
	if a.peeking || a.dragging || a.peekProgress > 0.01 {
		w = max(max(180, a.notchW)+2*peekStripPadX, notchTriggerWidth)
		h = max(max(32, a.notchH)+peekStripPadH, notchTriggerHeight)
	} else if a.notchW > 0 {
		w, h = a.notchW, a.notchH
	}
	return mygo.Rectangle{X: cx - w/2, Y: a.y, Width: w, Height: h}
}

// syncWindowFrame applies desiredFrame() to the resident window when the
// mapped rect changed, via a single setFrame:display: so the window never
// transiently keeps its old width at the new position (or vice versa).
func (a *shelf) syncWindowFrame() {
	if a.win == nil {
		return
	}
	r := a.desiredFrame()
	if r == a.frame {
		return
	}
	a.frame = r
	mac.SetWindowFrame(a.win.NativeHandle(), r.X, r.Y, r.Width, r.Height)
	a.win.Invalidate()
}

// syncMousePassThrough decides whether the resident transparent window
// intercepts mouse events, and keeps its mapped frame at desiredFrame().
// While collapsed and idle the window shrinks to the hardware housing and
// ignores the mouse so clicks and drags pass straight through to the menu
// bar and apps beneath; while visible, peeking, mid-animation or during a
// system drag it accepts events so the notch can be clicked, hovered and
// dropped onto — but its frame is only the top-center strip then, not the
// full panel area. During a drag the window accepts unconditionally: the
// strip is already flush with the top edge (Y=0), and waiting for the
// cursor to enter the trigger zone before flipping ignoresMouseEvents
// leaves a poll-interval window where the WindowServer fires the Mission
// Control / Spaces strip.
func (a *shelf) syncMousePassThrough() {
	a.syncWindowFrame()
	var accept bool
	switch {
	case a.visible || a.progress > 0.01:
		accept = true
	case a.dragging:
		// 拖拽期间无条件接收：窗口常驻顶边，先成为合法拖放目标，
		// macOS 才不会在光标触到 Y=0 时弹出 Spaces 栏抢走拖拽。
		accept = true
	case a.peeking || a.peekProgress > 0.01:
		if a.win != nil && a.notchW > 0 {
			// 只有光标落在实测刘海附近时才让常驻窗口接管事件；刘海几何
			// 无效（notchW == 0，显示器未匹配）时保持穿透。
			p := mygo.Screen.CursorScreenPoint()
			accept = p.X >= a.notchX-notchHoverPadX-28 && p.X <= a.notchX+a.notchW+notchHoverPadX+28 &&
				p.Y >= a.notchY && p.Y <= a.notchY+a.notchH+notchHoverPadY+24
		} else if a.win == nil {
			accept = true
		}
	}
	ignore := !accept
	if a.ignoringMouse == ignore {
		return
	}
	a.ignoringMouse = ignore
	if a.win != nil {
		mac.SetWindowIgnoresMouseEvents(a.win.NativeHandle(), ignore)
	}
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

// animate 推进展开（progress）与探头（peekProgress）两组动画。窗口常驻不
// 隐藏：收起态完全透明且忽略鼠标。拖拽开始时常驻窗口马上缩成刘海顶边
// 窄条并接管事件（见 syncWindowFrame），不让 Mission Control 抢走拖到
// 屏幕顶部的会话，也不会继续挡住面板覆盖区下方的应用。
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

func (a *shelf) monitorDrag(now time.Time) {
	active, serial := mac.DragState()
	if serial != a.dragSerial {
		a.dragSerial = serial
		a.suppressed = false
	}
	a.dragging = active
	if !active {
		a.suppressed = false
		a.edgeSince = time.Time{}
	}
	p := mygo.Screen.CursorScreenPoint()
	overNotch := a.overNotch(p)
	nearEdge := a.nearTrigger(p)
	// 拖拽开始（任意位置）或空手光标探入刘海区：立刻探头并切换为接收
	// 鼠标事件——这一步在光标到达屏幕顶边之前完成，Mission Control 因此
	// 不会在顶部拖拽时被触发。拖拽中收起态窗口跟着光标换屏。
	if active || overNotch {
		if !a.visible && !insideRect(p, a.dispBounds) {
			a.place(p)
		}
		if !a.suppressed || overNotch {
			a.setPeek(true)
		}
	}
	a.syncMousePassThrough()
	if active && !a.suppressed && !a.visible && nearEdge {
		if a.edgeSince.IsZero() {
			a.edgeSince = now
		}
		if now.Sub(a.edgeSince) >= edgeDwell {
			a.show(true)
			a.edgeSince = time.Time{}
		}
	} else {
		a.edgeSince = time.Time{}
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

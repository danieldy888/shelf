//go:build darwin

package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
)

// The notch shell: geometry, the three states (idle, peek, expanded), their
// animations and mouse pass-through. Content lives in panel.go and items.go.

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
// The strip is always wide enough for the island's wings, so live activity
// changes the drawing (and the hit-test band) without a native resize.
func (a *shelf) desiredFrame() mygo.Rectangle {
	if a.visible || a.progress > 0.01 {
		return mygo.Rectangle{X: a.x, Y: a.y, Width: panelWidth, Height: a.panelH()}
	}
	w := max(max(max(180, a.notchW)+2*peekStripPadX, notchTriggerWidth), a.islandStripW())
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
	// The island's wings sit in the menu bar's centre too, and are only
	// hit-testable while they are drawn: the same rule expands the shelf.
	if a.islandShown() && !a.visible && !a.peeking && (inside || a.overIsland(p)) {
		a.show(false)
		return
	}
	if inside {
		// Esc and Space are taken only while the panel is open; a press
		// on the collapsed notch band or a closing panel must not hold them.
		if a.visible {
			a.holdKeys()
		}
		a.takeServiceOwnership()
		return
	}
	a.releaseKeys()
	inMenuBar := p.Y >= a.notchY && p.Y <= a.notchY+max(36, a.notchH+4)
	if a.visible && !a.dragging && !inMenuBar {
		a.hide(hidePassive)
	}
}

// windowLevel is the overlay's one window-level policy, applied by
// syncMousePassThrough on every state change: a file drag sits above the
// menu bar so its strip cannot steal the drop; keyboard focus (clipboard
// search) must drop below kCGDockWindowLevel or the input method's
// candidate panel — drawn at 20 and anchored inside the opaque panel —
// stays completely hidden behind it (marked text still composes inline,
// which is why the bug reads as "IME types but no candidates").
func (a *shelf) windowLevel() int {
	switch {
	case a.dragging || a.pointerDrag:
		return mac.PopUpMenuWindowLevel
	case a.focusMode:
		return mac.SearchKeyboardLevel
	default:
		return mac.StatusWindowLevel
	}
}

// Idle clicks and menu-bar items outside the drawn shell pass through; the
// drawn shell — the notch band, widened to the island's wings while an
// activity shows — receives events, and a drag in flight accepts the whole
// strip. File drags to the top of the notch must hit a window, or
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
	bandW, bandH := a.idleBand()
	mac.SetOverlayHitTest(h, acceptAll, bandW, bandH)
	mac.SetWindowLevel(h, a.windowLevel())
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
		// ShowInactive 不夺焦，面板收不到按键，只能临时注册全局 Esc 与空格。
		a.holdKeys()
		a.win.Invalidate()
	}
}

// holdKeys takes Esc (close Quick Look, then collapse) and Space (Quick
// Look the selection) while the shelf is in use; releaseKeys returns them
// as soon as the user clicks in another app.
func (a *shelf) holdKeys() {
	if a.focusMode {
		// 键盘模式的窗口自己处理 Esc 与空格：全局注册会在按键到达窗口
		// 之前消费掉它们（空格就打不进搜索框），所以这里不占用。
		a.releaseKeys()
		return
	}
	if !a.escHeld {
		if err := mygo.GlobalShortcut.Register("Escape", a.escape); err != nil {
			a.report(fmt.Errorf("Esc 被其他应用占用，请用快捷键或收起按钮：%w", err))
		} else {
			a.escHeld = true
		}
	}
	if !a.spaceHeld {
		// Without Space, Quick Look stays available from the context menu.
		a.spaceHeld = mygo.GlobalShortcut.Register("Space", a.toggleQuickLook) == nil
	}
}

func (a *shelf) releaseKeys() {
	if a.escHeld {
		mygo.GlobalShortcut.Unregister("Escape")
		a.escHeld = false
	}
	if a.spaceHeld {
		mygo.GlobalShortcut.Unregister("Space")
		a.spaceHeld = false
	}
}

// escape closes Quick Look first, like Finder, then clears the clipboard
// search while it holds text, and finally collapses the shelf.
func (a *shelf) escape() {
	if mac.QuickLookVisible() {
		mac.CloseQuickLook()
		return
	}
	if a.tab == tabClipboard && a.search != "" {
		a.search = ""
		if a.win != nil {
			a.win.Invalidate()
		}
		return
	}
	a.hide(hideExplicit)
}

// hide collapses the shelf. exit decides whether the remembered app gets
// the keyboard back (see shouldRestoreApp); the remembered pid is always
// cleared so a later explicit hide cannot reactivate it a second time.
func (a *shelf) hide(exit hideExit) {
	if !a.visible && !a.peeking {
		return
	}
	// Any hide — manual or the flash's own timer — ends service ownership
	// so a queued timer can never reclaim a panel the user reopened.
	a.takeServiceOwnership()
	restore := a.shouldRestoreApp(exit)
	prev := a.prevAppPID
	a.visible, a.edgeOpened, a.peeking = false, false, false
	a.animationFrom, a.animationStart = a.progress, time.Now()
	a.peekFrom, a.peekAnimStart = a.peekProgress, time.Now()
	// 收起后回到默认视图：下次打开（悬停、拖入、托盘）从托盘标签开始，
	// 搜索框清空；键盘模式在用户明确的收起动作下交还焦点。
	a.focusMode, a.tab, a.search, a.reveal = false, tabTray, "", ""
	a.prevAppPID = 0
	a.releaseKeys()
	a.selection.clear()
	// 当前拖拽不再次触发；松开鼠标或出现新会话后解除。
	a.suppressed = true
	a.syncMousePassThrough()
	if restore {
		a.activatePreviousApp(prev)
	}
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
	if !a.islandStart.IsZero() {
		k := min(float32(now.Sub(a.islandStart))/float32(islandAnimation), 1)
		target := float32(0)
		if a.hasIsland() && !a.islandSwap {
			target = 1
		}
		a.islandK = a.islandFrom + (target-a.islandFrom)*ui.EaseOut(k)
		if k >= 1 {
			a.islandStart = time.Time{}
			finished = true
			if a.islandSwap {
				// The fold finished: swap in the new activity and grow the
				// wings back out, or stay folded if it ended altogether.
				a.island, a.islandSwap = a.islandNext, false
				a.islandNext = activity{}
				if a.hasIsland() {
					a.islandFrom, a.islandStart = a.islandK, now
					finished = false
				} else {
					a.islandK = 0
				}
			} else {
				a.islandK = target
			}
		}
	}
	if a.win == nil {
		return
	}
	// 动画途中逐帧重绘；结束时补一次穿透状态同步与终帧。
	if !a.animationStart.IsZero() || !a.peekAnimStart.IsZero() || !a.islandStart.IsZero() || finished {
		if finished {
			a.syncMousePassThrough()
		} else if !a.islandStart.IsZero() {
			// The wings change the drawn shell's width frame by frame;
			// move the hit-test band with them in one atomic store.
			bandW, bandH := a.idleBand()
			mac.SetOverlayBand(bandW, bandH)
		}
		a.win.Invalidate()
	}
}

// stripCenter is shared by the drop surface and its protected approach region.
func (a *shelf) stripCenter() int {
	if a.notchW <= 0 {
		return a.x + panelWidth/2
	}
	return a.notchX + a.notchW/2
}

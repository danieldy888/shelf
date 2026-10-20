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
	// The panel, the peek pill and the strip share one axis: the notch's
	// centre. A different centre made the expansion jump sideways.
	a.x = d.Bounds.X + (d.Bounds.Width-panelWidth)/2
	if a.notchW > 0 {
		a.x = a.notchX + a.notchW/2 - panelWidth/2
	}
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
// The strip is always wide enough for the now-playing peek pill, so a track
// coming or going changes the drawing without a native resize.
func (a *shelf) desiredFrame() mygo.Rectangle {
	if a.visible || a.progress > 0.01 {
		// A shrinking panel keeps its frame until the drawn shell has
		// eased down to the new height, so the bottom is never cut off.
		h := max(a.panelH(), int(a.shownH+0.5))
		return mygo.Rectangle{X: a.x, Y: a.y, Width: panelWidth, Height: h}
	}
	// The resident strip holds the largest peek shape for the current
	// state, so hovering only redraws; it resizes when a track comes or
	// goes, while nothing is drawn.
	pw, ph := a.peekSize()
	if a.np != nil && a.np.hasTrack() && a.settings.IslandNowPlaying {
		pw, ph = a.playerSize()
	}
	w := max(int(pw+0.5), notchTriggerWidth)
	h := max(int(ph+0.5), max(32, a.notchH)+peekStripPadH, notchTriggerHeight)
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

// peekSize is the hover shape: the now-playing player while a track is
// loaded (the notch grows into it, Droppy style), the drop pill while a
// drag is on, otherwise a bulge carrying a chevron. The glyphs sit wholly
// below the hardware notch: nothing drawn inside it is visible.
func (a *shelf) peekSize() (w, h float32) {
	if a.np != nil && a.np.showsPlayer() {
		return a.playerSize()
	}
	baseW, baseH := float32(max(180, a.notchW)), float32(max(32, a.notchH))
	if a.dragging && !a.ownDrag {
		return baseW + dropPillExtraW, baseH + dropPillBody
	}
	return baseW + 40, baseH + peekBody
}

// playerSize is the hover player's size: npPlayerW wide (wider on a wide
// notch) and npPlayerBody tall below the notch.
func (a *shelf) playerSize() (w, h float32) {
	baseW := float32(max(180, a.notchW))
	return max(baseW+npPlayerMinX, npPlayerW), float32(max(32, a.notchH)) + npPlayerBody
}

// shellSize is the one interpolation the shell Path and the content layout
// share: hardware notch → peek shape peekW×peekH (peekK) → expanded panel
// of height panelH (openK). The caller passes the drawn values: view()
// animates them per display frame and substitutes final ones in headless
// tests.
func (a *shelf) shellSize(openK, peekK, peekW, peekH, panelH float32) (w, h float32) {
	baseW := float32(max(180, a.notchW))
	baseH := float32(max(32, a.notchH))
	expW, expH := float32(panelWidth-16), panelH-8
	w0 := baseW + (peekW-baseW)*peekK
	h0 := baseH + (peekH-baseH)*peekK
	return w0 + (expW-w0)*openK, h0 + (expH-h0)*openK
}

// peekPillRect is the on-screen box of the hovered notch capsule, matching
// the shell drawn in view(): the now-playing pill while a track is
// loaded, otherwise the chevron bulge. A press anywhere on it expands the
// shelf — the design has no in-pill controls.
func (a *shelf) peekPillRect() mygo.Rectangle {
	w, h := a.peekSize()
	return mygo.Rectangle{X: a.stripCenter() - int(w/2), Y: a.y, Width: int(w + 0.5), Height: int(h + 0.5)}
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
	// A press on the visible pill expands either way — except a press the
	// hover player received itself: its transport buttons handle that, and
	// the player opens the shelf for presses elsewhere on it.
	if a.peeking && !a.visible && (inside || a.overPeekPill(p)) {
		if inside && a.np != nil && a.np.showsPlayer() {
			return
		}
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

// While idle the overlay ignores the mouse altogether, so clicks under and
// beside the notch reach whatever is below. Peeking, expanded and in-flight
// drags take the whole window: file drags to the top of the notch must hit
// a window, or WindowServer treats Y=0 as the Spaces edge (pointerDrag
// flips this on the drag's first movement, long before the top).
func (a *shelf) syncMousePassThrough() {
	a.syncWindowFrame()
	acceptAll := a.visible || a.progress > 0.01 || a.dragging || a.pointerDrag || a.peeking || a.peekProgress > 0.01
	a.ignoringMouse = !acceptAll
	if a.win == nil {
		return
	}
	h := a.win.NativeHandle()
	mac.SetOverlayAcceptsMouse(h, acceptAll)
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

// panelH returns the expanded panel's height: the content's own measure
// (panelTargetH) clamped to the cap place() computed from the display's
// work area; before the first placement the cap is panelHeight.
func (a *shelf) panelH() int {
	cap := a.h
	if cap <= 0 {
		cap = panelHeight
	}
	return min(cap, a.panelTargetH())
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
	// Each opening starts its height animation afresh at the panel's
	// current height (see view): no easing from a previous session's size.
	a.openGen++
	a.shownH = 0
	a.animationFrom, a.animationStart = a.progress, time.Now()
	a.syncMousePassThrough()
	if a.win != nil {
		a.win.ShowInactive()
		// ShowInactive 不夺焦，面板收不到按键，只能临时注册全局 Esc 与空格。
		a.holdKeys()
		a.win.Invalidate()
	}
}

// Global hotkey seams: holdKeys/releaseKeys register and unregister the
// panel's Esc and Space, which needs the app running; tests stub them to
// check who owns the keys.
var (
	registerKey   = mygo.GlobalShortcut.Register
	unregisterKey = mygo.GlobalShortcut.Unregister
)

// holdKeys takes Esc (close Quick Look, then collapse) and Space (Quick
// Look the selection) while the shelf is in use; releaseKeys returns them
// as soon as the user clicks in another app.
func (a *shelf) holdKeys() {
	if !a.keysAreOurs() {
		// 设置窗口在前台时它自己处理 Esc 与空格（Esc/⌘W 关窗、控件收
		// 空格），全局注册会把按键从它手里抢走；剪贴板搜索模式的窗口同
		// 理。两条路径都先确保已注册的按键已经归还。
		a.releaseKeys()
		return
	}
	if !a.escHeld {
		if err := registerKey("Escape", a.escape); err != nil {
			a.report(fmt.Errorf("Esc 被其他应用占用，请用快捷键或收起按钮：%w", err))
		} else {
			a.escHeld = true
		}
	}
	if !a.spaceHeld {
		// Without Space, Quick Look stays available from the context menu.
		a.spaceHeld = registerKey("Space", a.toggleQuickLook) == nil
	}
	a.syncCopyKey()
}

// ShowInactive does not give the overlay keyboard focus. Hold ⌘C like
// Space only while a selected tray picture can actually be copied.
const copyKey = "Cmd+C"

// wantsCopyKey reports whether the tray would act on ⌘C right now: the
// panel is open with its keys, on the tray page, and exactly one selected
// item is a picture. Pure; unit-tested.
func (a *shelf) wantsCopyKey() bool {
	if !a.visible || !a.keysAreOurs() || a.tab != pageTray || a.selection.count() != 1 || a.store == nil {
		return false
	}
	for _, item := range a.store.Items {
		if a.selection.has(item.ID) {
			return !item.OffTray && !a.isLeaving(item) && convertible(item)
		}
	}
	return false
}

// syncCopyKey follows selection changes without reclaiming keys released
// by an outside click. Headless views have no native key to acquire.
func (a *shelf) syncCopyKey() {
	if a.win == nil {
		return // headless test shelf: no app to register with
	}
	switch want := a.wantsCopyKey() && (a.escHeld || a.spaceHeld); {
	case want && !a.cmdCHeld:
		if registerKey(copyKey, a.copyTrayImage) == nil {
			a.cmdCHeld = true
		}
	case !want && a.cmdCHeld:
		unregisterKey(copyKey)
		a.cmdCHeld = false
	}
}

// keysAreOurs reports whether the shelf may hold the global Esc and Space:
// never while the Settings or 重新命名 window is key, and never while the
// panel itself has the keyboard for clipboard search. Pure; unit-tested.
func (a *shelf) keysAreOurs() bool {
	if a.settingsWin != nil {
		return false
	}
	if a.rn != nil && a.rn.win != nil {
		return false
	}
	return !a.focusMode
}

func (a *shelf) releaseKeys() {
	if a.escHeld {
		unregisterKey("Escape")
		a.escHeld = false
	}
	if a.spaceHeld {
		unregisterKey("Space")
		a.spaceHeld = false
	}
	if a.cmdCHeld {
		unregisterKey(copyKey)
		a.cmdCHeld = false
	}
}

// escape closes Quick Look first, like Finder, then clears the clipboard
// search while it holds text, and finally collapses the shelf.
func (a *shelf) escape() {
	if mac.QuickLookVisible() {
		mac.CloseQuickLook()
		return
	}
	if a.tab == pageClipboard && a.search != "" {
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
	if a.usage != nil { // 隐藏即取消 AI 用量的在途取数和扫描
		a.usage.stopWork()
	}
	a.animationFrom, a.animationStart = a.progress, time.Now()
	a.peekFrom, a.peekAnimStart = a.peekProgress, time.Now()
	// 收起后回到默认视图：下次打开（悬停、拖入、托盘）从托盘标签开始，
	// 搜索框清空；键盘模式在用户明确的收起动作下交还焦点。
	a.focusMode, a.tab, a.tabFrom, a.search, a.reveal = false, pageTray, pageTray, "", ""
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
		k := min(float32(now.Sub(a.peekAnimStart))/float32(peekAnimation), 1)
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

// stripCenter is shared by the drop surface and its protected approach region.
func (a *shelf) stripCenter() int {
	if a.notchW <= 0 {
		return a.x + panelWidth/2
	}
	return a.notchX + a.notchW/2
}

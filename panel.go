//go:build darwin

package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/store"
)

var (
	shelfSymbol = symbol(`<path d="M4 5v14h16V5M4 14h5l2 3h2l2-3h5M8 5h8M12 3v8m-3-3 3 3 3-3"/>`)
	textSymbol  = symbol(`<path d="M5 5h14M12 5v14M8 19h8"/>`)
	fileSymbol  = symbol(`<path d="M14 3H6v18h12V7zM14 3v5h4M9 12h6M9 16h6"/>`)
	closeSymbol = symbol(`<path d="m6 6 12 12M18 6 6 18"/>`)
	// 图钉：置顶标记（网格角标与列表行尾），填充的竖向 pin。
	pinSymbol = symbol(`<path d="M12 17v5M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16h14v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1v3.76z"/>`)
	pinFill   = fillSymbol(`<path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16h14v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1v3.76z"/><path d="M12 17v5" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/>`)
	// 耳栏：模块切换（托盘、剪贴板）与全局按钮（设置、收起）；清空在模块栏。
	trayNavSymbol   = symbol(`<path d="M3 13.5 5.5 5h13l2.5 8.5V19H3z"/><path d="M3 13.5h5.5l1.5 2.5h4l1.5-2.5H21"/>`)
	clipNavSymbol   = symbol(`<rect x="5" y="4" width="14" height="17" rx="2.5"/><path d="M9 4V3h6v1M9 10h6M9 14h4"/>`)
	trashSymbol     = symbol(`<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13"/>`)
	gearSymbol      = symbol(`<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>`)
	chevronUpSymbol = symbol(`<path d="M6 15l6-6 6 6"/>`)
	chevronDownSym  = symbolWidth(`<path d="M6 9l6 6 6-6"/>`, 2.2)
	searchSymbol    = symbol(`<circle cx="11" cy="11" r="7"/><path d="M20 20l-4-4"/>`)
)

// The expanded panel's pages are registered in pages.go (pageID,
// panelPage); tray and clipboard are the built-in pages, further pages
// register themselves in ear order.

func symbol(paths string) *ui.SVG {
	return symbolWidth(paths, 1.7)
}

// symbolWidth is symbol with a custom stroke width (the peek chevron uses
// the mock's heavier 2.2).
func symbolWidth(paths string, w float64) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-width="` + fmt.Sprintf("%g", w) + `" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

// fillSymbol is symbol for solid glyphs (pin badge, transport controls):
// the path fills with currentColor instead of stroking it.
func fillSymbol(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" fill="currentColor" stroke="none">` + paths + `</svg>`))
}

// DESIGN.md 色彩令牌：纯黑面板上只靠白色透明度分层，唯一强调色系统蓝。
var (
	ink      = ui.Hex("#F5F5F7")            // 主文字、主图标
	ink2     = ui.RGBA(255, 255, 255, 0.56) // 次文字、非激活标签、图标按钮
	ink3     = ui.RGBA(255, 255, 255, 0.34) // 辅助说明、时间、计数
	fill1    = ui.RGBA(255, 255, 255, 0.06) // 悬停底、搜索框
	fill2    = ui.RGBA(255, 255, 255, 0.10) // 激活标签胶囊、按钮悬停
	fill3    = ui.RGBA(255, 255, 255, 0.16) // 进度条轨道
	accent   = ui.Hex("#0A84FF")            // 唯一的强调色
	accent16 = accent.Alpha(0.16)           // 网格选中底
	accent24 = accent.Alpha(0.24)           // 列表选中底
	progress = ui.RGBA(255, 255, 255, 0.88) // 进度条
	danger   = ui.Hex("#FF453A")            // 错误红点
)

// 旧调色板名仍被 settings.go（同伴正在重写它）引用；全部映射到新令牌，
// 等那边落地后可以删掉别名。
var (
	textPrimary  = ink
	textMuted    = ink2
	cardSurface  = fill1
	cardHover    = fill2
	cardSelected = accent24
	hairline     = fill2
)

// panelTheme keeps the frame dark with the design's single accent: the
// default theme would take the user's accent color and a light appearance
// for scrollbars, focus rings and the search caret.
var panelTheme = func() *ui.Theme {
	t := ui.DarkTheme()
	t.Accent = accent
	t.AccentHover = accent.Alpha(0.8)
	t.AccentPressed = accent.Alpha(0.65)
	t.Selection = accent.Alpha(0.35)
	t.Focus = accent.Alpha(0.6)
	t.Background = ui.Transparent
	// Tooltips (the ear icons' names) take Inverse/InverseText, which
	// otherwise fall back to Text on Background — ink on transparent, a
	// blank white tip. A raised dark chip reads on the black panel.
	t.Inverse = ui.Hex("#2C2C2E")
	t.InverseText = ink
	t.FontSize = 13
	return t
}()

// 展开面板布局常量（对应 DESIGN.md「展开面板」）：耳栏 → 活动条 → 模块栏
// → 舞台，提示浮在舞台底部。
const (
	panelPadX      = 16                      // 左右内边距
	panelPadBottom = 16                      // 底部内边距
	earMinH        = 32                      // 耳栏最低高度（无刘海屏按菜单栏留出）
	earBtn         = 26                      // 耳栏图标按钮
	earBtnGap      = 6                       // 耳栏按钮间距
	earHousingPad  = 6                       // 摄像头壳体两侧各让出的空隙
	menuBarMinH    = 24                      // 无刘海屏的菜单栏高
	headerTopGap   = 8                       // 键盘搜索时：菜单栏下沿到耳栏
	headerH        = 28                      // 键盘搜索时耳栏的行高
	earBottomGap   = 8                       // 耳栏到活动条或模块栏
	liveH          = 56                      // 活动条高
	liveGap        = 12                      // 活动条到模块栏
	liveMax        = 2                       // 活动条最多并排的活动数
	moduleBarH     = 28                      // 模块栏高
	stageGap       = 6                       // 模块栏到舞台
	stageMinH      = 140                     // 舞台最矮（空状态）
	stageMaxH      = 320                     // 舞台最高，超出在舞台内滚动
	gridCols       = 5                       // 托盘网格列数
	gridGap        = 4                       // 格子间距
	tileH          = 100                     // 格子高
	clipSearchH    = 32                      // 搜索框高
	clipListTop    = 8                       // 列表上边距
	clipRowH       = 46                      // 剪贴板行高
	clipRowGap     = 2                       // 行间距
	hintPillH      = 34                      // 提示胶囊：12 上距 + 22 高
	panelMaxH      = 560                     // 面板最大高，受屏高钳制
	toastLife      = 2500 * time.Millisecond // 状态胶囊的展示时长
	toastFade      = 220 * time.Millisecond  // 淡出时长
	// earR 是壳顶边左右各伸出的耳角半径；窗口宽度必须包含两侧耳角，
	// 任何一帧耳角都是完整圆弧（desiredFrame 据此加宽）。
	earR = 10
)

// Page switch "fade through": the page being left fades out over the first
// tabOutShare of the switch, then the new page fades in, drifting tabShift
// points from its icon's side. The two never overlap, so their different
// rows (a grid, a list) don't show through each other.
const (
	tabShift    = 8
	tabOutShare = 0.35
)

// tabPhase is how far a page switch has run, 0→1 over tabAnimation; 1 when
// no switch is under way.
func (a *shelf) tabPhase(now time.Time) float32 {
	if a.tabAt.IsZero() {
		return 1
	}
	t := float32(now.Sub(a.tabAt)) / float32(tabAnimation)
	return min(max(t, 0), 1)
}

// panelHeightKey keys the panel's height animation to one opening, so a
// new opening starts at its own height instead of easing from the last.
type panelHeightKey struct{ gen int }

// view 画三种状态：空闲（完全不可见）、探头（悬停播放器、下拉箭头或拖拽
// 提示）、展开（纯黑面板从刘海里长出来）。单元测试里 a.win 为 nil，
// 直接按完整展开态渲染。
func (a *shelf) view(c *ui.Context) {
	a.buildingView = true
	defer func() { a.buildingView = false }()
	// A page that got switched off (or a stale id) must not stay current:
	// fold back to the tray before anything reads a.tab.
	a.normalizeTab()
	// Selection may change while building this frame.
	defer a.syncCopyKey()
	if c.Shortcut(0, ui.KeyEscape) {
		a.escape()
	}
	c.OnShortcut(ui.Cmd, ui.KeyQ, mygo.App.Quit)
	// 剪贴板标签的回车：焦点在搜索框时由输入框的 OnTextKey 处理（输入框先
	// 拿到方向键和回车），焦点在别处时在这里兜底。Esc 无此需要：输入框不
	// 消费它，窗口顶部注册的 Shortcut 已经覆盖所有焦点位置。
	if a.visible && a.tab == pageClipboard {
		if c.Shortcut(0, ui.KeyEnter) {
			a.copyClipSelection()
		}
	}
	// 托盘页选中一张图片时，⌘C 复制图片数据（文件项保持原有行为）。
	if a.visible && a.tab == pageTray {
		c.OnShortcut(ui.Cmd, ui.KeyC, a.copyTrayImage)
	}
	c.SetTheme(panelTheme)
	// 常驻窗口是 Transparent 的：mygo 根元素默认画满主题背景，必须改成
	// 全透明，否则整个窗口会一直盖着一块主题色灰板。
	c.Root().Background(ui.Transparent)
	opts := transfer.DropOptions{Formats: []transfer.Format{transfer.FileList, transfer.URIList, transfer.PNG, transfer.Text}, Operations: transfer.Copy}
	now := c.Now()
	// What a closed panel has to say grows out of the notch (feedback.go);
	// the text is measured before the shell is sized around it.
	// 悬停或外来拖拽占用探头形状期间，胶囊根本没画出来：计时随让位整体
	// 滑动（含没画过的新提示——它的时钟本来就从第一帧算起），回来后按
	// 剩余时间继续，绝不会被静默吃掉。
	if f := &a.fb; f.text != "" && !f.live {
		yielded := !a.visible && a.progress <= 0.01 && (a.peeking || (a.dragging && !a.ownDrag))
		switch {
		case yielded && f.pausedAt.IsZero():
			f.pausedAt = now
		case yielded:
			d := now.Sub(f.pausedAt)
			f.until, f.shownAt, f.pausedAt = f.until.Add(d), f.shownAt.Add(d), now
		case !f.pausedAt.IsZero():
			d := now.Sub(f.pausedAt)
			f.until, f.shownAt = f.until.Add(d), f.shownAt.Add(d)
			f.pausedAt = time.Time{}
		}
	}
	a.fb.follow(a, now)
	if f := &a.fb; f.text != "" && f.pillW == 0 {
		f.pillW = f.measure(c)
	}
	capsule := a.capsuleUp(now)
	if capsule {
		a.fb.schedule(c, now)
	}
	openTarget, peekTarget := float32(0), float32(0)
	if a.visible {
		openTarget = 1
	}
	if a.peeking || capsule {
		peekTarget = 1
	}
	// Keep one native drop target across all states. The transparent root does
	// not paint the theme background; clipping contains every animation frame.
	zone := ui.Column(c.Key("shelf")).Fill().AlignItems(ui.Center).Clip()
	// Draw springs at display cadence. Their last drawn values also drive
	// native frame size and mouse pass-through.
	openK, peekK, contentK := a.progress, a.peekProgress, float32(1)
	if a.win == nil && !a.peeking {
		openK = 1
	}
	sizeAnim := peekSpring
	if a.win != nil {
		openAnim := panelOpenSpring
		peekAnim := peekSpring
		if openTarget == 0 {
			openAnim = closeSpring
			// 展开中途关闭不得带着展开速度继续往外长：反向时重置为很小的
			// 向内初速度（临界阻尼下初速度 ≤ 0 保证壳尺寸单调变小）。
			openAnim.Spring.Velocity = -0.5
		} else if a.fx.openK < 1 && zone.Velocity("open") < 0 {
			// 收起中途重新打开也不得带着收起速度继续往回缩。
			zone.Jump("open", a.fx.openK)
			openAnim.Spring.Velocity = 0.5
		}
		switch {
		case capsule:
			// The capsule grows with the panel's own spring. A capsule that
			// is needed again ends any fold-back in flight.
			peekAnim, sizeAnim = openSpring, openSpring
			a.fb.retracting = false
		case peekTarget == 0 && (a.fb.drawn || a.fb.retracting):
			// The capsule leaves on a spring that never overshoots, for the
			// whole fold — not only the fold's first frame.
			peekAnim = retractSpring
			a.fb.retracting = true
		}
		if peekTarget == 0 {
			peekAnim.Spring.Velocity = -0.5 // 收起反向同样不带外冲速度
		}
		openK = zone.AnimateTo("open", openTarget, openAnim)
		if a.visible {
			// While the panel is opening, openK drives the shell alone:
			// freeze peek at its last drawn value so a second spring
			// cannot fight openK or reverse the shell on reopen.
			peekK = zone.Jump("peek", a.fx.peekK)
		} else {
			peekK = zone.AnimateTo("peek", peekTarget, peekAnim)
		}
		// The content follows its own channel, not openK: it springs in a
		// beat after the shell (contentSpring's 30ms delay), scales from
		// 0.88 and un-blurs as it fades in. While the panel is not expanded
		// the value rests at 0 — a Jump, not a spring — so every opening,
		// fresh or out of a close in flight, starts the content from
		// nothing instead of a stale fraction.
		if a.visible {
			contentK = zone.AnimateTo("content", 1, contentSpring)
		} else {
			contentK = zone.Jump("content", 0)
		}
		// AnimateTo first accounts for the time passed since the last frame
		// along the OLD target: a close issued mid-flight would otherwise
		// draw one more outward frame before turning. While closing, the
		// drawn interpolation may only shrink.
		if openTarget == 0 && openK > a.fx.openK {
			openK = a.fx.openK
		}
		if peekTarget == 0 && peekK > a.fx.peekK {
			peekK = a.fx.peekK
		}
		// A spring heading back to zero may overshoot its target; the shell
		// never shrinks past the hardware notch it grows from. The rest snap
		// stays under a point, so the fold visibly finishes instead of
		// jumping the last points.
		openK, peekK = max(openK, 0), max(peekK, 0)
		if openK < 0.002 {
			openK = 0
		}
		if peekK < 0.002 {
			peekK = 0
		}
		if a.peeking || peekK == 0 {
			a.fb.retracting = false
		}
	}
	// 真实窗口的彻底空闲态：什么都不画，常驻窗口完全不可见。阈值与上面
	// 的静止吸附一致（亚点级），收起最后一段平滑落位、看不出跳变。
	idle := a.win != nil && !a.visible && a.progress <= 0.01 && openK <= 0.002 && !a.peeking && peekK <= 0.002
	// Shelf's own drag-out never lands back on the shelf: the drop would
	// add a second item pointing at the same file. Without a target the
	// drag ends as if cancelled — nothing is added, the panel stays open
	// and 拖出后从托盘移除 does not fire.
	accepts := !a.ownDrag
	var over bool
	if accepts {
		_, over = ui.DataDragOver(zone, opts)
	}
	if idle {
		// 空闲态收到内容只入库不展开：那时窗口本就不显示，也无从接受拖放。
		// The mesh is the hardware notch: nothing is drawn, and the frame
		// the next state starts from is the notch's own box.
		a.fx.w, a.fx.h = float32(max(180, a.notchW)), float32(max(32, a.notchH))
		a.fx.peekW, a.fx.peekH = 0, 0
		a.fx.peek, a.fx.openMoving, a.fx.sizeMoving, a.fb.drawn = false, false, false, false
		a.fx.openK, a.fx.peekK = 0, 0
		a.fb.clearExpired(now, true)
		a.syncMousePassThrough()
		if accepts {
			if drop, ok := ui.DropData(zone, opts); ok {
				a.drop.done = true
				a.receive(drop.Data)
			}
		}
		return
	}
	peeked := a.peeking || peekK > 0.01
	// Content shows only while the panel is open. A closing panel is just
	// the shell retracting: hide() has already reset the tab and search,
	// and drawing that reset state on the way out would flip the content.
	expanded := a.win == nil && !a.peeking || a.visible
	closing := !expanded && openK > 0
	// The panel's height eases between contents (tab switch, rows coming
	// and going); the content itself is laid out at the target height.
	// While closing, the shell keeps the height it was showing.
	// The drop zone's file snapshot runs before the height math so the
	// panel already sizes for the zone on the frame it appears — no
	// clipped first frame and no mid-drag resize.
	if expanded {
		a.dropZoneSnap(over)
	} else {
		a.drop.on, a.drop.need = false, 0
	}
	targetH := float32(a.panelH())
	shownH := targetH
	// Keep the closing height until the drawn spring reaches the notch.
	if a.win != nil && (a.visible || openK > 0) {
		if closing && a.shownH > 0 {
			targetH = a.shownH
		}
		shownH = zone.AnimateTo(panelHeightKey{a.openGen}, targetH, ui.Animation{Duration: resizeAnimation, Ease: ui.EaseInOut})
	}
	a.shownH = shownH
	// The hover shape springs between its forms (chevron, drop pill,
	// player, capsule) when it changes while shown, e.g. a drag starting.
	// A fold is subtract-only: while the peek closes (peekTarget 0), the
	// shape freezes at what is actually drawn — re-aiming at the live
	// peekSize() answer would grow the folding shell toward a track's
	// player size before it could shrink.
	peekW, peekH := a.peekSize()
	if a.win != nil {
		if expanded {
			// Opening uses one fixed peek shape so its morph cannot fight
			// openK. Closing instead follows the current track/drag state.
			if a.fx.peekK > 0 && a.fx.peekW > 0 && a.fx.peekH > 0 {
				peekW, peekH = a.fx.peekW, a.fx.peekH
			}
			peekW = zone.Jump("peek-w", peekW)
			peekH = zone.Jump("peek-h", peekH)
		} else {
			switch {
			case closing:
				// Panel fold: the peek shape follows the live track/drag
				// state on the way down.
				peekW = zone.AnimateTo("peek-w", peekW, closeSpring)
				peekH = zone.AnimateTo("peek-h", peekH, closeSpring)
			case peekTarget == 0 && a.fx.peekW > 0 && a.fx.peekH > 0:
				// Peek folding to the notch is subtract-only: pin the
				// drawn shape. Jump, not AnimateTo — AnimateTo keeps an
				// outward velocity and grew the shell past its drawn
				// size when the pointer left mid-morph.
				peekW = zone.Jump("peek-w", a.fx.peekW)
				peekH = zone.Jump("peek-h", a.fx.peekH)
			case a.dragging && !a.ownDrag && !a.dropHot && (a.fx.peekW > peekW || a.fx.peekH > peekH):
				// An incoming drag outside the drop band owns the hint
				// shape. A fold remnant pinned mid-morph (e.g. the hover
				// player still growing when the drag began) rides the
				// re-rising shell past the menu bar for the whole
				// morph — the "notch flashes" bug. The drag's shape
				// never morphs, so snap the channels: the drawn shell
				// may only shrink from here.
				peekW = zone.Jump("peek-w", peekW)
				peekH = zone.Jump("peek-h", peekH)
			default:
				peekW = zone.AnimateTo("peek-w", peekW, sizeAnim)
				peekH = zone.AnimateTo("peek-h", peekH, sizeAnim)
			}
		}
	}
	a.fx.peekW, a.fx.peekH = peekW, peekH
	a.fx.sizeMoving = a.win != nil && (!zone.Settled("peek-w") || !zone.Settled("peek-h"))
	// 刘海壳尺寸在三态间插值：硬件刘海 ←→ 探头胶囊 ←→ 完整面板
	// （shellSize 是几何的唯一来源）。
	shellW, shellH := a.shellSize(openK, peekK, peekW, peekH, shownH)
	shellW, shellH = max(shellW, 0), max(shellH, 0)
	// The frame follows the drawn shell while the springs are in flight (see
	// desiredFrame): the opening overshoots, and a shape morph can be larger
	// than the shape it heads for.
	a.fx.w, a.fx.h = shellW, shellH
	a.fx.openK, a.fx.peekK = openK, peekK
	a.fx.openMoving = a.win != nil && !zone.Settled("open")
	a.fx.peek = !expanded && peeked && !closing
	a.syncMousePassThrough()
	// A failed job shakes the capsule, and only the capsule; the drawn
	// offset goes into fx so the hit box and frame follow the shape.
	var shake float32
	if capsule {
		shake = a.fb.shake(now, c.Preferences().ReduceMotion)
	}
	a.fx.shakeX = shake
	// 窗口贴在屏幕 Y=0（展开=面板全幅，其余=刘海顶边窄条，见
	// syncWindowFrame）：壳水平居中、顶端贴齐窗口顶边，探头才能从硬件
	// 刘海里长出来，而不是浮在菜单栏下方。窗口宽度含两侧耳角（earR），
	// 这里画出的耳角任何一帧都不会被裁。
	zone.Draw(func(p *ui.Painter, r ui.Rect) {
		x0 := (r.W - shellW) / 2
		x1 := x0 + shellW
		y1 := shellH
		ear := float32(earR)
		// 底部圆角：探头胶囊半圆 → 展开 30pt。The island reads rounded from
		// the first frame, so the corner no longer tracks openK.
		botR := min(shellH/2, 30)
		var path ui.Path
		path.MoveTo(x0-ear+shake, 0)
		path.QuadTo(x0+shake, 0, x0+shake, earR)
		path.LineTo(x0+shake, y1-botR)
		path.QuadTo(x0+shake, y1, x0+shake+botR, y1)
		path.LineTo(x1-botR+shake, y1)
		path.QuadTo(x1+shake, y1, x1+shake, y1-botR)
		path.LineTo(x1+shake, earR)
		path.QuadTo(x1+shake, 0, x1+ear+shake, 0)
		path.Close()
		p.FillPath(&path, ui.Hex("#000000"))
	})
	zone.Children(func() {
		if !expanded {
			if closing || !peeked {
				// 收起途中只有壳缩回刘海，不画内容。
				return
			}
			a.peekView(c, shellW, shellH, openK, peekK, shake, over, peekTarget == 0)
			return
		}
		// The content is laid out once at the panel's final size and the
		// growing shell uncovers it, so nothing inside moves or reflows
		// while the panel opens, closes or changes height.
		clip := ui.Column(c.Key("panel-clip")).Width(shellW).Height(shellH).Clip().AlignItems(ui.Center)
		clip.Children(func() {
			content := ui.Column(c.Key("panel")).Width(panelWidth-16).Height(targetH-8).
				Padding(0, panelPadX, panelPadBottom, panelPadX)
			if a.win != nil {
				// The content grows with the shell like an island: it
				// springs in a beat later (contentK), scaling up from its
				// top centre and shedding blur as it fades in. ReduceMotion
				// keeps only the fade — the channel also settles at once
				// there, so scale and blur are 1 and 0 anyway.
				content.Opacity(min(max((contentK-0.1)/0.6, 0), 1))
				if !c.Preferences().ReduceMotion {
					s := 0.88 + 0.12*contentK
					content.TransformOrigin(0.5, 0).Scale(s, s)
					// 不可见的内容不值得一次离屏模糊：低于淡入起点
					// (k ≤ 0.1 ⇒ opacity 0) 或残余模糊已不可辨 (< 0.25pt) 时跳过。
					if blur := 6 * (1 - contentK); contentK > 0.1 && blur >= 0.25 {
						content.Blur(blur)
					}
				}
			}
			content.Children(func() {
				a.earBar(c)
				a.activityStrip(c)
				a.dropBody(c, over)
				a.toastLayer(c, over)
			})
		})
	})
	// What this frame drew decides the next one's retraction spring and
	// whether the capsule's pill takes clicks. Once the folded-back shell
	// has fully retracted, the expired text is dropped for good.
	a.fb.drawn = capsule && !expanded && !closing && peeked
	a.fb.clearExpired(now, peekK == 0 && !capsule)
	if !accepts {
		return
	}
	if drop, ok := ui.DropData(zone, opts); ok {
		a.drop.done = true
		a.receive(drop.Data)
		if !a.visible {
			a.show(false)
		}
	}
}

// tabBody holds the current page's module bar and stage, fading through
// from the page just left (see tabOutShare). This is driven per frame
// rather than by an element Transition: a switch that changes the panel's
// height resizes the window, and mygo does not animate a resized frame's
// transitions.
func (a *shelf) tabBody(c *ui.Context, over bool) {
	t := float32(1)
	if a.win != nil {
		t = a.tabPhase(c.Now())
	}
	// The pages sit left to right as their icons do: going right, the new
	// page arrives from the right.
	dir := float32(1)
	if a.pageIndex(a.tab) < a.pageIndex(a.tabFrom) {
		dir = -1
	}
	if c.Preferences().ReduceMotion {
		dir = 0
	}
	stage := ui.Column(c.Key("stage")).FillWidth().Grow(1).MinHeight(0)
	stage.Children(func() {
		body := ui.Column(c.Key("body")).FillWidth().Grow(1).MinHeight(0)
		if t < 1 {
			in := ui.EaseOut(max(t-tabOutShare, 0) / (1 - tabOutShare))
			body.Opacity(in).Translate(tabShift*dir*(1-in), 0)
			c.AnimationFrame()
		}
		body.Children(func() { a.panelBody(c, over, a.currentPage(), true) })
		if t < tabOutShare && a.tabFrom != a.tab {
			// The outgoing page is only a picture: it takes no pointer
			// and never the keyboard. It may already be disabled; an
			// unregistered one is skipped instead of drawn half-built.
			if out := a.page(a.tabFrom); out != nil {
				old := ui.Column(c.Key("body-out")).Absolute().Left(0).Right(0).Top(0).Bottom(0).
					PassThrough().Opacity(1 - ui.EaseIn(t/tabOutShare))
				old.Children(func() { a.panelBody(c, false, out, false) })
			}
		}
	})
}

// earLayout places the ear bar. Normally it sits in the notch band itself,
// split around the camera housing; there is nothing else in that band
// while the panel is open. While the clipboard search holds the keyboard
// the window drops below the menu bar's level (see windowLevel), so the
// band is covered: the bar then moves below the menu bar as an ordinary
// row, without the housing gap.
func (a *shelf) earLayout() (spacer, rowH int, inBand bool) {
	if a.focusMode {
		return max(a.topInset, menuBarMinH) + headerTopGap, headerH, false
	}
	return 0, max(earMinH, a.topInset), true
}

// earBar is the panel's top row: page icons in the left ear, settings and
// collapse in the right one, the camera housing between them. The active
// page's capsule is drawn by the bar and glides to the other icon on a
// switch instead of jumping.
func (a *shelf) earBar(c *ui.Context) {
	spacer, rowH, inBand := a.earLayout()
	if spacer > 0 {
		ui.Box(c.Key("ear-spacer")).Height(float32(spacer))
	}
	bar := ui.Row(c.Key("ears")).Height(float32(rowH)).AlignItems(ui.Center).FillWidth()
	var navs map[pageID]ui.Element
	bar.Children(func() {
		// Both ears grow from a zero basis, so they split the width evenly
		// and the housing gap stays centred on the notch.
		ui.Row(c.Key("ear-left")).Grow(1).MinWidth(0).Gap(earBtnGap).AlignItems(ui.Center).Children(func() {
			navs = map[pageID]ui.Element{}
			for i, p := range a.enabledPages() {
				if i >= earMaxPages {
					// 左耳最多 5 个页面图标（DESIGN.md）；多出的页面仍能
					// 通过 setTab 打开，只是没有耳栏图标。
					break
				}
				navs[p.id()] = a.navButton(c, p)
			}
		})
		if inBand && a.notchW > 0 {
			ui.Box(c.Key("ear-housing")).Width(float32(a.notchW + 2*earHousingPad)).Shrink(0)
		}
		ui.Row(c.Key("ear-right")).Grow(1).MinWidth(0).Gap(earBtnGap).AlignItems(ui.Center).Justify(ui.End).Children(func() {
			if a.iconButton(c, "settings", gearSymbol, "设置").Clicked() {
				a.openSettings()
			}
			if a.iconButton(c, "collapse", chevronUpSymbol, "收起").Clicked() {
				a.hide(hideExplicit)
			}
		})
	})
	// The capsule follows the active icon's box from the last frame; the
	// icon paints its own capsule until that box is known (first frame).
	cur, ok := navs[a.currentPage().id()]
	r := cur.LayoutBounds()
	if !ok || r.W <= 0 {
		cur.Background(fill2)
		return
	}
	x := r.X
	if a.win != nil {
		if a.tabPhase(c.Now()) < 1 {
			// A page switch glides the capsule to the other icon.
			x = bar.AnimateTo("tab-x", r.X, ui.Animation{Duration: tabAnimation, Ease: ui.EaseInOut})
		} else {
			// Every other move (opening, the window shrink once the spring
			// rests, a new height) keeps the capsule on the icon instead of
			// easing behind it.
			x = bar.Jump("tab-x", r.X)
		}
	}
	prev := bar.LayoutBounds()
	bar.Draw(func(p *ui.Painter, box ui.Rect) {
		// The capsule rect comes from last frame's boxes; anchor it to the
		// bar's current box so a frame that moved the bar (window shrink
		// after the open spring rests, the blur pass) cannot unglue it.
		dx, dy := float32(0), float32(0)
		if prev.W > 0 {
			dx, dy = box.X-prev.X, box.Y-prev.Y
		}
		p.Fill(ui.Rect{X: x + dx, Y: r.Y + dy, W: r.W, H: r.H}, fill2, r.H/2)
	})
}

// navButton is one page icon of the left ear: ink when active (its capsule
// is earBar's), ink2 with a fill1 hover otherwise. The page's name is its
// tooltip and accessible label; the module bar spells it out below.
func (a *shelf) navButton(c *ui.Context, p panelPage) ui.Element {
	on := p.id() == a.currentPage().id()
	b := ui.ButtonBase(c.Key("page-"+string(p.id()))).Size(earBtn, earBtn).Radius(earBtn / 2).Center().Label(p.label()).Tooltip(p.label())
	if !on && b.Hovered() {
		b.Background(fill1)
	}
	b.Children(func() {
		g := p.icon(c, on || b.Hovered()).Size(15, 15)
		if on || b.Hovered() {
			g.TextColor(ink)
		} else {
			g.TextColor(ink2)
		}
	})
	if b.Clicked() {
		a.setTab(p.id())
	}
	return b
}

// iconButton is one 26×26 circular icon button (ears and module bar).
func (a *shelf) iconButton(c *ui.Context, key string, icon *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(earBtn, earBtn).Radius(earBtn / 2).Center().Label(label).Tooltip(label)
	if b.Hovered() {
		b.Background(fill2)
	}
	b.Children(func() {
		g := ui.Icon(c, icon).Size(15, 15)
		if b.Hovered() {
			g.TextColor(ink)
		} else {
			g.TextColor(ink2)
		}
	})
	return b
}

// liveActivities are the modules with something under way, in module
// order, at most liveMax: the strip has room for two side by side.
func (a *shelf) liveActivities() []liveActivity {
	var acts []liveActivity
	for _, m := range a.modules {
		if act, ok := m.(liveActivity); ok && act.live() {
			acts = append(acts, act)
			if len(acts) == liveMax {
				break
			}
		}
	}
	return acts
}

// activityStrip is the row under the ears that shows what is under way,
// on every page. With no activity it takes no space at all.
func (a *shelf) activityStrip(c *ui.Context) {
	acts := a.liveActivities()
	gap := float32(earBottomGap)
	if len(acts) == 0 {
		ui.Box(c.Key("live-none")).Height(gap)
		return
	}
	ui.Row(c.Key("live")).Height(liveH).FillWidth().Gap(6).Margin(gap, 0, liveGap, 0).Children(func() {
		for _, act := range acts {
			act.activity(c, len(acts) > 1)
		}
	})
}

// panelBody renders a page under the activity strip: its module bar, then
// its stage body from the page itself. live is false for the page fading
// out after a switch, which is drawn only.
func (a *shelf) panelBody(c *ui.Context, over bool, p panelPage, live bool) {
	a.moduleBar(c, p, live)
	p.body(c, over, live)
}

// moduleBar is the page's own 28pt row: its name and count on the left,
// the page's own controls on the right.
func (a *shelf) moduleBar(c *ui.Context, p panelPage, live bool) {
	title, count := p.moduleTitle()
	ui.Row(c.Key("module-bar")).Height(moduleBarH).AlignItems(ui.Center).FillWidth().
		Padding(0, 0, 0, 2).Margin(0, 0, stageGap, 0).Children(func() {
		ui.Text(c, title).SingleLine().FontSize(13).FontWeight(600).TextColor(ink)
		if count != "" {
			ui.Text(c, count).SingleLine().FontSize(11).FontWeight(600).TextColor(ink3).Margin(0, 0, 0, 8)
		}
		ui.Box(c).Grow(1)
		p.moduleTrailing(c, live)
	})
}

// clearable is what a page's 清空 removes: the unpinned rows the page
// shows, so the tray's button leaves the clipboard history alone and the
// other way round. While a search narrows the clipboard list there is
// nothing to clear — emptying only the matches would surprise.
func (a *shelf) clearable(list []store.Item, id pageID) []store.Item {
	if id == pageClipboard && strings.TrimSpace(a.search) != "" {
		return nil
	}
	var gone []store.Item
	for _, it := range list {
		if !it.Pinned {
			gone = append(gone, it)
		}
	}
	return gone
}

// searchField is the clipboard tab's search box: 32pt fill1 capsule with a
// magnifier, ink text and an accent caret. Typing filters the list as it
// goes; while the shelf was opened for search the box keeps the keyboard
// focus, so ↑/↓ walk the list and ⏎ copies and collapses without reaching
// for the mouse. Esc clears the search first, then collapses.
func (a *shelf) searchField(c *ui.Context, live bool) {
	wrap := ui.Row(c.Key("clip-search")).Height(clipSearchH).Padding(0, 10, 0, 10).Gap(7).
		AlignItems(ui.Center).Radius(10).Background(fill1)
	if !live {
		// The outgoing page's box is a picture of the field: an input here
		// would compete for the keyboard and the global keys.
		wrap.Children(func() {
			ui.Icon(c, searchSymbol).Size(14, 14).TextColor(ink3)
			if a.search != "" {
				ui.Text(c, a.search).SingleLine().FontSize(13).TextColor(ink)
			} else {
				ui.Text(c, "搜索剪贴板").SingleLine().FontSize(13).TextColor(ink3)
			}
		})
		return
	}
	wrap.Children(func() {
		ui.Icon(c, searchSymbol).Size(14, 14).TextColor(ink3)
		in := ui.TextInputBase(c.Key("clip-query"), &a.search).Grow(1).MinWidth(0).
			Placeholder("搜索剪贴板").Label(searchLabel).FontSize(13).TextColor(ink)
		if a.focusMode {
			in.Focus()
		}
		// 焦点在搜索框期间归还全局 Esc/空格：全局热键会在按键到达窗口
		// 前吃掉它们（空格就打不进查询里）；焦点一离开就收回，
		// 空格恢复快速预览。
		if a.win != nil {
			if in.Focused() {
				a.releaseKeys()
			} else if a.visible && !a.focusMode {
				a.holdKeys()
			}
		}
		in.OnTextKey(func(k ui.TextKey) bool {
			switch k.Key {
			case ui.KeyUp:
				a.moveClipSelection(-1)
			case ui.KeyDown:
				a.moveClipSelection(1)
			case ui.KeyEnter:
				a.copyClipSelection()
			case ui.KeyEscape:
				a.escape()
			default:
				return false
			}
			return true
		})
	})
}

// emptyState is what a page shows when it has no rows: a centered ink3
// icon, a medium ink2 title and an ink3 note line. The page picks the
// contents; this draws the shared layout.
func (a *shelf) emptyState(c *ui.Context, icon *ui.SVG, title, hint string) ui.Element {
	return ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
		ui.Icon(c, icon).Size(28, 28).TextColor(ink3)
		ui.Text(c, title).FontSize(13).FontWeight(500).TextColor(ink2)
		ui.Text(c, hint).TextAlign(ui.Center).FontSize(11).TextColor(ink3)
	})
}

// toastLayer stacks the panel's transient pills bottom-center: transient
// status (已复制/已放入/错误), the persistent update notice, the clipboard
// search hints and the drop hint while a drag hovers. PassThrough keeps
// the pills out of the pointer's way except for their own buttons.
func (a *shelf) toastLayer(c *ui.Context, over bool) {
	stack := ui.Column(c.Key("toasts")).Absolute().Left(16).Right(16).Bottom(10).
		AlignItems(ui.Center).Gap(6).PassThrough()
	stack.Children(func() {
		if a.tab == pageClipboard && (a.focusMode || a.search != "") {
			a.pill(c, "hint", "↑↓ 选择 · ↩ 复制并返回 · Esc 关闭", false)
		}
		if a.status != "" {
			a.statusToast(c)
		}
		if note := a.updateStatus(); note != "" {
			a.updatePill(c, note)
		}
		a.renameUndoPill(c)
		// The drop zone carries its own release hint in the module bar;
		// the 松开放入 capsule stays hidden while it is up.
		if (over || (a.dragging && !a.ownDrag)) && !a.drop.on {
			a.pill(c, "drop", "松开放入", false)
		}
	})
}

// pill is one bottom-center capsule: 11pt ink2 on fill2, 22pt tall.
func (a *shelf) pill(c *ui.Context, key, text string, isErr bool) {
	p := ui.Row(c.Key("pill-"+key)).Height(22).Padding(0, 12, 0, 12).Radius(11).
		AlignItems(ui.Center).Gap(6).Background(fill2)
	p.Children(func() {
		if isErr {
			ui.Box(c).Size(6, 6).Radius(3).Background(danger)
		}
		ui.Text(c, text).SingleLine().FontSize(11).TextColor(ink2)
	})
}

// statusToast renders a.status as a fading pill: an explicit statusUntil
// wins; a status without one (notices, errors) fades toastLife of seen
// time — the same clock dismissReadFeedback reads, so panel-closed time
// never eats a deadline-free toast. report()ed errors carry a red dot.
func (a *shelf) statusToast(c *ui.Context) {
	now := c.Now()
	seen := a.statusSeen
	if !a.statusSeenAt.IsZero() {
		seen += now.Sub(a.statusSeenAt)
	}
	until := a.statusUntil
	if until.IsZero() {
		until = now.Add(toastLife - seen)
	}
	remaining := until.Sub(now)
	if remaining <= 0 {
		// The monitor tick clears the text; this frame already shows
		// nothing rather than a stale pill.
		return
	}
	p := ui.Row(c.Key("pill-status")).Height(22).Padding(0, 12, 0, 12).Radius(11).
		AlignItems(ui.Center).Gap(6).Background(fill2)
	// 末段淡出：胶囊本身是透明 pill，不抢指针（toastLayer PassThrough）。
	k := p.AnimateTo("statusFade", float32(1), ui.Animation{Duration: toastFade})
	if remaining < toastFade {
		k = p.AnimateTo("statusFade", float32(0), ui.Animation{Duration: toastFade})
	}
	p.Opacity(k)
	p.Children(func() {
		if a.statusKind == statusErr {
			ui.Box(c).Size(6, 6).Radius(3).Background(danger)
		}
		ui.Text(c, a.status).SingleLine().FontSize(11).TextColor(ink)
	})
	// 让引擎在过期时刻再起一帧，tick 趁机清掉状态。
	c.After(remaining + 30*time.Millisecond)
}

// updatePill is the persistent update notice, transplanted from the old
// footer into a bottom pill; installing and relaunching stay user-chosen
// buttons (hence no PassThrough needed on the pill itself — its siblings
// still let clicks through).
func (a *shelf) updatePill(c *ui.Context, note string) {
	p := ui.Row(c.Key("pill-update")).Height(30).Padding(0, 8, 0, 14).Radius(15).
		AlignItems(ui.Center).Gap(8).Background(fill2)
	p.Children(func() {
		ui.Text(c, note).SingleLine().FontSize(11).TextColor(ink)
		switch {
		case a.updateInstalled:
			if ui.Button(c, "重新启动").Label("重新启动 Shelf 完成更新").Clicked() {
				relaunchApp()
			}
		case a.update != nil && !a.updateInstalling:
			if ui.Button(c, "安装更新").Label("安装更新 " + a.update.Version).Clicked() {
				a.installUpdate()
			}
		}
	})
}

// peekView draws the pill under the hardware notch while hovering: the
// now-playing player while a track is loaded, 「松开放入」 while a drag is
// on, otherwise the small downward chevron. A closed panel with something
// to report draws the status capsule instead (feedback.go). Like the
// player, the pill's content is laid out at its final size and uncovered
// by the growing shell; the glyphs sit in the band below the hardware
// notch, where the screen has pixels.
func (a *shelf) peekView(c *ui.Context, shellW, shellH, openK, peekK, shake float32, over, folding bool) {
	now := c.Now()
	if a.fb.retracting && !a.peeking && !(a.dragging && !a.ownDrag) {
		// The shell is still folding back from the capsule: only the frame
		// moves, as when the panel closes. The hover player is not drawn
		// over a capsule on its way out.
		return
	}
	// While the peek is folding (peek target 0) the shell keeps showing
	// the content it already had — folding is subtract-only. Re-evaluating
	// live state mid-fold would flash a loaded track's title inside the
	// shrinking shell.
	kind := a.fx.peekKind
	if !folding {
		switch {
		case a.capsuleUp(now):
			// The completion capsule outranks the player: a status that is
			// up is what the shell grew for (peekSize and desiredFrame
			// agree), and the player takes the shape back when the toast
			// is done.
			kind = peekCapsule
		case !over && a.np != nil && a.np.showsPlayer():
			kind = peekPlayer
		case a.pinReturning:
			// A pinned card held over the notch: the same pill shape as
			// the drop invite, reading 松开放回 (design §4).
			kind = peekReturn
		case a.dragging && !a.ownDrag:
			// Inside the notch band the drop pill invites the release;
			// outside it only the widened hint shows — never a glyph.
			if over || a.dropHot {
				kind = peekDrop
			} else {
				kind = peekHint
			}
		case over:
			kind = peekDrop
		default:
			kind = peekChevron
		}
		a.fx.peekKind = kind
	}
	switch kind {
	case peekCapsule:
		a.capsuleView(c, shellW, shellH, openK, peekK, shake, now)
		return
	case peekPlayer:
		if a.np != nil {
			pw, ph := a.playerSize()
			clip := ui.Column(c.Key("peek-clip")).Width(shellW).Height(shellH).Clip().AlignItems(ui.Center)
			drew := false
			clip.Children(func() { drew = a.np.peekPlayer(c, pw, ph, peekK) })
			if drew {
				return
			}
		}
		// The player's content is gone mid-fold (track ended): fold the
		// shell silently rather than swapping in another glyph.
		return
	case peekHint:
		// The drag hint draws nothing inside the shell: it only widens,
		// band-height, never a glyph below the notch.
		return
	}
	pw, ph := a.peekSize()
	if folding {
		// Folding keeps the content kind frozen but lays it out at the
		// shrinking shell's size: the frozen drop-pill size (a.fx.peekW/H)
		// would center the words below the clipping shell mid-fold, while
		// the live peekSize() answer (a loaded track's player size) pushes
		// them outside even sooner.
		pw, ph = shellW, shellH
	}
	notchH := float32(max(32, a.notchH))
	pill := ui.Column(c.Key("peek")).Width(shellW).Height(shellH).Clip().
		AlignItems(ui.Center).Label("展开 Shelf")
	pill.Children(func() {
		ui.Column(c.Key("peek-body")).Width(pw).Height(ph).Opacity(min(max(peekK, 0), 1)).Children(func() {
			ui.Box(c.Key("peek-notch")).Height(notchH)
			band := ui.Row(c.Key("peek-band")).FillWidth().Grow(1).Justify(ui.Center).AlignItems(ui.Center).Gap(6)
			band.Children(func() {
				if kind == peekDrop {
					ui.Icon(c, shelfSymbol).Size(14, 14).TextColor(ink)
					ui.Text(c, "松开放入").FontSize(12).FontWeight(600).SingleLine().TextColor(ink)
				} else if kind == peekReturn {
					ui.Icon(c, shelfSymbol).Size(14, 14).TextColor(ink)
					ui.Text(c, "松开放回").FontSize(12).FontWeight(600).SingleLine().TextColor(ink)
				} else {
					ui.Icon(c, chevronDownSym).Size(12, 12).TextColor(ink2)
				}
			})
		})
	})
	if pill.Clicked() {
		a.show(false)
	}
}

// capsuleView draws the completion capsule: the 16pt status dot over the
// toast's words, hung below the notch band. The capsule's outline is the
// shell path itself — the same black the hardware draws — so nothing here
// paints a second shape, a border, or a glow; the status colour stays
// inside the shell as the dot, which breathes while a job runs. The
// content is clipped to the shell while it grows.
func (a *shelf) capsuleView(c *ui.Context, shellW, shellH, _ /* openK */, peekK, shake float32, now time.Time) {
	f := &a.fb
	// The first drawn frame starts the capsule's clock: a status posted while
	// the panel was still closing is held for its full span from here.
	f.arm(now)

	baseW, baseH := float32(max(180, a.notchW)), float32(max(32, a.notchH))
	cw, ch := f.capsule(baseW, baseH)
	col := f.color()
	textW := cw - fbDot - fbGap - 2*fbPadX
	box := ui.Column(c.Key("fb-capsule")).Width(shellW).Height(shellH).AlignItems(ui.Center).Label("展开 Shelf · " + f.text)
	if shake != 0 {
		box.Translate(shake, 0)
	}
	box.Children(func() {
		// Everything inside the capsule is clipped to the shell actually
		// drawn this frame: the sliver grows out of the notch and uncovers
		// the words the same way the panel's shell uncovers its content.
		clip := ui.Column(c.Key("fb-clip")).Absolute().Top(0).Left(0).
			Width(shellW).Height(shellH).Clip().Justify(ui.End).AlignItems(ui.Center).PassThrough()
		clip.Children(func() {
			body := ui.Column(c.Key("fb-body")).Width(cw).Height(ch).Justify(ui.End).
				Opacity(min(max(peekK, 0), 1))
			body.Children(func() {
				ui.Row(c.Key("fb-msg")).Height(fbBody).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Gap(fbGap).Children(func() {
					dot := ui.Row(c.Key("fb-dot")).Size(fbDot, fbDot).Radius(fbDot / 2).Background(col).Center()
					if f.live {
						// A running job has no verdict yet: the dot
						// breathes instead of a light the hardware
						// would never show.
						dot.Opacity(f.accent(now, c.Preferences().ReduceMotion))
					}
					if g := f.glyph(); g != "" {
						dot.Children(func() { ui.Text(c, g).FontSize(10).FontWeight(800).TextColor(ui.Hex("#000000")) })
					}
					ui.Text(c.Key("fb-text"), f.text).SingleLine().FontSize(12).FontWeight(600).
						TextColor(ink).MaxWidth(textW)
				})
			})
		})
	})
	if box.Clicked() {
		a.show(false)
	}
}

// panelContentH is the expanded panel's desired content height (excluding
// the window frame margin): the ears, the activity strip when something
// is under way, the module bar, then the stage. Pages whose sharesHeight
// is true size the stage to the group's tallest, so switching between
// them never resizes the panel; any other page sizes it to its own
// stageH. While the drop zone is up its full height wins over the stage
// floor — the panel always contains the whole zone plus the bottom
// padding, so nothing is ever clipped mid-drag. Pages scroll past
// stageMaxH. Pure; unit-tested.
func (a *shelf) panelContentH() int {
	spacer, rowH, _ := a.earLayout()
	h := spacer + rowH + earBottomGap + moduleBarH + stageGap + panelPadBottom
	if len(a.liveActivities()) > 0 {
		h += liveH + liveGap
	}
	stage := min(max(a.currentStageH(), stageMinH), stageMaxH)
	if a.drop.on && a.drop.need > stage {
		stage = a.drop.need
	}
	h += stage
	if a.focusMode || a.search != "" {
		// The keyboard hint pill floats over the clipboard stage's foot.
		h += hintPillH
	}
	return h
}

// currentStageH is the stage height the panel sizes to now: the tallest
// stage of the height-sharing group while the current page is in it
// (switching pages inside the group keeps the height), otherwise the
// current page's own stageH — switching to or from such a page eases the
// panel to its own height over resizeAnimation.
func (a *shelf) currentStageH() int {
	if cur := a.currentPage(); cur.sharesHeight() {
		tallest := 0
		for _, p := range a.enabledPages() {
			if p.sharesHeight() {
				tallest = max(tallest, p.stageH())
			}
		}
		return tallest
	} else {
		return cur.stageH()
	}
}

// panelTargetH is panelContentH plus the 8pt the window keeps below the
// shell (shellSize's expH is panelH-8), clamped to the panel's size
// bound; the caller clamps it to the work area. Without the +8 the
// content column is 8pt short and the last stage row is clipped — an
// exact-fit page (the tools grid) shows it as a hard cut.
func (a *shelf) panelTargetH() int {
	return min(panelMaxH, a.panelContentH()+8)
}

// visibleItems is the item list the expanded panel shows and acts on: the
// current page's rows. The tray lists every item except ones hidden from
// it, the clipboard page the captures matching the search text; on both
// pinned items come first, stable within each group (otherwise newest
// first). Rows, selection ranges, menus and drag-outs all use it, so no
// action can reach a row the page is not showing and the display order is
// the selection order. Pages without an item list answer nil.
func (a *shelf) visibleItems() []store.Item { return a.itemsFor(a.currentPage().id()) }

// itemsFor is visibleItems for the given page (the page fading out after
// a switch draws its own rows). Each store-backed page owns its list
// (see pages.go); a page without one answers nil.
func (a *shelf) itemsFor(id pageID) []store.Item {
	switch p := a.page(id).(type) {
	case *trayPage:
		return p.items()
	case *clipboardPage:
		return p.items()
	case *toolsPage:
		// 工具页的动作卡作用于托盘选中项：它报托盘的列表，选择不随翻页丢。
		return a.itemsFor(pageTray)
	default:
		return nil
	}
}

// clipboardItemMatches reports whether a clipboard-list row matches the
// (already lowercased and trimmed) search text: text items by
// case-insensitive substring, images while the search is empty or by
// case-insensitive substring of their recognized text once recognition
// ran. The image text and the query go through the same whitespace fold
// the snippet uses, so a hit always renders a matching snippet.
func clipboardItemMatches(it store.Item, query string) bool {
	if query == "" {
		return true
	}
	if it.Kind == store.KindImage && it.OCRDone && it.OCRText != "" {
		return strings.Contains(strings.ToLower(ocrFold(it.OCRText)), ocrFold(query))
	}
	if it.Kind != store.KindText {
		return false
	}
	return strings.Contains(strings.ToLower(it.Text), query)
}

// setTab switches the panel's page. The selection follows: ids the new
// list does not show are dropped, so a ⇧-range never spans rows the user
// cannot see. Disabled or unregistered pages cannot be switched to.
func (a *shelf) setTab(id pageID) {
	p := a.page(id)
	if p == nil || !p.enabled() {
		return
	}
	cur := a.currentPage().id()
	if cur == id {
		a.tab = id // a.tab was stale; nothing to fade from
		return
	}
	a.tabFrom, a.tab = cur, id
	if cur == pageUsage && a.usage != nil { // 离开用量页即取消其后台工作
		a.usage.stopWork()
	}
	if cur == pageTools { // 离开工具页即退出编辑与目录
		a.resetToolsEdit()
	}
	if a.visible {
		a.tabAt = time.Now()
	}
	a.reveal = ""
	a.selection.keepExisting(a.visibleItems())
	a.syncWindowFrame()
	if a.win != nil {
		a.win.Invalidate()
	}
	a.syncCopyKey()
}

// moveClipSelection moves the clipboard tab's selection by delta rows
// (↑/↓ in the search field). With nothing selected yet, ↓ starts at the
// first row and ↑ at the last; the row is scrolled into view.
func (a *shelf) moveClipSelection(delta int) {
	items := a.visibleItems()
	if len(items) == 0 {
		return
	}
	next := 0
	if cur := indexOf(items, a.selection.anchor); cur >= 0 {
		next = min(max(cur+delta, 0), len(items)-1)
	} else if delta < 0 {
		next = len(items) - 1
	}
	a.selection.only(items[next].ID)
	a.reveal = items[next].ID
	if a.win != nil {
		a.win.Invalidate()
	}
}

// copyClipSelection is ⏎ on the clipboard tab: the selected item goes on
// the clipboard — text as text, an image as PNG — and the shelf collapses,
// so ⌘V pastes it into the app the user came from. Several selected texts
// are copied one per line.
func (a *shelf) copyClipSelection() {
	items := a.selection.chosen(a.visibleItems())
	if len(items) == 0 {
		return
	}
	if len(items) == 1 {
		if a.copyItem(items[0]) {
			a.hide(hideExplicit)
		}
		return
	}
	var texts []string
	for _, it := range items {
		if it.Kind != store.KindText {
			a.report(errors.New("这些项目不能一起复制；请只选一个"))
			return
		}
		texts = append(texts, it.Text)
	}
	if a.copyText(strings.Join(texts, "\n")) {
		a.hide(hideExplicit)
	}
}

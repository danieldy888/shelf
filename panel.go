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

// panelTab is one of the two lists the expanded panel shows.
const (
	tabTray      = iota // 托盘：全部内容
	tabClipboard        // 剪贴板：只显示剪贴板收录的文字与图片
)

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
)

// contentFade is the expanded content's opacity at open progress k: it
// stays transparent while the shell is still a notch-sized sliver and
// fades in over the last 60% of the growth, so the content arrives with
// the shell instead of flashing through it.
func contentFade(k float32) float32 {
	return ui.EaseOut(min(max((k-0.4)/0.6, 0), 1))
}

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
	if c.Shortcut(0, ui.KeyEscape) {
		a.escape()
	}
	c.OnShortcut(ui.Cmd, ui.KeyQ, mygo.App.Quit)
	// 剪贴板标签的回车：焦点在搜索框时由输入框的 OnTextKey 处理（输入框先
	// 拿到方向键和回车），焦点在别处时在这里兜底。Esc 无此需要：输入框不
	// 消费它，窗口顶部注册的 Shortcut 已经覆盖所有焦点位置。
	if a.visible && a.tab == tabClipboard {
		if c.Shortcut(0, ui.KeyEnter) {
			a.copyClipSelection()
		}
	}
	c.SetTheme(panelTheme)
	// 常驻窗口是 Transparent 的：mygo 根元素默认画满主题背景，必须改成
	// 全透明，否则整个窗口会一直盖着一块主题色灰板。
	c.Root().Background(ui.Transparent)
	opts := transfer.DropOptions{Formats: []transfer.Format{transfer.FileList, transfer.URIList, transfer.PNG, transfer.Text}, Operations: transfer.Copy}
	openTarget, peekTarget := float32(0), float32(0)
	if a.visible {
		openTarget = 1
	}
	if a.peeking || a.visible {
		peekTarget = 1
	}
	// 真实窗口的彻底空闲态：什么都不画，常驻窗口完全不可见。
	idle := a.win != nil && !a.visible && a.progress <= 0.01 && !a.peeking && a.peekProgress <= 0.01
	// Keep one native drop target across all states. The transparent root does
	// not paint the theme background; clipping contains every animation frame.
	zone := ui.Column(c.Key("shelf")).Fill().AlignItems(ui.Center).Clip()
	// The shell is drawn from values animated per display frame; the 30ms
	// tick's progress/peekProgress only decide state (window frame, mouse
	// pass-through). Headless tests draw the final shape directly.
	openK, peekK := a.progress, a.peekProgress
	if a.win == nil && !a.peeking {
		openK = 1
	}
	if a.win != nil {
		openK = zone.AnimateTo("open", openTarget, ui.Animation{Duration: panelAnimation, Ease: ui.EaseOut})
		peekK = zone.AnimateTo("peek", peekTarget, ui.Animation{Duration: peekAnimation, Ease: ui.EaseOut})
	}
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
		if accepts {
			if drop, ok := ui.DropData(zone, opts); ok {
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
	closing := !expanded && openK > 0.01
	// The panel's height eases between contents (tab switch, rows coming
	// and going); the content itself is laid out at the target height.
	// While closing, the shell keeps the height it was showing.
	targetH := float32(a.panelH())
	shownH := targetH
	if a.win != nil && (a.visible || a.progress > 0.01) {
		if closing && a.shownH > 0 {
			targetH = a.shownH
		}
		shownH = zone.AnimateTo(panelHeightKey{a.openGen}, targetH, ui.Animation{Duration: resizeAnimation, Ease: ui.EaseInOut})
	}
	a.shownH = shownH
	// The hover shape eases between its forms (chevron, drop pill,
	// player) when it changes while shown, e.g. a drag starting.
	peekW, peekH := a.peekSize()
	if a.win != nil {
		morph := ui.Animation{Duration: peekAnimation, Ease: ui.EaseOut}
		peekW = zone.AnimateTo("peek-w", peekW, morph)
		peekH = zone.AnimateTo("peek-h", peekH, morph)
	}
	// 刘海壳尺寸在三态间插值：硬件刘海 ←→ 探头胶囊 ←→ 完整面板
	// （shellSize 是几何的唯一来源）。
	shellW, shellH := a.shellSize(openK, peekK, peekW, peekH, shownH)
	// 窗口贴在屏幕 Y=0（展开=面板全幅，其余=刘海顶边窄条，见
	// syncWindowFrame）：壳水平居中、顶端贴齐窗口顶边，探头才能从硬件
	// 刘海里长出来，而不是浮在菜单栏下方。
	zone.Draw(func(p *ui.Painter, r ui.Rect) {
		x0 := (r.W - shellW) / 2
		x1 := x0 + shellW
		y1 := shellH
		earR := float32(10)
		// 底部圆角：探头胶囊半圆 → 展开 30pt。
		botR := min(shellH/2, 12+18*openK)
		var path ui.Path
		path.MoveTo(x0-earR, 0)
		path.QuadTo(x0, 0, x0, earR)
		path.LineTo(x0, y1-botR)
		path.QuadTo(x0, y1, x0+botR, y1)
		path.LineTo(x1-botR, y1)
		path.QuadTo(x1, y1, x1, y1-botR)
		path.LineTo(x1, earR)
		path.QuadTo(x1, 0, x1+earR, 0)
		path.Close()
		p.FillPath(&path, ui.Hex("#000000"))
	})
	zone.Children(func() {
		if !expanded {
			if closing || !peeked {
				// 收起途中只有壳缩回刘海，不画内容。
				return
			}
			a.peekView(c, shellW, shellH, peekK, over)
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
				content.Opacity(contentFade(openK))
			}
			content.Children(func() {
				a.earBar(c)
				a.activityStrip(c)
				a.tabBody(c, over)
				a.toastLayer(c, over)
			})
		})
	})
	if !accepts {
		return
	}
	if drop, ok := ui.DropData(zone, opts); ok {
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
	if a.tab < a.tabFrom {
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
		body.Children(func() { a.panelBody(c, over, a.tab, true) })
		if t < tabOutShare && a.tabFrom != a.tab {
			// The outgoing page is only a picture: it takes no pointer
			// and never the keyboard.
			old := ui.Column(c.Key("body-out")).Absolute().Left(0).Right(0).Top(0).Bottom(0).
				PassThrough().Opacity(1 - ui.EaseIn(t/tabOutShare))
			old.Children(func() { a.panelBody(c, false, a.tabFrom, false) })
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
	var navs [2]ui.Element
	bar.Children(func() {
		// Both ears grow from a zero basis, so they split the width evenly
		// and the housing gap stays centred on the notch.
		ui.Row(c.Key("ear-left")).Grow(1).MinWidth(0).Gap(earBtnGap).AlignItems(ui.Center).Children(func() {
			navs[tabTray] = a.navButton(c, "tab-tray", trayNavSymbol, "托盘", tabTray)
			navs[tabClipboard] = a.navButton(c, "tab-clipboard", clipNavSymbol, "剪贴板", tabClipboard)
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
	r := navs[a.tab].LayoutBounds()
	if r.W <= 0 {
		navs[a.tab].Background(fill2)
		return
	}
	x := r.X
	if a.win != nil {
		x = bar.AnimateTo("tab-x", r.X, ui.Animation{Duration: tabAnimation, Ease: ui.EaseInOut})
	}
	bar.Draw(func(p *ui.Painter, _ ui.Rect) {
		p.Fill(ui.Rect{X: x, Y: r.Y, W: r.W, H: r.H}, fill2, r.H/2)
	})
}

// navButton is one page icon of the left ear: ink when active (its capsule
// is earBar's), ink2 with a fill1 hover otherwise. The page's name is its
// tooltip and accessible label; the module bar spells it out below.
func (a *shelf) navButton(c *ui.Context, key string, icon *ui.SVG, label string, tab int) ui.Element {
	on := a.tab == tab
	b := ui.ButtonBase(c.Key(key)).Size(earBtn, earBtn).Radius(earBtn / 2).Center().Label(label).Tooltip(label)
	if !on && b.Hovered() {
		b.Background(fill1)
	}
	b.Children(func() {
		g := ui.Icon(c, icon).Size(15, 15)
		if on || b.Hovered() {
			g.TextColor(ink)
		} else {
			g.TextColor(ink2)
		}
	})
	if b.Clicked() {
		a.setTab(tab)
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
// the tray's thumbnail grid or the clipboard's search field and list. live
// is false for the page fading out after a switch, which is drawn only.
func (a *shelf) panelBody(c *ui.Context, over bool, tab int, live bool) {
	list := a.itemsFor(tab)
	a.moduleBar(c, list, tab, live)
	if tab == tabClipboard {
		a.clipboardBody(c, list, live)
		return
	}
	if len(list) == 0 {
		a.emptyState(c, tab)
		return
	}
	ui.Scroll(c.Key("items")).Grow(1).MinHeight(0).Children(func() {
		ui.Grid(c.Key("grid")).ColumnTracks(ui.Fr(1), ui.Fr(1), ui.Fr(1), ui.Fr(1), ui.Fr(1)).
			Gap(gridGap).FillWidth().Children(func() {
			// 快照：右键菜单或拖入回调可改变原切片。
			for _, item := range list {
				a.itemTile(c, item)
			}
		})
	})
}

// moduleBar is the page's own 28pt row: its name and count on the left;
// on the right the tray's multi-select hint and the page's 清空.
func (a *shelf) moduleBar(c *ui.Context, list []store.Item, tab int, live bool) {
	title, count := "托盘", fmt.Sprintf("%d 项", len(list))
	if tab == tabClipboard {
		title, count = "剪贴板", fmt.Sprintf("%d 条", len(list))
	}
	ui.Row(c.Key("module-bar")).Height(moduleBarH).AlignItems(ui.Center).FillWidth().
		Padding(0, 0, 0, 2).Margin(0, 0, stageGap, 0).Children(func() {
		ui.Text(c, title).SingleLine().FontSize(13).FontWeight(600).TextColor(ink)
		if len(list) > 0 {
			ui.Text(c, count).SingleLine().FontSize(11).FontWeight(600).TextColor(ink3).Margin(0, 0, 0, 8)
		}
		ui.Box(c).Grow(1)
		if tab == tabTray && len(list) > 0 {
			hint := "⌘ / ⇧ 多选 · 空格预览"
			if n := a.selection.count(); n > 1 {
				hint = fmt.Sprintf("已选 %d 项 · 一起拖出", n)
			}
			ui.Text(c, hint).SingleLine().FontSize(11).FontWeight(600).TextColor(ink3).Margin(0, 6, 0, 0)
		}
		if gone := a.clearable(list, tab); len(gone) > 0 {
			if a.iconButton(c, "clear", trashSymbol, "清空").Clicked() && live {
				a.remove(gone)
			}
		}
	})
}

// clearable is what the module bar's 清空 removes: the unpinned rows the
// page shows, so the tray's button leaves the clipboard history alone and
// the other way round. While a search narrows the clipboard list there is
// nothing to clear — emptying only the matches would surprise.
func (a *shelf) clearable(list []store.Item, tab int) []store.Item {
	if tab == tabClipboard && strings.TrimSpace(a.search) != "" {
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

// clipboardBody is the clipboard tab: the 32pt search field, then the
// capture rows (source-app icon, text, 「应用 · 相对时间」).
func (a *shelf) clipboardBody(c *ui.Context, list []store.Item, live bool) {
	a.searchField(c, live)
	if len(list) == 0 {
		a.emptyState(c, tabClipboard)
		return
	}
	ui.Scroll(c.Key("items")).Grow(1).MinHeight(0).Margin(clipListTop, 0, 0, 0).Children(func() {
		ui.Column(c.Key("clip-list")).Gap(clipRowGap).FillWidth().Children(func() {
			for _, item := range list {
				a.clipRow(c, item)
			}
		})
	})
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
// icon, a medium ink2 title and an ink3 note line.
func (a *shelf) emptyState(c *ui.Context, tab int) {
	icon, title, hint := shelfSymbol, "把文件拖到这里", "文件、文件夹、文字和图片"
	switch {
	case tab == tabClipboard && a.search != "":
		icon, title, hint = searchSymbol, "没有匹配的内容", "换个关键词，或按 Esc 清空搜索"
	case tab == tabClipboard && !a.settings.ClipboardHistory:
		icon, title, hint = textSymbol, "剪贴板记录已关闭", "在设置里打开「记录复制的内容」"
	case tab == tabClipboard:
		icon, title, hint = textSymbol, "还没有剪贴板记录", "在其他应用按 ⌘C，文字和截图会自动收好"
	case a.settings.ClipboardHistory:
		hint += "\n复制的文字与截图也会自动收好"
	}
	ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
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
		if a.tab == tabClipboard && (a.focusMode || a.search != "") {
			a.pill(c, "hint", "↑↓ 选择 · ↩ 复制并返回 · Esc 关闭", false)
		}
		if a.status != "" {
			a.statusToast(c)
		}
		if note := a.updateStatus(); note != "" {
			a.updatePill(c, note)
		}
		if over || a.dragging {
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

// statusToast renders a.status as a fading pill and arms its expiry: an
// explicit statusUntil wins; a status without one (notices, errors) fades
// toastLife after it first shows. report()ed errors carry a red dot.
func (a *shelf) statusToast(c *ui.Context) {
	now := c.Now()
	if a.statusShown.IsZero() {
		a.statusShown = now
	}
	until := a.statusUntil
	if until.IsZero() {
		until = a.statusShown.Add(toastLife)
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
		if a.statusErr {
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
// on, otherwise the small downward chevron. Like the player, the pill's
// content is laid out at its final size and uncovered by the growing
// shell; the glyphs sit in the band below the hardware notch, where the
// screen has pixels.
func (a *shelf) peekView(c *ui.Context, shellW, shellH, peekK float32, over bool) {
	if !over && a.np != nil && a.np.showsPlayer() {
		pw, ph := a.playerSize()
		clip := ui.Column(c.Key("peek-clip")).Width(shellW).Height(shellH).Clip().AlignItems(ui.Center)
		drew := false
		clip.Children(func() { drew = a.np.peekPlayer(c, pw, ph, peekK) })
		if drew {
			return
		}
	}
	pw, ph := a.peekSize()
	notchH := float32(max(32, a.notchH))
	pill := ui.Column(c.Key("peek")).Width(shellW).Height(shellH).Clip().
		AlignItems(ui.Center).Label("展开 Shelf")
	pill.Children(func() {
		ui.Column(c.Key("peek-body")).Width(pw).Height(ph).Children(func() {
			ui.Box(c.Key("peek-notch")).Height(notchH)
			band := ui.Row(c.Key("peek-band")).FillWidth().Grow(1).Justify(ui.Center).AlignItems(ui.Center).Gap(6)
			band.Children(func() {
				if over || a.dragging {
					ui.Icon(c, shelfSymbol).Size(14, 14).TextColor(ink)
					ui.Text(c, "松开放入").FontSize(12).FontWeight(600).SingleLine().TextColor(ink)
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

// panelContentH is the expanded panel's desired content height (excluding
// the window frame margin): the ears, the activity strip when something
// is under way, the module bar, then the stage. The stage is as tall as
// the tallest page needs, clamped between stageMinH and stageMaxH (each
// page scrolls past that), so switching pages never resizes the panel —
// only content coming and going does. Pure; unit-tested.
func (a *shelf) panelContentH() int {
	spacer, rowH, _ := a.earLayout()
	h := spacer + rowH + earBottomGap + moduleBarH + stageGap + panelPadBottom
	if len(a.liveActivities()) > 0 {
		h += liveH + liveGap
	}
	stage := max(a.stageH(tabTray), a.stageH(tabClipboard))
	h += min(max(stage, stageMinH), stageMaxH)
	if a.focusMode || a.search != "" {
		// The keyboard hint pill floats over the clipboard stage's foot.
		h += hintPillH
	}
	return h
}

// stageH is the height a page's stage content wants, before clamping.
func (a *shelf) stageH(tab int) int {
	list := a.itemsFor(tab)
	switch {
	case tab == tabClipboard:
		h := clipSearchH
		if len(list) > 0 {
			h += clipListTop + len(list)*(clipRowH+clipRowGap) - clipRowGap
		}
		return h
	case len(list) > 0:
		rows := (len(list) + gridCols - 1) / gridCols
		return rows*tileH + (rows-1)*gridGap
	}
	return 0
}

// panelTargetH is panelContentH clamped to the panel's size bound; the
// caller clamps it to the work area.
func (a *shelf) panelTargetH() int {
	return min(panelMaxH, a.panelContentH())
}

// visibleItems is the list the expanded panel shows and acts on: the tray
// tab lists every item except ones hidden from it, and the clipboard tab
// lists clipboard captures that match the search text; on both tabs
// pinned items come first, stable within each group (otherwise newest
// first). Rows, selection ranges, menus and drag-outs all use it, so no
// action can reach a row the tab is not showing and the display order is
// the selection order.
func (a *shelf) visibleItems() []store.Item { return a.itemsFor(a.tab) }

// itemsFor is visibleItems for the given page (the page fading out after
// a switch draws its own rows).
func (a *shelf) itemsFor(tab int) []store.Item {
	if a.store == nil {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(a.search))
	var pinned, rest []store.Item
	for _, it := range a.store.Items {
		if tab == tabClipboard && (it.Source != store.SourceClipboard || !clipboardItemMatches(it, query)) {
			continue
		}
		// 托盘页的移除对剪贴板收录只做到「不在托盘显示」：标了 OffTray 的
		// 项目仍留在剪贴板页，托盘不再列出。
		if tab == tabTray && (it.OffTray || a.isLeaving(it)) {
			continue
		}
		// 设计决定：剪贴板收录的文字只属于剪贴板标签；托盘放文件、
		// 文件夹、图片（含剪贴板截图）以及拖入/服务收录/置顶的文字。
		if tab == tabTray && it.Source == store.SourceClipboard &&
			it.Kind == store.KindText && !it.Pinned {
			continue
		}
		if it.Pinned {
			pinned = append(pinned, it)
		} else {
			rest = append(rest, it)
		}
	}
	return append(pinned, rest...)
}

// clipboardItemMatches reports whether a clipboard-list row matches the
// (already lowercased) search text: text items by case-insensitive
// substring, images only while the search is empty.
func clipboardItemMatches(it store.Item, query string) bool {
	if query == "" {
		return true
	}
	if it.Kind != store.KindText {
		return false
	}
	return strings.Contains(strings.ToLower(it.Text), query)
}

// setTab switches the panel's list. The selection follows: ids the new
// list does not show are dropped, so a ⇧-range never spans rows the user
// cannot see.
func (a *shelf) setTab(tab int) {
	if a.tab == tab {
		return
	}
	a.tabFrom, a.tab = a.tab, tab
	if a.visible {
		a.tabAt = time.Now()
	}
	a.reveal = ""
	a.selection.keepExisting(a.visibleItems())
	a.syncWindowFrame()
	if a.win != nil {
		a.win.Invalidate()
	}
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

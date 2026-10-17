//go:build darwin

package main

import (
	"errors"
	"fmt"
	"slices"
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
	// 顶栏图标按钮：清空、设置、收起。
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
	t.FontSize = 13
	return t
}()

// 展开面板布局常量（对应 DESIGN.md「展开面板」；headerTop 是刘海下沿
// 到顶栏的 8pt 间隔）。
const (
	panelPadX      = 16                      // 左右内边距
	panelPadBottom = 16                      // 底部内边距
	headerTopGap   = 8                       // 刘海下沿到顶栏
	headerH        = 28                      // 顶栏高
	panelCardTop   = 14                      // 正在播放卡片上边距（margin-top:14）
	panelCardH     = 94                      // 卡片块：56 内容 + 10 上距 + 28 进度行
	panelSecTop    = 18                      // 区块标题上边距
	panelSecBottom = 8                       // 区块标题到网格
	panelSecLineH  = 15                      // 区块标题行高（11pt）
	gridCols       = 5                       // 托盘网格列数
	gridGap        = 4                       // 格子间距
	tileH          = 100                     // 格子高
	clipSearchTop  = 14                      // 搜索框上边距
	clipSearchH    = 32                      // 搜索框高
	clipListTop    = 8                       // 列表上边距
	clipRowH       = 46                      // 剪贴板行高
	clipRowGap     = 2                       // 行间距
	hintPillH      = 34                      // 提示胶囊：12 上距 + 22 高
	panelMinH      = 160                     // 空态最小面板高
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

// tabFade is the incoming tab body's progress, 0→1 over tabAnimation
// after a switch; 1 when no switch is under way.
func (a *shelf) tabFade(now time.Time) float32 {
	if a.tabAt.IsZero() {
		return 1
	}
	t := float32(now.Sub(a.tabAt)) / float32(tabAnimation)
	return ui.EaseOut(min(max(t, 0), 1))
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
	_, over := ui.DataDragOver(zone, opts)
	if idle {
		// 空闲态收到内容只入库不展开：那时窗口本就不显示，也无从接受拖放。
		if drop, ok := ui.DropData(zone, opts); ok {
			a.receive(drop.Data)
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
				// 刘海黑壳本身占据顶部；内容顶栏从刘海下沿 +8 开始。
				ui.Box(c.Key("notch-spacer")).Height(float32(max(0, a.topInset+headerTopGap)))
				a.panelHeader(c)
				a.tabBody(c, over)
				a.toastLayer(c, over)
			})
		})
	})
	if drop, ok := ui.DropData(zone, opts); ok {
		a.receive(drop.Data)
		if !a.visible {
			a.show(false)
		}
	}
}

// tabBody holds the current tab's content. After a switch it fades in and
// settles from a few points toward its tab's side of the header. This is
// driven per frame rather than by an element Transition: a switch that
// changes the panel's height resizes the window, and mygo does not
// animate a resized frame's transitions.
func (a *shelf) tabBody(c *ui.Context, over bool) {
	body := ui.Column(c.Key("body")).FillWidth().Grow(1).MinHeight(0)
	if a.win != nil {
		if k := a.tabFade(c.Now()); k < 1 {
			dx := float32(-10)
			if a.tab == tabClipboard {
				dx = 10
			}
			if c.Preferences().ReduceMotion {
				dx = 0
			}
			body.Opacity(k).Translate(dx*(1-k), 0)
			c.AnimationFrame()
		}
	}
	body.Children(func() { a.panelBody(c, over) })
}

// panelHeader is the 28pt top bar of the expanded panel: text tabs on the
// left, icon buttons on the right — clear, settings, collapse. The active
// tab's capsule is drawn by the bar itself and glides to the other tab on
// a switch, instead of jumping.
func (a *shelf) panelHeader(c *ui.Context) {
	hdr := ui.Row(c.Key("hdr")).Height(headerH).AlignItems(ui.Center).Gap(6).FillWidth()
	var tabs [2]ui.Element
	hdr.Children(func() {
		tabs[tabTray] = a.panelTab(c, "tab-tray", "托盘", tabTray)
		tabs[tabClipboard] = a.panelTab(c, "tab-clipboard", "剪贴板", tabClipboard)
		ui.Box(c).Grow(1)
		hasUnpinned := a.store != nil && slices.ContainsFunc(a.store.Items, func(it store.Item) bool { return !it.Pinned })
		if hasUnpinned {
			if a.headerIcon(c, "clear", trashSymbol, "清空").Clicked() {
				if err := a.store.Clear(); err != nil {
					// The list may already be empty and saved even
					// when a stored image could not be deleted.
					a.report(err)
				}
				// 置顶项连同受管图片留在列表里：作废被清掉的
				// 缩略图、把选中剪到仍在的行，再重绘。
				a.dropStaleThumbnails()
				a.selection.keepExisting(a.visibleItems())
				if a.win != nil {
					a.win.Invalidate()
				}
			}
		}
		if a.headerIcon(c, "settings", gearSymbol, "设置").Clicked() {
			a.openSettings()
		}
		if a.headerIcon(c, "collapse", chevronUpSymbol, "收起").Clicked() {
			a.hide(hideExplicit)
		}
	})
	// The capsule follows the active tab's box from the last frame; the
	// tab paints its own capsule until that box is known (first frame).
	r := tabs[a.tab].LayoutBounds()
	if r.W <= 0 {
		tabs[a.tab].Background(fill2)
		return
	}
	x, w := r.X, r.W
	if a.win != nil {
		glide := ui.Animation{Duration: tabAnimation, Ease: ui.EaseInOut}
		x = hdr.AnimateTo("tab-x", r.X, glide)
		w = hdr.AnimateTo("tab-w", r.W, glide)
	}
	hdr.Draw(func(p *ui.Painter, _ ui.Rect) {
		p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, fill2, r.H/2)
	})
}

// panelTab is one text tab of the header: ink when active (its capsule is
// panelHeader's), ink2 with a fill1 hover otherwise.
func (a *shelf) panelTab(c *ui.Context, key, label string, tab int) ui.Element {
	on := a.tab == tab
	b := ui.ButtonBase(c.Key(key)).Height(26).Padding(0, 11, 0, 11).Radius(13).Label(label)
	if !on && b.Hovered() {
		b.Background(fill1)
	}
	b.Children(func() {
		text := ui.Text(c, label).SingleLine().FontSize(13).FontWeight(600)
		if on {
			text.TextColor(ink)
		} else {
			text.TextColor(ink2)
		}
	})
	if b.Clicked() {
		a.setTab(tab)
	}
	return b
}

// headerIcon is one 26×26 circular icon button of the header.
func (a *shelf) headerIcon(c *ui.Context, key string, icon *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(26, 26).Radius(13).Center().Label(label)
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

// panelBody renders the part below the header for the current tab: the
// tray's now-playing card, section line and thumbnail grid, or the
// clipboard tab's search field and list.
func (a *shelf) panelBody(c *ui.Context, over bool) {
	list := a.visibleItems()
	if a.tab == tabTray {
		// 托盘标签列表上方：第一个报告已绘制的 panelCard（如正在播放）。
		for _, m := range a.modules {
			if card, ok := m.(panelCard); ok && card.card(c) {
				break
			}
		}
		// 区块标题行只在有内容时出现。
		if len(list) > 0 {
			ui.Row(c.Key("sec")).Height(panelSecLineH).AlignItems(ui.Center).FillWidth().
				Margin(panelSecTop, 2, panelSecBottom, 2).Children(func() {
				ui.Text(c, fmt.Sprintf("%d 项", len(list))).SingleLine().FontSize(11).FontWeight(600).TextColor(ink3).Grow(1)
				hint := "⌘ / ⇧ 多选 · 空格预览"
				if n := a.selection.count(); n > 1 {
					hint = fmt.Sprintf("已选 %d 项 · 一起拖出", n)
				}
				ui.Text(c, hint).SingleLine().FontSize(11).FontWeight(600).TextColor(ink3)
			})
		}
	}
	if a.tab == tabClipboard {
		a.clipboardBody(c, list)
		return
	}
	if len(list) == 0 {
		a.emptyState(c)
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

// clipboardBody is the clipboard tab: the 32pt search field, then the
// capture rows (source-app icon, text, 「应用 · 相对时间」).
func (a *shelf) clipboardBody(c *ui.Context, list []store.Item) {
	a.searchField(c)
	if len(list) == 0 {
		a.emptyState(c)
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
func (a *shelf) searchField(c *ui.Context) {
	wrap := ui.Row(c.Key("clip-search")).Height(clipSearchH).Padding(0, 10, 0, 10).Gap(7).
		AlignItems(ui.Center).Radius(10).Background(fill1).
		Margin(clipSearchTop, 0, 0, 0)
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

// emptyState is what the panel shows when the current tab has no rows: a
// centered ink3 icon, a medium ink2 title and an ink3 note line.
func (a *shelf) emptyState(c *ui.Context) {
	icon, title, hint := shelfSymbol, "把文件拖到这里", "文件、文件夹、文字和图片"
	switch {
	case a.tab == tabClipboard && a.search != "":
		icon, title, hint = searchSymbol, "没有匹配的内容", "换个关键词，或按 Esc 清空搜索"
	case a.tab == tabClipboard && !a.settings.ClipboardHistory:
		icon, title, hint = textSymbol, "剪贴板记录已关闭", "在设置里打开「记录复制的内容」"
	case a.tab == tabClipboard:
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
// the window frame margin), computed so the shell hugs its content like
// the mock does and scrolls only past panelMaxH. Pure; unit-tested.
func (a *shelf) panelContentH() int {
	top := max(0, a.topInset+headerTopGap) + headerH + panelPadBottom
	list := a.visibleItems()
	switch a.tab {
	case tabClipboard:
		h := top + clipSearchTop + clipSearchH
		if len(list) == 0 {
			return max(panelMinH, h+96)
		}
		h += clipListTop + len(list)*(clipRowH+clipRowGap) - clipRowGap
		if a.focusMode || a.search != "" {
			h += hintPillH
		}
		return h
	default:
		h := top
		if len(list) > 0 {
			// 区块标题行只在有内容时存在（与 panelBody 一致）。
			h += panelSecTop + panelSecLineH + panelSecBottom
		}
		if a.np != nil && a.np.hasTrack() {
			h += panelCardTop + panelCardH
		}
		if len(list) == 0 {
			return max(panelMinH, h+96)
		}
		rows := (len(list) + gridCols - 1) / gridCols
		return h + rows*tileH + (rows-1)*gridGap
	}
}

// panelTargetH is panelContentH clamped to the panel's size bounds; the
// caller clamps it to the work area.
func (a *shelf) panelTargetH() int {
	return min(panelMaxH, max(panelMinH, a.panelContentH()))
}

// visibleItems is the list the expanded panel shows and acts on: the tray
// tab lists every item and the clipboard tab lists clipboard captures that
// match the search text; on both tabs pinned items come first, stable
// within each group (otherwise newest first). Rows, selection ranges,
// menus and drag-outs all use it, so no action can reach a row the tab is
// not showing and the display order is the selection order.
func (a *shelf) visibleItems() []store.Item {
	if a.store == nil {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(a.search))
	var pinned, rest []store.Item
	for _, it := range a.store.Items {
		if a.tab == tabClipboard && (it.Source != store.SourceClipboard || !clipboardItemMatches(it, query)) {
			continue
		}
		// 设计决定：剪贴板收录的文字只属于剪贴板标签；托盘放文件、
		// 文件夹、图片（含剪贴板截图）以及拖入/服务收录/置顶的文字。
		if a.tab == tabTray && it.Source == store.SourceClipboard &&
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
	a.tab = tab
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

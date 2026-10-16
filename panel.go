//go:build darwin

package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

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
	// 图钉：置顶标记（列表里的行首小图标）。
	pinSymbol = symbol(`<path d="M12 17v4M8 4h8l-1 6 3 3v1H6v-1l3-3z"/>`)
)

// panelTab is one of the two lists the expanded panel shows.
const (
	tabTray      = iota // 托盘：全部内容
	tabClipboard        // 剪贴板：只显示剪贴板收录的文字与图片
)

func symbol(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

// 展开态配色：纯黑刘海壳上只放深色 UI，让托盘看起来像一整块硬件黑岛。
var (
	textPrimary  = ui.Hex("#F3F4F6")
	textMuted    = ui.Hex("#9CA3AF")
	cardSurface  = ui.RGBA(255, 255, 255, 0.08)
	cardHover    = ui.RGBA(255, 255, 255, 0.14)
	cardSelected = ui.RGBA(10, 132, 255, 0.45)
	hairline     = ui.RGBA(255, 255, 255, 0.10)
)

// view 把刘海画成三态形态：收起（窗口常驻但完全不绘制）、探头（硬件刘海
// 向下鼓出小胶囊）、展开（纯黑面板从刘海里长出来）。单元测试里 a.win 为
// nil，直接按完整展开态渲染。
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
	t := c.Theme()
	// 常驻窗口是 Transparent 的：mygo 根元素默认画满主题背景，必须改成
	// 全透明，否则整个 440×400 窗口会一直盖着一块主题色灰板。
	c.Root().Background(ui.Transparent)
	opts := transfer.DropOptions{Formats: []transfer.Format{transfer.FileList, transfer.URIList, transfer.PNG, transfer.Text}, Operations: transfer.Copy}
	openK := a.progress
	if a.win == nil && !a.peeking {
		openK = 1
	}
	// 真实窗口的彻底空闲态：什么都不画，常驻窗口完全不可见。
	idle := a.win != nil && !a.visible && a.progress <= 0.01 && !a.peeking && a.peekProgress <= 0.01
	island := idle && a.islandShown()
	// Keep one native drop target across all states. The transparent root does
	// not paint the theme background; clipping contains every animation frame.
	zone := ui.Column(c.Key("shelf")).Fill().AlignItems(ui.Center).Clip()
	_, over := ui.DataDragOver(zone, opts)
	if idle && !island {
		// 空闲态收到内容只入库不展开：那时窗口本就不显示，也无从接受拖放。
		if drop, ok := ui.DropData(zone, opts); ok {
			a.receive(drop.Data)
		}
		return
	}
	peeked := a.peeking || a.peekProgress > 0.01
	expanded := a.win == nil && !a.peeking && !island || a.visible || openK >= 0.15
	// 刘海壳尺寸在三态间插值：硬件刘海 ←→ 探头胶囊 ←→ 完整面板；
	// 岛翼在探头/展开时收回壳内（shellSize 是几何的唯一来源）。
	shellW, shellH := a.shellSize(openK)
	// 窗口贴在屏幕 Y=0（展开=面板全幅，其余=刘海顶边窄条，见
	// syncWindowFrame）：壳水平居中、顶端贴齐窗口顶边，探头才能从硬件
	// 刘海里长出来，而不是浮在菜单栏下方。
	zone.Draw(func(p *ui.Painter, r ui.Rect) {
		x0 := (r.W - shellW) / 2
		x1 := x0 + shellW
		y1 := shellH
		earR := float32(10)
		botR := 14 + 10*openK
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
			if !peeked {
				if island || a.islandK > 0.01 {
					// 刘海岛：黑色壳向左/右展开成翼，内容是 leading 图标、
					// 右侧文字与/或电平条；点击展开（见 mouseDown）。
					a.islandView(c, shellW)
				}
				// 收起动画尾段：壳继续缩回硬件刘海，不显示内容。
				return
			}
			// 探头胶囊：显示底部提示，单击展开；拖放上来直接接收。
			pill := ui.Column(c.Key("peek")).Width(shellW).Height(shellH).Padding(0, 8, 0, 8).
				Justify(ui.End).AlignItems(ui.Center).Label("展开 Shelf")
			hint := "点击展开 Shelf"
			if over || a.dragging {
				hint = "松开放入托盘"
			}
			pill.Children(func() {
				ui.Text(c, hint).FontSize(11).SingleLine().TextColor(textMuted).Padding(0, 0, 7, 0)
			})
			if pill.Clicked() {
				a.show(false)
			}
			return
		}
		content := ui.Column(c.Key("panel")).Width(shellW).Height(shellH).Clip().
			Padding(0, 16, 10, 16).Gap(10)
		content.Children(func() {
			// 刘海黑壳本身占据顶部；内容顶栏从刘海下沿以下开始。
			ui.Box(c.Key("notch-spacer")).Height(float32(max(0, a.topInset)))
			ui.Row(c).Gap(10).Children(func() {
				ui.Icon(c, shelfSymbol).Size(25, 25).TextColor(t.Accent)
				ui.Text(c, appName).FontSize(21).FontWeight(650).TextColor(textPrimary).Grow(1)
				if a.store != nil && slices.ContainsFunc(a.store.Items, func(it store.Item) bool { return !it.Pinned }) {
					clear := ui.ButtonBase(c.Key("clear")).Padding(6, 12).Radius(7).Label("清空托盘")
					if clear.Hovered() {
						clear.Background(cardHover)
					} else {
						clear.Background(cardSurface)
					}
					clear.Children(func() {
						ui.Text(c, "清空").SingleLine().FontSize(12).TextColor(textPrimary)
					})
					if clear.Clicked() {
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
				collapse := ui.ButtonBase(c.Key("collapse")).Padding(6, 12).Radius(7).Label("收起托盘")
				if collapse.Hovered() {
					collapse.Background(cardHover)
				} else {
					collapse.Background(cardSurface)
				}
				collapse.Children(func() {
					ui.Text(c, "收起").SingleLine().FontSize(12).TextColor(textPrimary)
				})
				if collapse.Clicked() {
					a.hide(hideExplicit)
				}
			})
			// 标签切换：托盘（全部内容）｜剪贴板（剪贴板收录的内容）。
			// 剪贴板标签下同一行右侧是搜索框。
			ui.Row(c.Key("tabs")).Gap(8).AlignItems(ui.Center).Children(func() {
				a.tabSwitch(c)
				if a.tab == tabClipboard {
					a.searchField(c)
				}
			})
			ui.Text(c, a.panelHint()).SingleLine().FontSize(12).TextColor(textMuted)
			ui.Divider(c).Background(hairline)
			// 托盘标签列表上方：第一个报告已绘制的 panelCard（如正在播放）。
			if a.tab == tabTray {
				for _, m := range a.modules {
					if card, ok := m.(panelCard); ok && card.card(c) {
						break
					}
				}
			}
			list := a.visibleItems()
			if len(list) == 0 {
				a.emptyState(c)
			} else {
				ui.Scroll(c.Key("items")).Grow(1).Gap(6).Children(func() {
					// 快照：删除按钮或拖入回调可改变原切片。
					for _, item := range list {
						a.itemView(c, item)
					}
				})
			}
			if over {
				ui.Box(c).Padding(12).Radius(8).Background(t.Accent).Children(func() {
					ui.Text(c, "松开放入托盘").TextAlign(ui.Center).TextColor(t.AccentText).Bold()
				})
			}
			ui.Divider(c).Background(hairline)
			if a.status != "" {
				ui.Text(c, a.status).MaxLines(3).FontSize(11).TextColor(textMuted)
			} else {
				selected := a.selection.count()
				footer := fmt.Sprintf("%d 项 · 右键更多操作 · ⌘/⇧ 多选", len(list))
				hint := shortcutHint
				if a.tab == tabClipboard {
					hint = clipboardHint
					footer = fmt.Sprintf("%d 项 · ↑↓ 选择 · ⏎ 复制并收起", len(list))
				}
				if selected > 1 {
					footer = fmt.Sprintf("已选 %d 项 · 一起拖出 · 空格预览", selected)
				}
				ui.Row(c).Children(func() {
					ui.Text(c, footer).Grow(1).SingleLine().FontSize(11).TextColor(textMuted)
					ui.Text(c, hint).FontSize(11).TextColor(textMuted)
				})
			}
		})
	})
	if drop, ok := ui.DropData(zone, opts); ok {
		a.receive(drop.Data)
		if !a.visible {
			a.show(false)
		}
	}
}

// tabSwitch is the compact segmented switch in the panel header:
// 「托盘」(everything, as before) | 「剪贴板」(clipboard captures only).
func (a *shelf) tabSwitch(c *ui.Context) {
	row := ui.Row(c.Key("tab-switch")).Shrink(0).Padding(2).Gap(2).Radius(8).Background(cardSurface)
	row.Children(func() {
		a.tabButton(c, "tab-tray", "托盘", tabTray)
		a.tabButton(c, "tab-clipboard", "剪贴板", tabClipboard)
	})
}

// tabButton is one segment of the switch; the chosen one is highlighted
// and a click switches the list.
func (a *shelf) tabButton(c *ui.Context, key, label string, tab int) {
	on := a.tab == tab
	b := ui.ButtonBase(c.Key(key)).Padding(3, 10).Radius(6).Label(label)
	switch {
	case on:
		b.Background(ui.RGBA(255, 255, 255, 0.24))
	case b.Hovered():
		b.Background(cardHover)
	}
	b.Children(func() {
		text := ui.Text(c, label).SingleLine().FontSize(12)
		if on {
			text.TextColor(textPrimary).FontWeight(600)
		} else {
			text.TextColor(textMuted)
		}
	})
	if b.Clicked() {
		a.setTab(tab)
	}
}

// searchField is the clipboard tab's search box. Typing filters the list
// as it goes; while the shelf was opened for search the box keeps the
// keyboard focus, so ↑/↓ walk the list and ⏎ copies and collapses without
// reaching for the mouse. Esc clears the search first, then collapses.
func (a *shelf) searchField(c *ui.Context) {
	wrap := ui.Row(c.Key("clip-search")).Grow(1).Padding(3, 8).Radius(7).Background(cardSurface)
	wrap.Children(func() {
		in := ui.TextInputBase(c.Key("clip-query"), &a.search).Grow(1).MinWidth(0).
			Placeholder("搜索文字").Label(searchLabel).FontSize(12).TextColor(textPrimary)
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

// panelHint is the line under the title: what the current tab holds.
func (a *shelf) panelHint() string {
	if a.tab != tabClipboard {
		return "拖进来，随时带走"
	}
	if !a.settings.ClipboardHistory {
		return "剪贴板记录已关闭 · 菜单栏图标 → 剪贴板记录"
	}
	return "复制的内容自动收好 · " + clipboardHint + " 打开搜索"
}

// emptyState is what the panel shows when the current tab has no rows: the
// tray keeps its drop hint, the clipboard tab explains how captures arrive
// or that the search matched nothing.
func (a *shelf) emptyState(c *ui.Context) {
	t := c.Theme()
	icon, title, hint := shelfSymbol, "把文件拖到这里", "文件、文件夹、文字和图片"
	switch {
	case a.tab == tabClipboard && a.search != "":
		icon, title, hint = textSymbol, "没有匹配的内容", "换个关键词，或按 Esc 清空搜索"
	case a.tab == tabClipboard && !a.settings.ClipboardHistory:
		icon, title, hint = textSymbol, "剪贴板记录已关闭", "在菜单栏图标的「剪贴板记录」里重新打开"
	case a.tab == tabClipboard:
		icon, title, hint = textSymbol, "还没有剪贴板记录", "在其他应用按 ⌘C，文字和截图会自动收好"
	case a.settings.ClipboardHistory:
		hint += "\n复制的文字与截图也会自动收好"
	}
	ui.Column(c).Grow(1).Center().Gap(16).Children(func() {
		ui.Icon(c, icon).Size(48, 48).TextColor(t.Accent)
		ui.Text(c, title).FontSize(17).FontWeight(600).TextColor(textPrimary)
		ui.Text(c, hint).TextAlign(ui.Center).FontSize(12).TextColor(textMuted)
	})
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
	a.reveal = ""
	a.selection.keepExisting(a.visibleItems())
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

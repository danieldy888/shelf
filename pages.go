//go:build darwin

package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"shelf/internal/store"
)

// pageID names one page of the expanded panel. Pages are self-contained
// (a panelPage holds its own host), so there is no registry map to keep in
// sync: a page joins by registerPage and leaves by returning false from
// enabled.
type pageID string

const (
	pageTray      pageID = "tray"      // 托盘：全部内容
	pageClipboard pageID = "clipboard" // 剪贴板：只显示剪贴板收录的文字与图片
	pageUsage     pageID = "usage"     // AI 用量（由 AI 用量切片实现并登记）
)

// earMaxPages is the most page icons the left ear shows (DESIGN.md: 左耳最
// 多 5 个页面图标).
const earMaxPages = 5

// panelPage is one page of the expanded panel: its left-ear icon, its
// stage body under the module bar, and its own module-bar row content.
// live is false while the page is only the outgoing picture of a fade —
// drawn, but never taking the pointer or the keyboard; over is true while
// a drag hovers the panel. icon's active means the button is highlighted
// (the current page or a hovered icon), the same states that paint an
// ordinary page icon ink instead of ink2.
type panelPage interface {
	id() pageID
	label() string // tooltip and accessible name of the ear icon; the module bar spells it out too
	icon(c *ui.Context, active bool) ui.Element
	enabled() bool      // false keeps the icon off the ear and falls the current page back to the tray
	stageH() int        // the stage height this page wants, before stageMinH/stageMaxH clamping
	sharesHeight() bool // true pages size the stage to the group's tallest so switching keeps the panel's height
	body(c *ui.Context, over bool, live bool) ui.Element
	moduleTitle() (title, count string)                   // the module bar's left side; empty count shows nothing
	moduleTrailing(c *ui.Context, live bool) []ui.Element // the module bar's right side, appended in order
}

// ensurePages registers the built-in pages once; later pages are appended
// by registerPage in ear order. Pages assigned to a.pages before the first
// use (a test replacing the registry) keep their ids: a tray or clipboard
// page among them takes the built-in's seat.
func (a *shelf) ensurePages() {
	if a.pagesReady {
		return
	}
	a.pagesReady = true
	extra := a.pages
	a.pages = []panelPage{&trayPage{host: a}, &clipboardPage{host: a}}
	for _, p := range extra {
		a.registerPage(p)
	}
}

// registerPage adds p to the panel's page list, after the pages already
// registered; a page registered under an existing id replaces it (the
// order and ear position are kept). Built-in pages are registered lazily
// on first use so tests that build a bare &shelf{} get the same registry
// as the app.
func (a *shelf) registerPage(p panelPage) {
	a.ensurePages()
	for i, q := range a.pages {
		if q.id() == p.id() {
			a.pages[i] = p
			return
		}
	}
	a.pages = append(a.pages, p)
}

// enabledPages are the registered pages whose icon is on the ear bar, in
// ear order.
func (a *shelf) enabledPages() []panelPage {
	a.ensurePages()
	var out []panelPage
	for _, p := range a.pages {
		if p.enabled() {
			out = append(out, p)
		}
	}
	return out
}

// page returns the registered page with id, nil when none is registered
// (a disabled page still answers — enabled is what drops it).
func (a *shelf) page(id pageID) panelPage {
	a.ensurePages()
	for _, p := range a.pages {
		if p.id() == id {
			return p
		}
	}
	return nil
}

// pageIndex is id's position in the registered order; the ear icons share
// that order. -1 when no such page is registered.
func (a *shelf) pageIndex(id pageID) int {
	a.ensurePages()
	for i, p := range a.pages {
		if p.id() == id {
			return i
		}
	}
	return -1
}

// currentPage is the page the panel shows now: a.tab's page when it is
// registered and enabled, otherwise the tray — a page that gets switched
// off (e.g. AI 用量's setting) never stays current.
func (a *shelf) currentPage() panelPage {
	if p := a.page(a.tab); p != nil && p.enabled() {
		return p
	}
	if p := a.page(pageTray); p != nil {
		return p
	}
	return &trayPage{host: a}
}

// normalizeTab folds a stale a.tab (unset, or the id of a page that is now
// disabled or unregistered) back to the tray. The switch is silent: no
// fade, no outgoing page.
func (a *shelf) normalizeTab() {
	if p := a.page(a.tab); p != nil && p.enabled() {
		return
	}
	a.tab, a.tabFrom = pageTray, pageTray
	a.selection.keepExisting(a.visibleItems())
}

// trayPage is the panel's home page: the 5-column thumbnail grid of every
// stored item except ones hidden from the tray.
type trayPage struct{ host *shelf }

func (p *trayPage) id() pageID    { return pageTray }
func (p *trayPage) label() string { return "托盘" }
func (p *trayPage) icon(c *ui.Context, _ bool) ui.Element {
	return ui.Icon(c, trayNavSymbol)
}
func (p *trayPage) enabled() bool      { return true }
func (p *trayPage) sharesHeight() bool { return true }

// items is the page's list: everything but captures hidden from the tray
// (OffTray, or a drag-out removal still in its grace); pinned first.
func (p *trayPage) items() []store.Item {
	a := p.host
	if a.store == nil {
		return nil
	}
	var pinned, rest []store.Item
	for _, it := range a.store.Items {
		// 托盘页的移除对剪贴板收录只做到「不在托盘显示」：标了 OffTray 的
		// 项目仍留在剪贴板页，托盘不再列出。
		if it.OffTray || a.isLeaving(it) {
			continue
		}
		// 设计决定：剪贴板收录的文字只属于剪贴板标签；托盘放文件、
		// 文件夹、图片（含剪贴板截图）以及拖入/服务收录/置顶的文字。
		if it.Source == store.SourceClipboard && it.Kind == store.KindText && !it.Pinned {
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

func (p *trayPage) stageH() int {
	if n := len(p.items()); n > 0 {
		rows := (n + gridCols - 1) / gridCols
		return rows*tileH + (rows-1)*gridGap
	}
	return 0
}

func (p *trayPage) body(c *ui.Context, _ bool, _ bool) ui.Element {
	a := p.host
	list := p.items()
	if len(list) == 0 {
		hint := "文件、文件夹、文字和图片"
		if a.settings.ClipboardHistory {
			hint += "\n复制的文字与截图也会自动收好"
		}
		return a.emptyState(c, shelfSymbol, "把文件拖到这里", hint)
	}
	return ui.Scroll(c.Key("items")).Grow(1).MinHeight(0).Children(func() {
		ui.Grid(c.Key("grid")).ColumnTracks(ui.Fr(1), ui.Fr(1), ui.Fr(1), ui.Fr(1), ui.Fr(1)).
			Gap(gridGap).FillWidth().Children(func() {
			// 快照：右键菜单或拖入回调可改变原切片。
			for _, item := range list {
				a.itemTile(c, item)
			}
		})
	})
}

func (p *trayPage) moduleTitle() (title, count string) {
	return "托盘", fmt.Sprintf("%d 项", len(p.items()))
}

func (p *trayPage) moduleTrailing(c *ui.Context, live bool) []ui.Element {
	a := p.host
	list := p.items()
	var out []ui.Element
	if len(list) > 0 {
		hint := "⌘ / ⇧ 多选 · 空格预览"
		if n := a.selection.count(); n > 1 {
			hint = fmt.Sprintf("已选 %d 项 · 一起拖出", n)
		}
		out = append(out, ui.Text(c, hint).SingleLine().FontSize(11).FontWeight(600).TextColor(ink3).Margin(0, 6, 0, 0))
	}
	if gone := a.clearable(list, p.id()); len(gone) > 0 {
		b := a.iconButton(c, "clear", trashSymbol, "清空")
		if b.Clicked() && live {
			a.removeFrom(gone, p.id())
		}
		out = append(out, b)
	}
	return out
}

// clipboardPage is the capture history: the 32pt search field over the
// 46pt rows (source-app icon, text, 「应用 · 相对时间」). Only clipboard
// captures matching the search text are listed.
type clipboardPage struct{ host *shelf }

func (p *clipboardPage) id() pageID    { return pageClipboard }
func (p *clipboardPage) label() string { return "剪贴板" }
func (p *clipboardPage) icon(c *ui.Context, _ bool) ui.Element {
	return ui.Icon(c, clipNavSymbol)
}
func (p *clipboardPage) enabled() bool      { return true }
func (p *clipboardPage) sharesHeight() bool { return true }

// items is the page's list: clipboard captures matching the (already
// trimmed) search text; pinned first.
func (p *clipboardPage) items() []store.Item {
	a := p.host
	if a.store == nil {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(a.search))
	var pinned, rest []store.Item
	for _, it := range a.store.Items {
		if it.Source != store.SourceClipboard || !clipboardItemMatches(it, query) {
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

func (p *clipboardPage) stageH() int {
	h := clipSearchH
	if n := len(p.items()); n > 0 {
		h += clipListTop + n*(clipRowH+clipRowGap) - clipRowGap
	}
	return h
}

func (p *clipboardPage) body(c *ui.Context, _ bool, live bool) ui.Element {
	a := p.host
	return ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		a.searchField(c, live)
		list := p.items()
		if len(list) == 0 {
			icon, title, hint := textSymbol, "还没有剪贴板记录", "在其他应用按 ⌘C，文字和截图会自动收好"
			switch {
			case a.search != "":
				icon, title, hint = searchSymbol, "没有匹配的内容", "换个关键词，或按 Esc 清空搜索"
			case !a.settings.ClipboardHistory:
				icon, title, hint = textSymbol, "剪贴板记录已关闭", "在设置里打开「记录复制的内容」"
			}
			a.emptyState(c, icon, title, hint)
			return
		}
		ui.Scroll(c.Key("items")).Grow(1).MinHeight(0).Margin(clipListTop, 0, 0, 0).Children(func() {
			ui.Column(c.Key("clip-list")).Gap(clipRowGap).FillWidth().Children(func() {
				for _, item := range list {
					a.clipRow(c, item)
				}
			})
		})
	})
}

func (p *clipboardPage) moduleTitle() (title, count string) {
	return "剪贴板", fmt.Sprintf("%d 条", len(p.items()))
}

func (p *clipboardPage) moduleTrailing(c *ui.Context, live bool) []ui.Element {
	a := p.host
	if gone := a.clearable(p.items(), p.id()); len(gone) > 0 {
		b := a.iconButton(c, "clear", trashSymbol, "清空")
		if b.Clicked() && live {
			a.removeFrom(gone, p.id())
		}
		return []ui.Element{b}
	}
	return nil
}

//go:build darwin

package main

// The tool cards' catalog, drawing and file-drop entry, shared by the
// tool page (cardPanel / cardEdit surfaces) and the desktop pin windows
// (cardDesktop, task B). The persisted arrangement lives in
// internal/layout (layout.json); this file is what a card id means and
// how it draws, accepts files and runs.
//
// For the pin windows, one card instance is exactly these pieces:
//
//	el := a.toolCard(c, layout.Card{ID: id, Span: span}, cardDesktop)
//	// el is the card: content, fill1 shell, file-drop target and hot
//	// state are already wired. The pin adds its own tap handling via
//	// el.Clicked() → a.runToolCard(id) and its own drag-to-move via
//	// HandleInput, as tools_page.go does.
//	// A file drop landing on it calls a.runToolCardDrop(id, drop.Data)
//	// — already wired inside toolCard when the card accepts the class.
//
// Card ids are stable: layout.json stores them. The 框选文字 card has no
// entry yet — it appears once task C's startScreenOCR entry exists
// (PLAN.md 任务 A 第 3 条); its intended def is in a comment at
// toolCatalog.

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/aiusage"
	"shelf/internal/layout"
	"shelf/internal/mac"
	"shelf/internal/store"
)

// cardKind separates one-shot actions from widgets with their own state.
type cardKind int

const (
	cardAction cardKind = iota
	cardWidget
)

// toolCardDef is one catalog entry (design draft §3's table): the card
// id stored in layout.json, its title and glyph, the column span it
// defaults to (variableSpan cards toggle 1/2 in edit mode), what file
// classes it accepts and how a drop maps to the drop zone's action.
// accepts answers for a classifyDrop result; available keeps a card out
// of the picker while its feature is off (AI 用量 follows its setting).
type toolCardDef struct {
	id           string
	kind         cardKind
	title        string
	icon         *ui.SVG
	span         int
	variableSpan bool
	accepts      func(class dropClass) bool
	available    func(a *shelf) bool
}

// Card glyphs added for the tool page; the action glyphs come from the
// drop zone's set (dropactions.go) so the same action reads alike.
var (
	toolsNavSymbol = symbol(`<rect x="4" y="4" width="7" height="7" rx="2"/><rect x="13" y="4" width="7" height="7" rx="2"/><rect x="4" y="13" width="16" height="7" rx="2"/>`)
	cupSymbol      = symbol(`<path d="M5 9h11v5a5 5 0 0 1-5 5h-1a5 5 0 0 1-5-5z"/><path d="M16 11h1.5a2.5 2.5 0 0 1 0 5H16M8 3v3M11 3v3M14 3v3"/>`)
	plusSymbol     = symbol(`<path d="M12 5v14M5 12h14"/>`)
	pencilSymbol   = symbol(`<path d="M4 20h4L19 9l-4-4L4 16z"/><path d="M13 7l4 4"/>`)
	// ocrSymbol is the 框选文字 glyph from the design file, kept here so
	// the deferred card def only loses the comment when it lands.
	ocrSymbol = symbol(`<path d="M4 8V5a1 1 0 0 1 1-1h3M16 4h3a1 1 0 0 1 1 1v3M20 16v3a1 1 0 0 1-1 1h-3M8 20H5a1 1 0 0 1-1-1v-3"/><path d="M8 10h8M8 14h5"/>`)
	awakeWarn = ui.Hex("#FF9F0A") // 保持唤醒的橙点（设计稿 var(--warn)）
)

// toolCatalog is every card in picker order: actions first, then
// widgets (design §3's rows in order). The 框选文字 card is deliberately
// absent — its entry is task C's startScreenOCR; when C merges, it goes
// to the front of the actions and the default layout with it:
//
//	{id: "ocr", kind: cardAction, title: "框选文字", icon: ocrSymbol,
//		span: 1, accepts: imagesOnly, available: always},
func toolCatalog() []toolCardDef {
	return []toolCardDef{
		{id: "fit1m", kind: cardAction, title: "压缩到 1 MB", icon: shrinkSymbol, span: 1, accepts: imagesOnly, available: always},
		{id: "topdf", kind: cardAction, title: "转成 PDF", icon: pdfSymbol, span: 1, accepts: imagesOnly, available: always},
		{id: "tojpeg", kind: cardAction, title: "转 JPEG", icon: imgSymbol, span: 1, accepts: imagesOnly, available: always},
		{id: "toword", kind: cardAction, title: "转 Word", icon: docSymbol, span: 1, accepts: imagesOrPDFs, available: always},
		{id: "extract", kind: cardAction, title: "解压", icon: unzipSymbol, span: 1, accepts: archivesOnly, available: always},
		{id: "resize", kind: cardAction, title: "缩小图片", icon: shrinkSymbol, span: 1, accepts: imagesOnly, available: always},
		{id: "zip", kind: cardAction, title: "打成 zip", icon: zipSymbol, span: 1, accepts: anyClass, available: always},
		{id: "airdrop", kind: cardAction, title: "隔空投送", icon: airDropSymbol, span: 1, accepts: anyClass, available: always},
		{id: "playing", kind: cardWidget, title: "正在播放", icon: musicSymbol, span: 2, accepts: never, available: always},
		{id: "awake", kind: cardWidget, title: "保持唤醒", icon: cupSymbol, span: 1, variableSpan: true, accepts: never, available: always},
		{id: "recent", kind: cardWidget, title: "最近文件", icon: docSymbol, span: 2, accepts: never, available: always},
		{id: "clip", kind: cardWidget, title: "剪贴板最新", icon: clipNavSymbol, span: 2, accepts: never, available: always},
		{id: "usage", kind: cardWidget, title: "AI 用量", icon: gaugeSymbol, span: 2, accepts: never, available: usageAvailable},
	}
}

// The class predicates read classifyDrop's result, same as the drop
// zone's rules (dropactions.go): a card takes exactly the files its
// action could run on. zip and AirDrop take every non-empty class —
// folders, pictures, PDFs and mixes alike.
var (
	always         = func(*shelf) bool { return true }
	usageAvailable = func(a *shelf) bool { return a.settings.AIUsage.Enabled }
	imagesOnly     = func(class dropClass) bool { return class == classImages }
	imagesOrPDFs   = func(class dropClass) bool { return class == classImages || class == classPDFs }
	archivesOnly   = func(class dropClass) bool { return class == classArchives }
	anyClass       = func(class dropClass) bool { return true }
	never          = func(class dropClass) bool { return false }
)

// toolCardDef answers the catalog entry for id, or nil: a layout file
// can name a card this build does not know (a future card, or one that
// was removed), and such cells draw nothing.
func toolCardDefFor(id string) *toolCardDef {
	catalog := toolCatalog()
	for i := range catalog {
		if catalog[i].id == id {
			return &catalog[i]
		}
	}
	return nil
}

// toolCardSurface is where a card draws: the tool page's normal form,
// its name-only editing form, or a desktop pin window (task B). The
// panel and desktop draw identically except that the pin gets the
// border its floating-on-wallpaper needs (design §4) — the pin's own
// window adds that 0.5pt edge, the card itself stays the same.
type toolCardSurface int

const (
	cardPanel   toolCardSurface = iota // tool page, live form
	cardEdit                           // editing: glyph + name only, no live controls
	cardDesktop                        // pin window, live form
)

// toolCard draws one card: the fill1 14-radius shell with the card's
// content for its surface, the hot treatment while dragged files it
// accepts hover it, and its own drop target so a release on it runs the
// action instead of shelving the files. The caller sets the card's size
// and adds its surface's pointer wiring (long-press / drag-to-reorder in
// the editing grid, window drag on the desktop).
func (a *shelf) toolCard(c *ui.Context, spec layout.Card, surf toolCardSurface) ui.Element {
	def := toolCardDefFor(spec.ID)
	title := spec.ID
	if def != nil {
		title = def.title
	}
	card := ui.Column(c.Key("tc-" + spec.ID)).Radius(14).MinWidth(0).MinHeight(0).Label(title)
	if def == nil {
		// Unknown id in layout.json: keep an empty cell so the grid
		// still shows where the card would be.
		return card.Background(fill1)
	}
	// Dragged files hover: cards that take this class are drop targets
	// with the accent treatment; the rest dim to 38% (design §1) and
	// stay out of the drop's way, so a release falls through to the
	// panel's plain 放入托盘 receive.
	dragging := len(a.drop.files) > 0
	class := a.drop.class
	accepts := dragging && def.accepts(class)
	var hot bool
	if surf != cardEdit && accepts {
		_, hot = ui.DataDragOver(card, dropCellOpts)
	}
	switch {
	case hot:
		card.Background(accent16).Border(1.5, accent.Alpha(0.9))
	default:
		card.Background(fill1)
	}
	if a.win != nil && !c.Preferences().ReduceMotion {
		target := float32(1)
		if hot {
			target = 1.03
		}
		scale := card.AnimateTo("hot", target, ui.Animation{Spring: ui.SpringOptions{Duration: 200 * time.Millisecond, Bounce: 0.15}})
		card.Scale(scale, scale)
	} else if hot {
		card.Scale(1.03, 1.03)
	}
	if dragging && !accepts {
		card.Disabled(true).Opacity(0.38)
	}
	if surf == cardEdit {
		a.toolCardEditBody(c, card, def)
	} else {
		a.toolCardBody(c, card, def, class, spec.Span)
	}
	if accepts && surf != cardEdit {
		if drop, ok := ui.DropData(card, dropCellOpts); ok {
			a.runToolCardDrop(def.id, drop.Data)
		}
	}
	return card
}

// toolCardEditBody is the card's editing form (design §2): glyph and
// name, no live controls — the widget cards collapse to their name so a
// pause button can't fire while the card wobbles. Two-span widgets carry
// the 小组件 subtitle the mock shows.
func (a *shelf) toolCardEditBody(c *ui.Context, card ui.Element, def *toolCardDef) {
	card.Center().Gap(5).Children(func() {
		ui.Icon(c, def.icon).Size(20, 20).TextColor(ink)
		ui.Text(c, def.title).SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
		if def.span == 2 && def.kind == cardWidget {
			ui.Text(c, "小组件").SingleLine().FontSize(10).TextColor(ink3)
		}
	})
}

// toolCardBody is the live content: action cards centre glyph + name +
// subtitle, widgets lay out per card. While files hover the panel the
// action's subtitle becomes the dragged count (design §1: 副标题换成拖
// 入的数量).
func (a *shelf) toolCardBody(c *ui.Context, card ui.Element, def *toolCardDef, class dropClass, span int) {
	if def.kind == cardAction {
		card.Center().Gap(5).Children(func() {
			ui.Icon(c, def.icon).Size(20, 20).TextColor(ink)
			ui.Text(c, def.title).SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
			if sub := a.toolCardSub(def, class); sub != "" {
				ui.Text(c, sub).SingleLine().FontSize(10).TextColor(ink3)
			}
		})
		return
	}
	switch def.id {
	case "playing":
		a.toolPlayingBody(c, card)
	case "awake":
		a.toolAwakeBody(c, card, span)
	case "recent":
		a.toolRecentBody(c, card)
	case "clip":
		a.toolClipBody(c, card)
	case "usage":
		a.toolUsageBody(c, card)
	default:
		card.Center().Gap(5).Children(func() {
			ui.Icon(c, def.icon).Size(20, 20).TextColor(ink)
			ui.Text(c, def.title).SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
		})
	}
}

// toolCardSub is the action card's subtitle: the dragged file count
// while files the card accepts hover, or empty otherwise.
func (a *shelf) toolCardSub(def *toolCardDef, class dropClass) string {
	if len(a.drop.files) > 0 && def.accepts(class) {
		return dropCountTextFor(len(a.drop.files), class)
	}
	return ""
}

// toolCardDropAction is the drop zone cell a release on the card runs
// (design: 规则和投放区动作格相同). Images onto 转成 PDF merge into one
// PDF past the first; the other targets map 1:1.
func (a *shelf) toolCardDropAction(id string, paths []string) dropAction {
	def := toolCardDefFor(id)
	if def == nil {
		return zipAction(len(paths))
	}
	switch id {
	case "fit1m":
		return dropAction{id: id, icon: def.icon, label: def.title, sub: "以内", target: toFit1M}
	case "tojpeg":
		return dropAction{id: id, icon: def.icon, label: def.title, sub: "每张一份", target: toJPEG}
	case "resize":
		return dropAction{id: id, icon: def.icon, label: def.title, sub: "长边 1600 像素", target: toMedium}
	case "topdf":
		if len(paths) > 1 {
			return dropAction{id: id, icon: def.icon, label: "合成一个 PDF", sub: "按拖入顺序", target: toMergedPDF}
		}
		return dropAction{id: id, icon: def.icon, label: def.title, sub: "1 页", target: toPDF}
	case "toword":
		return dropAction{id: id, icon: def.icon, label: def.title, sub: "识别文字", target: toWord}
	case "extract":
		sub := filepath.Base(paths[0])
		if len(paths) > 1 {
			sub = fmt.Sprintf("%d 项", len(paths))
		}
		return dropAction{id: id, icon: def.icon, label: def.title, sub: sub, target: toExtract}
	case "airdrop":
		return dropAction{id: id, icon: def.icon, label: def.title, isAirDrop: true}
	default: // zip and any future any-class action
		return zipAction(len(paths))
	}
}

// runToolCardDrop executes a release on card id: the drop zone's own
// runner on a cell shaped like the card's action — the dragged files it
// sees are the panel's frozen drop.files, and the transfer data is the
// fallback for the pin windows, which never saw a panel drag. B calls
// this for its pins' drops exactly as the page does inside toolCard.
func (a *shelf) runToolCardDrop(id string, data transfer.Data) {
	def := toolCardDefFor(id)
	if def == nil || def.kind != cardAction {
		return
	}
	files, class := a.drop.files, a.drop.class
	if len(files) == 0 {
		f, err := dropFilesData(data)
		if err != nil || len(f) == 0 {
			return
		}
		files, class = f, classifyDrop(f)
	}
	if !def.accepts(class) {
		return
	}
	a.runDropAction(a.toolCardDropAction(id, files), data)
}

// runToolCard runs the card's tap action: an action card converts or
// AirDrops the tray's current selection, the widget cards do their own
// toggle/copy/jump. It reports whether the tap did something — the page
// uses the answer to jiggle a card that had nothing to act on (design:
// 没有可用的选中项时卡片轻摇一下，提示「把文件拖到这里」).
func (a *shelf) runToolCard(id string) bool {
	def := toolCardDefFor(id)
	if def == nil {
		return false
	}
	switch def.id {
	case "playing", "recent":
		return true // the card acts only through its own controls/thumbs
	case "awake":
		m := a.awake
		if m == nil {
			return false
		}
		if m.active() {
			m.stop(time.Now())
		} else {
			m.start(time.Now(), time.Hour)
		}
		return true
	case "clip":
		if it, ok := a.latestClipboardItem(); ok {
			return a.copyText(it.Text)
		}
		return false
	case "usage":
		if a.usage != nil {
			a.setTab(pageUsage)
			return true
		}
		return false
	}
	items := a.toolCardTargets(id)
	if len(items) == 0 {
		return false
	}
	if id == "airdrop" {
		paths, _ := usable(items)
		if len(paths) == 0 {
			return false
		}
		if err := airDrop(paths, nil); err != nil {
			a.report(err)
		}
		return true
	}
	target := a.toolCardTarget(id, len(items))
	if target < 0 {
		return false
	}
	a.startConvert(items, target)
	return true
}

// toolCardTarget is the conversion a tap on the action card starts;
// -1 where the card does not convert.
func (a *shelf) toolCardTarget(id string, n int) convertTarget {
	switch id {
	case "fit1m":
		return toFit1M
	case "tojpeg":
		return toJPEG
	case "resize":
		return toMedium
	case "topdf":
		if n > 1 {
			return toMergedPDF
		}
		return toPDF
	case "toword":
		return toWord
	case "extract":
		return toExtract
	case "zip":
		return toZip
	}
	return -1
}

// toolCardTargets is the tray selection an action card applies to,
// filtered to what the card can run on: the same class rules as a file
// drop (convertible images, PDFs, archives, or any on-disk item for zip
// and AirDrop). Nothing fitting means no selection the card could use.
func (a *shelf) toolCardTargets(id string) []store.Item {
	def := toolCardDefFor(id)
	if def == nil || def.kind != cardAction {
		return nil
	}
	var out []store.Item
	for _, it := range a.selection.chosen(a.itemsFor(pageTray)) {
		switch id {
		case "fit1m", "tojpeg", "resize", "topdf":
			if convertible(it) {
				out = append(out, it)
			}
		case "toword":
			if convertible(it) || isPDF(it) {
				out = append(out, it)
			}
		case "extract":
			if isArchive(it) {
				out = append(out, it)
			}
		default: // zip, airdrop: any file still on disk
			if !it.Missing && (it.Kind == store.KindFile || it.Kind == store.KindImage) {
				out = append(out, it)
			}
		}
	}
	return out
}

// latestClipboardItem is the newest clipboard capture (pinned first,
// like the clipboard page): the 剪贴板最新 card's payload.
func (a *shelf) latestClipboardItem() (store.Item, bool) {
	if a.store == nil {
		return store.Item{}, false
	}
	var pinned, rest []store.Item
	for _, it := range a.store.Items {
		if it.Source != store.SourceClipboard {
			continue
		}
		if it.Pinned {
			pinned = append(pinned, it)
		} else {
			rest = append(rest, it)
		}
	}
	items := append(pinned, rest...)
	if len(items) == 0 {
		return store.Item{}, false
	}
	return items[0], true
}

// awakeRemain is the 保持唤醒 card's subtitle: the countdown for timed
// holds, 一直 for the open-ended one, 关 while off.
func (a *shelf) awakeRemain() string {
	m := a.awake
	if m == nil || !m.active() {
		return "关"
	}
	if m.duration == 0 {
		return "一直"
	}
	return "剩 " + m.remainText(time.Now())
}

// toolAwakeBody is the 保持唤醒 widget: the 1-column form centres
// glyph + name + state with the on-dot in the corner; the 2-column form
// goes horizontal and adds the duration pills (design §3: 2 列时多出
// 30 分钟 / 1 / 2 / 4 小时 / 一直 选择).
func (a *shelf) toolAwakeBody(c *ui.Context, card ui.Element, span int) {
	if span == 1 {
		a.toolAwakeBodyNarrow(c, card)
		return
	}
	a.toolAwakeBodyWide(c, card)
}

// toolAwakeBodyNarrow is the 1-column form.
func (a *shelf) toolAwakeBodyNarrow(c *ui.Context, card ui.Element) {
	m := a.awake
	on := m != nil && m.active()
	card.Center().Gap(5).Children(func() {
		ui.Icon(c, cupSymbol).Size(20, 20).TextColor(ink)
		ui.Text(c, "保持唤醒").SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
		ui.Text(c, a.awakeRemain()).SingleLine().FontSize(10).TextColor(ink3)
		if on {
			ui.Box(c.Key("on-dot")).Absolute().Top(8).Right(8).Size(6, 6).Radius(3).Background(awakeWarn)
		}
	})
}

// toolAwakeBodyWide is the 2-column form, adding the duration pills.
func (a *shelf) toolAwakeBodyWide(c *ui.Context, card ui.Element) {
	m := a.awake
	on := m != nil && m.active()
	card.Padding(0, 12, 0, 12).Justify(ui.Center).Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
				ui.Icon(c, cupSymbol).Size(20, 20).TextColor(ink)
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, "保持唤醒").SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
					ui.Text(c, a.awakeRemain()).SingleLine().FontSize(10).TextColor(ink3)
				})
				if on {
					ui.Box(c.Key("on-dot")).Size(6, 6).Radius(3).Background(awakeWarn)
				}
			})
			ui.Row(c.Key("awake-chips")).Gap(6).Margin(8, 0, 0, 0).Children(func() {
				for _, choice := range awakeDurations {
					label := choice.label
					if i := strings.Index(label, "（"); i >= 0 {
						label = label[:i]
					}
					a.awakeChip(c, "chip-"+label, label, choice.d)
				}
			})
		})
	})
}

// awakeChip is one duration pill of the 2-column 保持唤醒 card; the held
// span (or 一直) gets the accent fill.
func (a *shelf) awakeChip(c *ui.Context, key, label string, d time.Duration) {
	m := a.awake
	on := m != nil && m.active() && m.duration == d
	chip := ui.Row(c.Key(key)).Height(20).Padding(0, 9, 0, 9).Radius(10).AlignItems(ui.Center).Label(label)
	if on {
		chip.Background(accent)
	} else {
		chip.Background(fill2)
	}
	chip.Children(func() {
		t := ui.Text(c, label).SingleLine().FontSize(10).FontWeight(600)
		if on {
			t.TextColor(ink)
		} else {
			t.TextColor(ink2)
		}
	})
	if chip.Clicked() && m != nil {
		if on {
			m.stop(time.Now())
		} else {
			m.start(time.Now(), d)
		}
	}
}

// toolRecentBody is the 最近文件 widget: title + hint on the left, up to
// three newest tray thumbnails on the right; each thumb drags out like
// its tray tile (design §3).
func (a *shelf) toolRecentBody(c *ui.Context, card ui.Element) {
	card.Padding(0, 12, 0, 12).Justify(ui.Center).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, "最近文件").SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
				ui.Text(c, "拖出即可").SingleLine().FontSize(10).TextColor(ink3)
			})
			ui.Row(c.Key("recent-thumbs")).Gap(6).AlignItems(ui.Center).Children(func() {
				items := a.itemsFor(pageTray)
				show := items[:min(3, len(items))]
				for _, it := range show {
					a.recentThumb(c, it)
				}
				if len(show) == 0 {
					ui.Icon(c, docSymbol).Size(20, 20).TextColor(ink3)
				}
			})
		})
	})
}

// recentThumb is one 40pt tray file thumbnail on the card: it drags out
// exactly like the tray tile (the copy lands in drag-out/, original
// untouched); 拖出后从托盘移除 does not apply — the card is not the tray
// page.
func (a *shelf) recentThumb(c *ui.Context, item store.Item) {
	thumb := ui.Row(c.Key("recent-"+item.ID)).Size(40, 40).Radius(8).Center().Clip().
		Label(itemTitle(item))
	if !item.Missing {
		thumb.DragDataFrom(func() transfer.Data {
			a.ownDrag = true
			a.takeServiceOwnership()
			if a.win != nil {
				a.win.Invalidate()
			}
			a.dragOutIDs = nil // a widget card is not the tray page: no removal
			return a.dragOut(item)
		}, transfer.DragOptions{Operations: transfer.Copy, Done: a.completed})
	}
	if b := a.thumbnail(item); b != nil {
		thumb.Children(func() {
			ui.Image(c.Key("bmp"), b).Size(40, 40).Fit(ui.Cover)
		})
	} else {
		thumb.Background(fill2).Children(func() {
			ui.Icon(c, fileSymbol).Size(18, 18).TextColor(ink3)
		})
	}
}

// toolClipBody is the 剪贴板最新 widget: the newest capture's text (or
// the screenshot title), with 点按复制 as the hint. The tap is wired by
// the surface (runToolCard).
func (a *shelf) toolClipBody(c *ui.Context, card ui.Element) {
	card.Padding(0, 12, 0, 12).Justify(ui.Center).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
			ui.Row(c.Key("clip-ic")).Size(36, 36).Radius(10).Background(fill2).Center().Children(func() {
				ui.Icon(c, clipNavSymbol).Size(16, 16).TextColor(ink2)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				if it, ok := a.latestClipboardItem(); ok {
					ui.Text(c, itemTitle(it)).SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
					ui.Text(c, "点按复制").SingleLine().FontSize(10).TextColor(ink3)
				} else {
					ui.Text(c, "剪贴板最新").SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
					ui.Text(c, "还没有剪贴板记录").SingleLine().FontSize(10).TextColor(ink3)
				}
			})
		})
	})
}

// toolUsageBody is the AI 用量 widget: the least-remaining tool's double
// ring beside its name and remaining percent, like the page's quota
// card. With no quota data yet it shows the bare rings (design §3).
func (a *shelf) toolUsageBody(c *ui.Context, card ui.Element) {
	var five, week *aiusage.Window
	ok := false
	if a.usage != nil {
		five, week, ok = a.usage.leastWindow()
	}
	card.Padding(0, 12, 0, 12).Justify(ui.Center).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
			a.toolUsageRing(c, five, week)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, "AI 用量").SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
				sub := "暂无数据"
				if ok {
					w := five
					if w == nil {
						w = week
					}
					sub = fmt.Sprintf("剩余 %.0f%%", max(0, 100-w.UsedPct))
				}
				ui.Text(c, sub).SingleLine().FontSize(10).TextColor(ink3)
			})
		})
	})
}

// toolUsageRing is the card's 44pt double ring: weekly outside, 5-hour
// inside, same colours and even-pace tick as the page's 64pt one.
func (a *shelf) toolUsageRing(c *ui.Context, five, week *aiusage.Window) {
	e := ui.Box(c.Key("tc-ring")).Size(44, 44).Center()
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if a.usage != nil {
			a.usage.drawWindowRing(p, r, 19, 3.5, week, usageWeekly, false, true)
			a.usage.drawWindowRing(p, r, 13.5, 3.5, five, usageFive, false, true)
		}
	})
	e.Children(func() {
		ui.Icon(c, gaugeSymbol).Size(14, 14).TextColor(ink3)
	})
}

// toolPlayingBody is the 正在播放 widget: 52pt cover, title and
// 歌手 · 应用 meta, and the transport buttons — the activity strip's
// parts in card form (design §3: 按钮就地控制). No track collapses to a
// quiet name-only card.
func (a *shelf) toolPlayingBody(c *ui.Context, card ui.Element) {
	m := a.np
	if m == nil {
		card.Center().Gap(5).Children(func() {
			ui.Icon(c, musicSymbol).Size(20, 20).TextColor(ink3)
			ui.Text(c, "正在播放").SingleLine().FontSize(12).FontWeight(600).TextColor(ink3)
		})
		return
	}
	st, ok := m.track()
	if !ok {
		card.Center().Gap(5).Children(func() {
			ui.Icon(c, musicSymbol).Size(20, 20).TextColor(ink3)
			ui.Text(c, "正在播放").SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
			ui.Text(c, "没有内容").SingleLine().FontSize(10).TextColor(ink3)
		})
		return
	}
	card.Padding(0, 12, 0, 12).Justify(ui.Center).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
			m.coverArt(c, "tc-np-art", 52, 10)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Justify(ui.Center).Children(func() {
				ui.Text(c, st.Title).SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
				if meta := m.cardMeta(st); meta != "" {
					ui.Text(c, meta).SingleLine().FontSize(10).TextColor(ink3)
				}
			})
			ui.Row(c.Key("tc-np-ctl")).Gap(10).AlignItems(ui.Center).Children(func() {
				m.controlButton(c, "tc-np-prev", prevFill, "上一首", mac.NPPrevious, 15, 15)
				m.controlButton(c, "tc-np-toggle", toggleSymbol(st.Playing), playPauseLabel(st.Playing), mac.NPPlayPause, 15, 15)
				m.controlButton(c, "tc-np-next", nextFill, "下一首", mac.NPNext, 15, 15)
			})
		})
	})
}

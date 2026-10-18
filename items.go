//go:build darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/store"
)

// Item rows come in two shapes: itemTile is the tray tab's 5-column grid
// tile (thumbnail over the name), clipRow the clipboard tab's list row
// (source-app icon, text, 「应用 · 相对时间」). Both keep the same
// selection, click, context menu and drag-out behaviour.

// itemTile is one cell of the tray grid: a 56pt thumbnail (the stored
// image, or the file's Finder icon), the name under it, a pin badge top
// right and the accent ring while selected. Moved files dim to 38% and
// name 「· 已移动」.
func (a *shelf) itemTile(c *ui.Context, item store.Item) {
	title := itemTitle(item)
	name := title
	if item.Missing {
		name += " · 已移动"
	}
	tile := ui.Column(c.Key(item.ID)).Height(tileH).Radius(12).AlignItems(ui.Center).
		Padding(8, 0, 0, 0).Label(name)
	if a.reveal == item.ID {
		// 键盘移动选中行后把它留在可见范围内。
		tile.ScrollIntoView()
	}
	selected := a.selection.has(item.ID)
	switch {
	case selected:
		tile.Background(accent16).Border(1.5, accent.Alpha(0.9))
	case tile.Hovered():
		tile.Background(fill1)
	}
	if !item.Missing {
		tile.DragDataFrom(func() transfer.Data {
			// 本应用发起的拖出会话：拖拽监视据此抑制摇晃召唤与篮子。
			a.ownDrag = true
			a.takeServiceOwnership()
			return a.dragOut(item)
		}, transfer.DragOptions{Operations: transfer.Copy, Done: a.completed})
	}
	tile.ContextMenu(func(m *ui.Menu) { a.itemMenu(m, item) })
	tile.Children(func() {
		a.tileThumb(c, item)
		ui.Text(c, name).MaxLines(2).TextAlign(ui.Center).FontSize(11).
			TextColor(ink.Alpha(0.86)).Margin(7, 4, 0, 4).MaxWidth(88)
		if item.Pinned {
			// 右上 16pt 黑底图钉角标：作为 tile 的子元素定位，不能放到
			// 网格层（否则所有角标叠在网格右上角并抢走点击）。
			ui.Row(c.Key("pin")).Absolute().Top(6).Right(10).Size(16, 16).Radius(8).Center().
				Background(ui.Hex("#000000")).Label("已置顶").
				Children(func() {
					ui.Icon(c, pinFill).Size(9, 9).TextColor(ink)
				})
		}
	})
	if item.Missing {
		tile.Opacity(0.38)
	}
	if tile.Clicked() {
		mods := tile.ClickModifiers()
		// 显示顺序来自当前标签（两个标签都是置顶在前），⇧ 选择跟着它走。
		a.selection.click(a.visibleItems(), item.ID, mods)
		a.reveal = ""
		if mods&(ui.Cmd|ui.Shift) == 0 && item.Kind == store.KindText {
			a.copyText(item.Text)
		}
		if a.win != nil {
			a.win.Invalidate()
		}
	}
}

// tileThumb draws the tile's 56pt thumbnail: the stored image (rounded,
// shadowed), the file's system icon, or for text a fill2 note card showing
// the first lines of the text (DESIGN review: no bare glyph).
func (a *shelf) tileThumb(c *ui.Context, item store.Item) {
	box := ui.Row(c.Key("thumbnail")).Size(56, 56).Center()
	if item.Kind == store.KindText {
		// itemTitle 给出有界预览（折叠空白、160 字截断），完整文字仍归
		// 保存、复制与拖出用。
		text := itemTitle(item)
		if text == "" {
			box.Radius(10).Background(fill2).Children(func() {
				ui.Icon(c, textSymbol).Size(22, 22).TextColor(ink3)
			})
			return
		}
		box.Radius(10).Background(fill2).AlignItems(ui.Start).Justify(ui.Start).Clip().Children(func() {
			ui.Text(c, text).MaxLines(3).FontSize(8).TextColor(ink2).Padding(7, 7, 0, 7)
		})
		return
	}
	box.Children(func() {
		defer formatBadge(c, item)
		if thumb := a.thumbnail(item); thumb != nil {
			im := ui.Image(c.Key("bmp"), thumb).Size(56, 56).Radius(10).Fit(ui.Cover)
			if item.Kind == store.KindImage {
				// 图片缩略图：圆角 + 阴影，像 Finder 里的预览。
				im.Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.45))
			} else {
				// 文件/文件夹用系统图标：图标自带形状，不再裁圆角。
				im.Radius(0).Fit(ui.Contain)
			}
			return
		}
		if item.Kind == store.KindImage {
			ui.Icon(c, fileSymbol).Size(34, 34).TextColor(ink3)
			return
		}
		ui.Icon(c, fileSymbol).Size(40, 40).TextColor(ink3)
	})
}

// formatBadge marks a converted picture's new format — 「JPG」, 「HEIC」,
// 「PNG」 — at the thumbnail's bottom-left corner, so a converted copy reads
// apart from its original. Documents carry their kind in their icon.
func formatBadge(c *ui.Context, item store.Item) {
	if !item.Converted || item.Missing {
		return
	}
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(item.Path), "."))
	switch ext {
	case "JPG", "PNG", "HEIC":
	default:
		return
	}
	ui.Row(c.Key("format")).Absolute().Left(-4).Bottom(-4).Height(14).Padding(0, 4, 0, 4).Radius(4).
		Center().Background(ui.Hex("#1C1C1E")).Border(1, ui.Hex("#000000")).Children(func() {
		ui.Text(c, ext).SingleLine().FontSize(8.5).FontWeight(700).TextColor(ink)
	})
}

// clipRow is one clipboard-tab list row: 46pt, the source app's icon (or a
// text glyph), the captured text single-line, 「应用名 · 相对时间」 under
// it, the pin mark on pinned rows and 「↩ 复制」 on the selected one while
// searching.
func (a *shelf) clipRow(c *ui.Context, item store.Item) {
	title := itemTitle(item)
	row := ui.Row(c.Key(item.ID)).Height(clipRowH).Padding(0, 10, 0, 10).Gap(10).Radius(10).
		AlignItems(ui.Center).Label(title)
	if a.reveal == item.ID {
		row.ScrollIntoView()
	}
	selected := a.selection.has(item.ID)
	switch {
	case selected:
		row.Background(accent24)
	case row.Hovered():
		row.Background(fill1)
	}
	if !item.Missing {
		row.DragDataFrom(func() transfer.Data {
			a.ownDrag = true
			a.takeServiceOwnership()
			return a.dragOut(item)
		}, transfer.DragOptions{Operations: transfer.Copy, Done: a.completed})
	}
	row.ContextMenu(func(m *ui.Menu) { a.itemMenu(m, item) })
	row.Children(func() {
		a.clipRowIcon(c, item)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Justify(ui.Center).Children(func() {
			text := title
			if item.Kind == store.KindImage {
				text = "截图 " + item.CreatedAt.Local().Format("01-02 15:04")
			}
			ui.Text(c, text).SingleLine().FontSize(13).TextColor(ink)
			ui.Text(c, a.clipRowMeta(item)).SingleLine().FontSize(11).TextColor(ink3)
		})
		if selected && a.focusMode {
			ui.Text(c, "↩ 复制").SingleLine().FontSize(11).TextColor(ink3)
		}
		if item.Pinned {
			ui.Icon(c.Key("pin"), pinFill).Size(12, 12).TextColor(ink.Alpha(0.5)).Label("已置顶")
		}
	})
	if row.Clicked() {
		mods := row.ClickModifiers()
		a.selection.click(a.visibleItems(), item.ID, mods)
		a.reveal = ""
		if mods&(ui.Cmd|ui.Shift) == 0 && item.Kind == store.KindText {
			a.copyText(item.Text)
		}
		if a.win != nil {
			a.win.Invalidate()
		}
	}
}

// clipRowIcon is the row's 22pt source-app icon, cached by bundle id, or
// the kind's glyph when no app is known.
func (a *shelf) clipRowIcon(c *ui.Context, item store.Item) {
	if item.SourceApp != "" {
		if icon := a.clipAppIcon(item.SourceApp); icon != nil {
			ui.Image(c.Key("ai-"+item.SourceApp), icon).Size(22, 22)
			return
		}
	}
	icon := textSymbol
	if item.Kind == store.KindImage {
		icon = fileSymbol
	}
	ui.Box(c).Size(22, 22).Radius(6).Center().Background(fill2).Children(func() {
		ui.Icon(c, icon).Size(12, 12).TextColor(ink3)
	})
}

// clipRowMeta is the row's 「应用 · 相对时间」 line.
func (a *shelf) clipRowMeta(item store.Item) string {
	ago := relativeTime(item.CreatedAt, time.Now())
	if item.SourceApp == "" {
		return ago
	}
	return a.appName(item.SourceApp) + " · " + ago
}

// clipAppIcon caches mac.AppIcon bitmaps per bundle id so a long list
// never asks the workspace twice for the same app. The cache is bounded:
// past appIconCacheLimit entries it is rebuilt on demand — clipboard rows
// re-resolve their icons on the next paint, apps gone from the list are
// simply never asked for again.
func (a *shelf) clipAppIcon(bundle string) *ui.Bitmap {
	if a.appIcons == nil {
		a.appIcons = make(map[string]*ui.Bitmap)
	}
	if b, ok := a.appIcons[bundle]; ok {
		return b
	}
	if len(a.appIcons) >= appIconCacheLimit {
		a.appIcons = make(map[string]*ui.Bitmap)
	}
	var b *ui.Bitmap
	if png := mac.AppIcon(bundle, 32); len(png) > 0 {
		if decoded, err := ui.DecodeBitmap(png); err == nil {
			b = decoded
		}
	}
	a.appIcons[bundle] = b
	return b
}

// appIconCacheLimit caps the source-app icon cache; rows re-resolve on
// the next paint after a flush, so a cap costs at most one repaint's
// worth of lookups.
const appIconCacheLimit = 64

// relativeTime renders the clipboard row's 「· 相对时间」: 刚刚 / N 分钟前 /
// 今天 15:04 / 昨天 / N 天前 / M月D日.
func relativeTime(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	local := t.Local()
	d := now.Sub(local)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour && local.Day() == now.Day() && local.Month() == now.Month():
		return "今天 " + local.Format("15:04")
	case d < 48*time.Hour:
		return "昨天"
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%d月%d日", int(local.Month()), local.Day())
	}
}

func itemTitle(item store.Item) string {
	if item.Kind == store.KindText {
		// 只缩短显示摘要；保存、复制和拖出仍使用完整文字。
		const limit = 160
		var preview strings.Builder
		preview.Grow(min(len(item.Text), limit*3))
		count, space := 0, false
		for _, r := range item.Text {
			if unicode.IsSpace(r) {
				space = count > 0
				continue
			}
			if count >= limit || space && count+1 >= limit {
				preview.WriteRune('…')
				break
			}
			if space {
				preview.WriteByte(' ')
				count++
				space = false
			}
			preview.WriteRune(r)
			count++
		}
		return preview.String()
	}
	if item.Kind == store.KindImage {
		return "截图 " + item.CreatedAt.Local().Format("01-02 15:04:05")
	}
	return filepath.Base(item.Path)
}

func (a *shelf) dragData(item store.Item) transfer.Data {
	if item.Kind == store.KindText {
		return transfer.TextData(item.Text)
	}
	if _, err := os.Stat(item.Path); err != nil {
		a.store.Refresh()
		a.report(fmt.Errorf("文件已移动或无法读取：%w", err))
		return transfer.Data{}
	}
	files, err := transfer.FileData(item.Path)
	if err != nil {
		a.report(err)
		return transfer.Data{}
	}
	if item.Kind != store.KindImage {
		return files
	}
	// 一个对象的原生文件 URL 和 PNG 双表示：Finder 接受文件，编辑器接受图片。
	png, err := os.ReadFile(item.Path)
	if err != nil {
		a.report(err)
		return transfer.Data{}
	}
	itemReps := files.Items()[0]
	var reps []transfer.Representation
	for _, format := range itemReps.Formats() {
		b, err := itemReps.Read(format)
		if err != nil {
			a.report(err)
			return transfer.Data{}
		}
		reps = append(reps, transfer.Bytes(format, b))
	}
	reps = append(reps, transfer.Bytes(transfer.PNG, png))
	return transfer.New(transfer.NewItem(reps...))
}

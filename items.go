//go:build darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/store"
)

// One row per stored item: its look, selection, click-to-copy, right-click
// menu, removal and drag-out.

func (a *shelf) itemView(c *ui.Context, item store.Item) {
	title := itemTitle(item)
	row := ui.Row(c.Key(item.ID)).Padding(10).Gap(10).Radius(8).Label(title)
	if a.reveal == item.ID {
		// 键盘移动选中行后把它留在可见范围内。
		row.ScrollIntoView()
	}
	switch {
	case a.selection.has(item.ID):
		row.Background(cardSelected)
	case row.Hovered():
		row.Background(cardHover)
	default:
		row.Background(cardSurface)
	}
	if !item.Missing {
		row.DragDataFrom(func() transfer.Data {
			// 本应用发起的拖出会话：拖拽监视据此抑制摇晃召唤与篮子。
			a.ownDrag = true
			a.takeServiceOwnership()
			return a.dragOut(item)
		}, transfer.DragOptions{Operations: transfer.Copy, Done: a.completed})
	}
	row.ContextMenu(func(m *ui.Menu) { a.itemMenu(m, item) })
	row.Children(func() {
		icon := fileSymbol
		if item.Kind == store.KindText {
			icon = textSymbol
		}
		var source ui.ImageSource = icon
		if thumb := a.thumbnail(item); thumb != nil {
			source = thumb
		}
		im := ui.Image(c.Key("thumbnail"), source).Size(40, 40).Fit(ui.Contain).TextColor(textMuted)
		if item.Missing {
			im.Grayscale().Opacity(0.55)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			color := textPrimary
			if item.Missing {
				color = textMuted
			}
			ui.Text(c, title).SingleLine().FontSize(13).FontWeight(550).TextColor(color)
			detail := "文件引用 · 拖出使用"
			if item.Kind == store.KindText {
				detail = "文字 · 单击复制"
			}
			if item.Kind == store.KindImage {
				detail = "PNG 图片 · 拖出使用"
			}
			if item.Source == store.SourceClipboard {
				detail = "剪贴板收录 · " + detail
			}
			if item.Missing {
				detail = "文件已移动"
			}
			ui.Text(c, detail).SingleLine().FontSize(11).TextColor(textMuted)
		})
		if item.Pinned {
			ui.Icon(c.Key("pin"), pinSymbol).Size(13, 13).TextColor(textMuted).Label("已置顶")
		}
		remove := ui.ButtonBase(c.Key("remove")).Size(26, 26).Padding(0).Center().Radius(13).Label("移除 " + title)
		remove.Children(func() { ui.Icon(c, closeSymbol).Size(12, 12).TextColor(textMuted) })
		if remove.Clicked() {
			a.remove([]store.Item{item})
		}
	})
	if row.Clicked() {
		mods := row.ClickModifiers()
		// 显示顺序来自当前标签（剪贴板标签有置顶排序），⇧ 选择跟着它走。
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

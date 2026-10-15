//go:build darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/store"
)

var (
	shelfSymbol = symbol(`<path d="M4 5v14h16V5M4 14h5l2 3h2l2-3h5M8 5h8M12 3v8m-3-3 3 3 3-3"/>`)
	textSymbol  = symbol(`<path d="M5 5h14M12 5v14M8 19h8"/>`)
	fileSymbol  = symbol(`<path d="M14 3H6v18h12V7zM14 3v5h4M9 12h6M9 16h6"/>`)
	closeSymbol = symbol(`<path d="m6 6 12 12M18 6 6 18"/>`)
)

func symbol(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

func (a *shelf) view(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) {
		a.hide()
	}
	c.OnShortcut(ui.Cmd, ui.KeyQ, mygo.App.Quit)
	t := c.Theme()
	zone := ui.Column(c.Key("shelf")).Fill().Padding(16).Gap(12).Background(t.Background)
	opts := transfer.DropOptions{Formats: []transfer.Format{transfer.FileList, transfer.URIList, transfer.PNG, transfer.Text}, Operations: transfer.Copy}
	_, over := ui.DataDragOver(zone, opts)
	zone.Children(func() {
		ui.Row(c).Gap(10).Children(func() {
			ui.Icon(c, shelfSymbol).Size(25, 25).TextColor(t.Accent)
			ui.Text(c, appName).FontSize(21).FontWeight(650).Grow(1)
			if a.store != nil && len(a.store.Items) > 0 {
				if ui.Button(c, "清空").Label("清空托盘").Clicked() {
					if err := a.store.Clear(); err != nil {
						a.report(err)
					} else {
						a.clearThumbnails()
						if a.win != nil {
							a.win.Invalidate()
						}
					}
				}
			}
			if ui.Button(c, "收起").Label("收起托盘").Clicked() {
				a.hide()
			}
		})
		ui.Text(c, "拖进来，随时带走").FontSize(12).TextColor(t.TextMuted)
		ui.Divider(c)
		if a.store == nil || len(a.store.Items) == 0 {
			ui.Column(c).Grow(1).Center().Gap(16).Children(func() {
				ui.Icon(c, shelfSymbol).Size(48, 48).TextColor(t.Accent)
				ui.Text(c, "把文件拖到这里").FontSize(17).FontWeight(600)
				ui.Text(c, "文件、文件夹、文字和图片\n复制的文字与截图也会自动收好").TextAlign(ui.Center).FontSize(12).TextColor(t.TextMuted)
			})
		} else {
			ui.Scroll(c.Key("items")).Grow(1).Gap(6).Children(func() {
				// 快照：删除按钮或拖入回调可改变原切片。
				items := a.store.Items
				for _, item := range items {
					a.itemView(c, item)
				}
			})
		}
		if over {
			ui.Box(c).Padding(12).Radius(8).Background(t.Accent).Children(func() {
				ui.Text(c, "松开放入托盘").TextAlign(ui.Center).TextColor(t.AccentText).Bold()
			})
		}
		ui.Divider(c)
		if a.status != "" {
			ui.Text(c, a.status).MaxLines(3).FontSize(11).TextColor(t.TextMuted)
		} else {
			count := 0
			if a.store != nil {
				count = len(a.store.Items)
			}
			ui.Row(c).Children(func() {
				ui.Text(c, fmt.Sprintf("%d 项 · 拖出不删除", count)).Grow(1).FontSize(11).TextColor(t.TextMuted)
				ui.Text(c, shortcutHint).FontSize(11).TextColor(t.TextMuted)
			})
		}
	})
	if drop, ok := ui.DropData(zone, opts); ok {
		a.receive(drop.Data)
	}
}

func (a *shelf) itemView(c *ui.Context, item store.Item) {
	t := c.Theme()
	title := itemTitle(item)
	row := ui.Row(c.Key(item.ID)).Padding(10).Gap(10).Radius(8).Label(title)
	if row.Hovered() {
		row.Background(t.SurfaceHover)
	} else {
		row.Background(t.Surface)
	}
	if !item.Missing {
		row.DragDataFrom(func() transfer.Data { return a.dragData(item) }, transfer.DragOptions{Operations: transfer.Copy, Done: a.completed})
	}
	row.Children(func() {
		icon := fileSymbol
		if item.Kind == store.KindText {
			icon = textSymbol
		}
		var source ui.ImageSource = icon
		if thumb := a.thumbnail(item); thumb != nil {
			source = thumb
		}
		im := ui.Image(c.Key("thumbnail"), source).Size(40, 40).Fit(ui.Contain).TextColor(t.TextMuted)
		if item.Missing {
			im.Grayscale().Opacity(0.55)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			color := t.Text
			if item.Missing {
				color = t.TextMuted
			}
			ui.Text(c, title).SingleLine().FontSize(13).FontWeight(550).TextColor(color)
			detail := "文件引用 · 拖出使用"
			if item.Kind == store.KindText {
				detail = "文字 · 单击复制"
			}
			if item.Kind == store.KindImage {
				detail = "PNG 图片 · 拖出使用"
			}
			if item.Missing {
				detail = "文件已移动"
			}
			ui.Text(c, detail).SingleLine().FontSize(11).TextColor(t.TextMuted)
		})
		remove := ui.Button(c.Key("remove"), "").Size(26, 26).Padding(0).Center().Label("移除 " + title)
		remove.Children(func() { ui.Icon(c, closeSymbol).Size(12, 12) })
		if remove.Clicked() {
			if err := a.store.Remove(item.ID); err != nil {
				a.report(err)
			} else {
				a.invalidateThumbnail(item.ID)
				if a.win != nil {
					a.win.Invalidate()
				}
			}
		}
	})
	if row.Clicked() && item.Kind == store.KindText {
		if err := mygo.Clipboard.Write(transfer.TextData(item.Text)); err != nil {
			a.report(err)
		} else {
			a.clipboardCount = mac.ClipboardChangeCount()
			a.status, a.statusUntil = "已复制 · 在其他应用按 ⌘V 粘贴", time.Now().Add(3*time.Second)
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

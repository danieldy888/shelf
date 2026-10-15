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

// 展开态配色：纯黑刘海壳上只放深色 UI，让托盘看起来像一整块硬件黑岛。
var (
	textPrimary = ui.Hex("#F3F4F6")
	textMuted   = ui.Hex("#9CA3AF")
	cardSurface = ui.RGBA(255, 255, 255, 0.08)
	cardHover   = ui.RGBA(255, 255, 255, 0.14)
	hairline    = ui.RGBA(255, 255, 255, 0.10)
)

// view 把刘海画成三态形态：收起（窗口常驻但完全不绘制）、探头（硬件刘海
// 向下鼓出小胶囊）、展开（纯黑面板从刘海里长出来）。单元测试里 a.win 为
// nil，直接按完整展开态渲染。
func (a *shelf) view(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) {
		a.hide()
	}
	c.OnShortcut(ui.Cmd, ui.KeyQ, mygo.App.Quit)
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
	if a.win != nil && !a.visible && a.progress <= 0.01 && !a.peeking && a.peekProgress <= 0.01 {
		idle := ui.Box(c.Key("shelf")).Fill()
		// 空闲态也注册为拖放目标：拖拽一开始窗口缩成刘海顶边窄条并接管
		// 鼠标事件（见 syncWindowFrame），WindowServer 在那里始终看到一个
		// 合法拖放目标而非触发 Spaces 栏，窄条之外仍把事件让给下层应用。
		ui.DataDragOver(idle, opts)
		if drop, ok := ui.DropData(idle, opts); ok {
			a.receive(drop.Data)
		}
		return
	}
	peeked := a.peeking || a.peekProgress > 0.01
	expanded := a.win == nil && !a.peeking || a.visible || openK >= 0.15
	// 刘海壳尺寸在三态间插值：硬件刘海 ←→ 探头胶囊 ←→ 完整面板。
	baseW := float32(max(180, a.notchW))
	baseH := float32(max(32, a.notchH))
	peekW, peekH := baseW+56, baseH+18
	expW, expH := float32(panelWidth-16), float32(a.panelH()-8)
	w0 := baseW + (peekW-baseW)*a.peekProgress
	h0 := baseH + (peekH-baseH)*a.peekProgress
	shellW := w0 + (expW-w0)*openK
	shellH := h0 + (expH-h0)*openK
	zone := ui.Column(c.Key("shelf")).Fill().AlignItems(ui.Center)
	// 窗口帧随状态缩放（展开=面板全幅，探头/拖拽=刘海顶边窄条，见
	// syncWindowFrame）：r.W/r.H 是当前帧的实际尺寸，壳只在水平方向居中、
	// 顶端贴齐 Y=0，因此同一段路径代码对两种帧都成立。
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
	_, over := ui.DataDragOver(zone, opts)
	zone.Children(func() {
		if !expanded {
			if !peeked {
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
				if a.store != nil && len(a.store.Items) > 0 {
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
							a.report(err)
						} else {
							a.clearThumbnails()
							if a.win != nil {
								a.win.Invalidate()
							}
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
					a.hide()
				}
			})
			ui.Text(c, "拖进来，随时带走").FontSize(12).TextColor(textMuted)
			ui.Divider(c).Background(hairline)
			if a.store == nil || len(a.store.Items) == 0 {
				ui.Column(c).Grow(1).Center().Gap(16).Children(func() {
					ui.Icon(c, shelfSymbol).Size(48, 48).TextColor(t.Accent)
					ui.Text(c, "把文件拖到这里").FontSize(17).FontWeight(600).TextColor(textPrimary)
					ui.Text(c, "文件、文件夹、文字和图片\n复制的文字与截图也会自动收好").TextAlign(ui.Center).FontSize(12).TextColor(textMuted)
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
			ui.Divider(c).Background(hairline)
			if a.status != "" {
				ui.Text(c, a.status).MaxLines(3).FontSize(11).TextColor(textMuted)
			} else {
				count := 0
				if a.store != nil {
					count = len(a.store.Items)
				}
				ui.Row(c).Children(func() {
					ui.Text(c, fmt.Sprintf("%d 项 · 拖出不删除", count)).Grow(1).FontSize(11).TextColor(textMuted)
					ui.Text(c, shortcutHint).FontSize(11).TextColor(textMuted)
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

func (a *shelf) itemView(c *ui.Context, item store.Item) {
	title := itemTitle(item)
	row := ui.Row(c.Key(item.ID)).Padding(10).Gap(10).Radius(8).Label(title)
	if row.Hovered() {
		row.Background(cardHover)
	} else {
		row.Background(cardSurface)
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
			if item.Missing {
				detail = "文件已移动"
			}
			ui.Text(c, detail).SingleLine().FontSize(11).TextColor(textMuted)
		})
		remove := ui.ButtonBase(c.Key("remove")).Size(26, 26).Padding(0).Center().Radius(13).Label("移除 " + title)
		remove.Children(func() { ui.Icon(c, closeSymbol).Size(12, 12).TextColor(textMuted) })
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

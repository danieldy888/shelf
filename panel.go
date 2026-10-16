//go:build darwin

package main

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
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
	idle := a.win != nil && !a.visible && a.progress <= 0.01 && !a.peeking && a.peekProgress <= 0.01
	// Keep one native drop target across all states. The transparent root does
	// not paint the theme background; clipping contains every animation frame.
	zone := ui.Column(c.Key("shelf")).Fill().AlignItems(ui.Center).Clip()
	_, over := ui.DataDragOver(zone, opts)
	if idle {
		// 空闲态收到内容只入库不展开：那时窗口本就不显示，也无从接受拖放。
		if drop, ok := ui.DropData(zone, opts); ok {
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
					hint := "文件、文件夹、文字和图片"
					if a.settings.ClipboardHistory {
						hint += "\n复制的文字与截图也会自动收好"
					}
					ui.Text(c, hint).TextAlign(ui.Center).FontSize(12).TextColor(textMuted)
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

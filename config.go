package main

import (
	"strings"
	"time"
)

const (
	appName = "ShelfU"
	// Carbon 全局快捷键，不需要辅助功能权限。
	trayShortcut = "Ctrl+Alt+Space"
	// 打开剪贴板搜索：显示托盘、切到剪贴板标签并把焦点放进搜索框。
	clipboardShortcut = "Ctrl+Alt+V"
	// 框选文字：框一块屏幕区域，识别出的文字直接进剪贴板。
	ocrShortcut = "Ctrl+Alt+O"
	// searchLabel 是搜索框的无障碍名称（也是测试定位用的标签）。
	searchLabel = "搜索剪贴板"
	// dropBandPadX 是刘海投放带的左右余量：拖拽时 ShelfU 只在「刘海宽
	// + 两侧余量 × 投放带高」这一条里接拖放与响应鼠标。投放带高度
	// 是菜单栏加 guardDropPad：拖放守卫把光标夹到带底这一行，落点
	// 必须在菜单栏下方、仍在窗口内。
	dropBandPadX   = 24 // 投放带相对刘海的水平外扩，单位 pt。
	guardDropPad   = 16 // 投放带相对菜单栏的向下外扩，单位 pt。
	dropHintExtraW = 32 // 拖拽带外提示：壳比刘海多出的宽度（两侧各半），单位 pt。
	notchHoverPadX = 8  // 空手移入刘海探头的水平余量，单位 pt。
	notchHoverPadY = 4  // 空手移入刘海探头的垂直余量，单位 pt。
	// Idle, hover and drag share one transparent strip. Opening, closing and
	// a level change resize the window; the drag filter protects its centre.
	edgeLeaveDelay  = 350 * time.Millisecond
	pointerInterval = 30 * time.Millisecond
	// panelAnimation is the open/close progress's time base: a.progress runs
	// 0→1 over it, and the 「彻底空闲」 window check reads the same
	// value. The real window's shell and content move on springs.
	panelAnimation = 260 * time.Millisecond
	peekAnimation  = 160 * time.Millisecond // 悬停探头长出/缩回
	tabAnimation   = 240 * time.Millisecond // 页面切换：胶囊滑动、旧页淡出后新页淡入
	// 探头里的图形必须整体落在硬件刘海下方：刘海区域没有像素。
	peekBody       = 18 // 空手悬停：刘海下方露出的高度（放下拉箭头）
	dropPillBody   = 32 // 拖拽中：刘海下方「松开放入」那一行的高度
	dropPillExtraW = 60 // 拖拽中：比刘海宽出的部分
	// 面板尺寸见 panel.go 的 islandW/islandH/sheetW/sheetH；原生窗口初始
	// 尺寸用 sheetW × sheetH，两种状态都画在同一个窗口里。
	// 设置窗口：普通有标题窗口的尺寸（设置内容超出时内部滚动）。
	settingsWidth  = 460
	settingsHeight = 520
)

// shortcutHint is what the panel footer shows for trayShortcut; it is
// derived from the constant so the hint can never disagree with the
// registered shortcut.
var shortcutHint = shortcutLabel(trayShortcut)

// clipboardHint is the panel footer's label for clipboardShortcut, derived
// from the constant the same way.
var clipboardHint = shortcutLabel(clipboardShortcut)

// ocrHint is the settings row's label for ocrShortcut, derived the same way.
var ocrHint = shortcutLabel(ocrShortcut)

// clipboardLimitChoices are the clipboard text limits offered in the
// menu-bar menu; the setting takes any number, the menu the usual ones.
var clipboardLimitChoices = []int{30, 100, 300}

// shortcutLabel renders an Electron-style accelerator ("Ctrl+Alt+Space")
// as macOS glyphs ("⌃⌥Space"). Unknown spellings are returned unchanged
// so the hint never claims a shortcut the app did not register.
func shortcutLabel(acc string) string {
	parts := strings.Split(acc, "+")
	glyphs := make([]string, 0, len(parts))
	for i, raw := range parts {
		token := strings.TrimSpace(raw)
		if token == "" {
			return acc
		}
		if i == len(parts)-1 {
			switch {
			case strings.EqualFold(token, "space"):
				token = "Space"
			case len(token) == 1:
				token = strings.ToUpper(token)
			}
			glyphs = append(glyphs, token)
			continue
		}
		switch strings.ToLower(token) {
		case "cmd", "command", "super", "cmdorctrl":
			glyphs = append(glyphs, "⌘") // 目标平台只有 macOS
		case "ctrl", "control":
			glyphs = append(glyphs, "⌃")
		case "alt", "option":
			glyphs = append(glyphs, "⌥")
		case "shift":
			glyphs = append(glyphs, "⇧")
		default:
			return acc
		}
	}
	return strings.Join(glyphs, "")
}

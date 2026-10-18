package main

import (
	"strings"
	"time"
)

const (
	appName = "Shelf"
	// Carbon 全局快捷键，不需要辅助功能权限。
	trayShortcut = "Ctrl+Alt+Space"
	// 打开剪贴板搜索：显示托盘、切到剪贴板标签并把焦点放进搜索框。
	clipboardShortcut = "Ctrl+Alt+V"
	// searchLabel 是搜索框的无障碍名称（也是测试定位用的标签）。
	searchLabel        = "搜索剪贴板"
	notchTriggerWidth  = 280 // 刘海拖拽接收区与保护区的宽度，单位 pt。
	notchTriggerHeight = 64  // 拖拽靠近刘海时的垂直感应高度，单位 pt。
	notchHoverPadX     = 8   // 空手移入刘海探头的水平余量，单位 pt。
	notchHoverPadY     = 4   // 空手移入刘海探头的垂直余量，单位 pt。
	// Idle, hover and drag share one transparent strip. Only expansion
	// changes the native window size; the drag filter protects its centre.
	peekStripPadX   = 40 // 探头/拖拽窄条相对刘海的水平外扩，单位 pt。
	peekStripPadH   = 40 // 探头/拖拽顶边条相对刘海的垂直外扩，单位 pt。
	edgeLeaveDelay  = 350 * time.Millisecond
	pointerInterval = 30 * time.Millisecond
	panelAnimation  = 260 * time.Millisecond // 展开/收起 260ms 缓出（DESIGN.md「动效」）
	peekAnimation   = 160 * time.Millisecond // 悬停探头长出/缩回
	resizeAnimation = 240 * time.Millisecond // 切换标签、内容增减时面板高度的过渡
	tabAnimation    = 240 * time.Millisecond // 页面切换：胶囊滑动、旧页淡出后新页淡入
	// 探头里的图形必须整体落在硬件刘海下方：刘海区域没有像素。
	peekBody       = 18 // 空手悬停：刘海下方露出的高度（放下拉箭头）
	dropPillBody   = 32 // 拖拽中：刘海下方「松开放入」那一行的高度
	dropPillExtraW = 60 // 拖拽中：比刘海宽出的部分
	panelWidth     = 560
	panelHeight    = 400
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

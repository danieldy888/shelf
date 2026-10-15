package main

import (
	"strings"
	"time"
)

const (
	appName = "Shelf"
	// Carbon 全局快捷键，不需要辅助功能权限。
	trayShortcut       = "Ctrl+Alt+Space"
	notchTriggerWidth  = 280 // 拖拽靠近刘海时的水平感应宽度，单位 pt。
	notchTriggerHeight = 64  // 拖拽靠近刘海时的垂直感应高度，单位 pt。
	notchHoverPadX     = 8   // 空手移入刘海探头的水平余量，单位 pt。
	notchHoverPadY     = 4   // 空手移入刘海探头的垂直余量，单位 pt。
	// 拖拽/探头时常驻窗口缩成刘海两侧各 peekStripPadX、刘海下方
	// peekStripPadH（不低于 notchTriggerHeight）的顶边窄条：只拦截刘海
	// 周围的事件与拖放，不再挡住面板覆盖区下方的应用。
	peekStripPadX     = 40 // 探头窄条相对刘海的水平外扩，单位 pt。
	peekStripPadH     = 40 // 探头窄条相对刘海的垂直外扩，单位 pt。
	edgeDwell         = 90 * time.Millisecond
	edgeLeaveDelay    = 350 * time.Millisecond
	clipboardInterval = 500 * time.Millisecond
	pointerInterval   = 30 * time.Millisecond
	panelAnimation    = 180 * time.Millisecond
	panelWidth        = 440
	panelHeight       = 400
)

// shortcutHint is what the panel footer shows for trayShortcut; it is
// derived from the constant so the hint can never disagree with the
// registered shortcut.
var shortcutHint = shortcutLabel(trayShortcut)

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

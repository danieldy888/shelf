package main

import "testing"

// 面板底部的快捷键提示必须由 trayShortcut 常量渲染而来，
// 不能与实际注册的快捷键不一致（README 承诺“改常量即同步提示”）。
func TestShortcutLabelFollowsConstant(t *testing.T) {
	cases := map[string]string{
		"Ctrl+Alt+Space":        "⌃⌥Space",
		"Ctrl+Alt+S":            "⌃⌥S",
		"Cmd+Shift+Z":           "⌘⇧Z",
		"CmdOrCtrl+Shift+Space": "⌘⇧Space",
		"Super+Alt+2":           "⌘⌥2",
		"Shift+F5":              "⇧F5",
		// 未知写法原样显示，不虚构按键。
		"Bogus+Key": "Bogus+Key",
		"":          "",
	}
	for in, want := range cases {
		if got := shortcutLabel(in); got != want {
			t.Fatalf("shortcutLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if shortcutHint != shortcutLabel(trayShortcut) {
		t.Fatalf("shortcutHint %q does not follow trayShortcut %q", shortcutHint, trayShortcut)
	}
}

//go:build darwin

package main

import (
	"time"

	"github.com/egoist/mygo"

	"shelf/internal/mac"
)

// clipboardModule records copied text and images into the shelf. It is
// present only while the clipboardHistory setting is on.
type clipboardModule struct {
	host  *shelf
	count int64     // pasteboard changeCount already handled
	last  time.Time // last poll; polls run every clipboardInterval
}

// newClipboardModule starts from the current clipboard, so whatever was
// copied before launch is not recorded.
func newClipboardModule(host *shelf) *clipboardModule {
	return &clipboardModule{host: host, count: mac.ClipboardChangeCount()}
}

func (m *clipboardModule) tick(now time.Time) {
	if now.Sub(m.last) < clipboardInterval {
		return
	}
	m.last = now
	count := mac.ClipboardChangeCount()
	if count == m.count {
		return
	}
	m.count = count
	a := m.host
	if a.store == nil {
		return
	}
	// 每次变化只收一条：按来源声明的首选表示取文字或图片，避免双条目。
	// 同时记录复制发生的瞬间哪个应用在最前：右键菜单据此提供“不再记录
	// 来自该应用的内容”，设置里被排除的应用则整条跳过。
	bundleID, _ := mac.FrontmostApp()
	switch clipCapture(mac.ClipboardTypes(), bundleID, a.settings.ExcludedApps) {
	case clipText:
		if text := mygo.Clipboard.ReadText(); text != "" {
			a.changed(a.store.AddClipText(text, bundleID))
		}
	case clipImage:
		if png := mygo.Clipboard.ReadImage(); len(png) > 0 {
			a.changed(a.store.AddClipImage(png, bundleID))
		}
	}
}

// skipCurrent marks the clipboard's current contents as handled; used after
// Shelf itself writes to the clipboard so the copy is not recorded again.
// Safe on a nil module (clipboard history off).
func (m *clipboardModule) skipCurrent() {
	if m != nil {
		m.count = mac.ClipboardChangeCount()
	}
}

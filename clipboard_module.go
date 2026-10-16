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
	count int64 // pasteboard changeCount already handled
}

// newClipboardModule starts from the current clipboard, so whatever was
// copied before launch is not recorded.
func newClipboardModule(host *shelf) *clipboardModule {
	return &clipboardModule{host: host, count: mac.ClipboardChangeCount()}
}

// tick runs on every monitoring poll (~30 ms): the changeCount read is
// cheap and keeps latency low, which also lets the frontmost app be
// attributed to the app that copied, not whatever is frontmost when a
// slower poll happens to run.
func (m *clipboardModule) tick(_ time.Time) {
	count := mac.ClipboardChangeCount()
	if count == m.count {
		return
	}
	m.count = count
	a := m.host
	if a.store == nil {
		return
	}
	// 记录复制发生的瞬间哪个应用在最前。Shelf 自己处于前台（搜索面板
	// 拿了键盘）时不记来源；被用户排除的应用整条跳过。
	bundleID, _ := mac.FrontmostApp()
	bundleID = captureSource(mac.FrontmostPID(), mac.OwnPID(), bundleID)
	// 每次变化只收一条：按来源声明的首选表示取文字或图片，避免双条目。
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

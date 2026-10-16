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
	switch classifyClipboard(mac.ClipboardTypes()) {
	case clipText:
		if text := mygo.Clipboard.ReadText(); text != "" {
			a.changed(a.store.AddText(text))
		}
	case clipImage:
		if png := mygo.Clipboard.ReadImage(); len(png) > 0 {
			a.changed(a.store.AddImage(png))
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

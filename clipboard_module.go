//go:build darwin

package main

import (
	"time"

	"github.com/egoist/mygo"

	"shelf/internal/mac"
)

// The recorder reads the system pasteboard in production; tests can supply
// copy events without writing non-private fixtures to the user's pasteboard.
type clipReader interface {
	changeCount() int64
	sourceApp() string
	types() []string
	text() string
	image() []byte
}

type systemClipReader struct{}

func (systemClipReader) changeCount() int64 { return mac.ClipboardChangeCount() }
func (systemClipReader) types() []string    { return mac.ClipboardTypes() }
func (systemClipReader) text() string       { return mygo.Clipboard.ReadText() }
func (systemClipReader) image() []byte      { return mygo.Clipboard.ReadImage() }
func (systemClipReader) sourceApp() string {
	bundleID, _ := mac.FrontmostApp()
	return captureSource(mac.FrontmostPID(), mac.OwnPID(), bundleID)
}

// clipboardModule records copied text and images throughout the app session.
type clipboardModule struct {
	host  *shelf
	board clipReader
	count int64 // pasteboard changeCount already handled
}

// newClipboardModule starts from the current clipboard, so whatever was
// copied before launch is not recorded.
func newClipboardModule(host *shelf) *clipboardModule {
	board := systemClipReader{}
	return &clipboardModule{host: host, board: board, count: board.changeCount()}
}

// tick runs on every monitoring poll (~30 ms): the changeCount read is
// cheap and keeps latency low, which also lets the frontmost app be
// attributed to the app that copied, not whatever is frontmost when a
// slower poll happens to run.
func (m *clipboardModule) tick(_ time.Time) {
	count := m.board.changeCount()
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
	bundleID := m.board.sourceApp()
	// 每次变化只收一条：按来源声明的首选表示取文字或图片，避免双条目。
	switch clipCapture(m.board.types(), bundleID, a.settings.ExcludedApps) {
	case clipText:
		if text := m.board.text(); text != "" {
			a.changed(a.store.AddClipText(text, bundleID))
			a.warmClipApp(bundleID)
		}
	case clipImage:
		if png := m.board.image(); len(png) > 0 {
			a.changed(a.store.AddClipImage(png, bundleID))
			a.warmClipApp(bundleID)
		}
	}
}

// warmClipApp resolves the new row's app name and icon now, on the tick
// that recorded the copy, instead of on the frame that first draws the
// clipboard page: the name lookup is a several-millisecond workspace call
// (measured ~8ms cold) and would otherwise land inside a page switch.
func (a *shelf) warmClipApp(bundle string) {
	if bundle == "" || bundle == clipSourceOCR || a.store == nil {
		return
	}
	a.appName(bundle)
	a.clipAppIcon(bundle)
}

// skipCurrent marks the clipboard's current contents as handled; used after
// Shelf itself writes to the clipboard so the copy is not recorded again.
// Safe on a nil module in shelves that have not started monitoring yet.
func (m *clipboardModule) skipCurrent() {
	if m != nil {
		m.count = m.board.changeCount()
	}
}

//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/store"
)

// targets is what an action on item applies to: the whole selection when
// item is part of a multi-item selection, otherwise item alone, which then
// becomes the selection, as in Finder.
func (a *shelf) targets(item store.Item) []store.Item {
	if a.selection.has(item.ID) && a.selection.count() > 1 {
		return a.selection.chosen(a.visibleItems())
	}
	a.selection.only(item.ID)
	return []store.Item{item}
}

// usable splits items into the paths of files and images that still exist
// and the texts; moved files are left out.
func usable(items []store.Item) (paths, texts []string) {
	for _, it := range items {
		switch {
		case it.Kind == store.KindText:
			texts = append(texts, it.Text)
		case !it.Missing:
			paths = append(paths, it.Path)
		}
	}
	return paths, texts
}

// itemMenu is the right-click menu of a row. Its items depend only on the
// targets, so the menu is built the same way when it opens and when an
// item is chosen.
func (a *shelf) itemMenu(m *ui.Menu, item store.Item) {
	items := a.targets(item)
	paths, texts := usable(items)
	files := len(paths) > 0
	if m.Item("打开").Disabled(!files).Chosen() {
		for _, p := range paths {
			if err := mygo.Shell.OpenPath(p); err != nil {
				a.report(err)
				return
			}
		}
	}
	if m.Item("在 Finder 中显示").Disabled(!files).Chosen() {
		for _, p := range paths {
			mygo.Shell.ShowItemInFolder(p)
		}
	}
	if m.Item("快速查看").Disabled(!files).Chosen() {
		a.quickLook(paths)
	}
	if m.Item("隔空投送…").Disabled(!files && len(texts) == 0).Chosen() {
		if err := mac.AirDrop(paths, texts); err != nil {
			a.report(err)
		}
	}
	a.convertMenu(m, items)
	m.Separator()
	copyLabel := "复制文字"
	if files {
		copyLabel = "复制路径"
	}
	if m.Item(copyLabel).Disabled(!files && len(texts) == 0).Chosen() {
		lines := texts
		if files {
			lines = paths
		}
		a.copyText(strings.Join(lines, "\n"))
	}
	// 置顶：作用于右键的行（或多选的全部）；全部已置顶时改为取消置顶。
	pinned := len(items) > 0
	for _, it := range items {
		if !it.Pinned {
			pinned = false
			break
		}
	}
	pinLabel := "置顶"
	if pinned {
		pinLabel = "取消置顶"
	}
	if m.Item(pinLabel).Chosen() {
		a.setPinned(items, !pinned)
	}
	// 剪贴板来源：一键把这个应用加入不再记录列表。
	if item.Source == store.SourceClipboard && item.SourceApp != "" {
		if m.Item("不再记录来自「" + a.appName(item.SourceApp) + "」的内容").Disabled(a.settingsBroken).Chosen() {
			a.excludeClipboardApp(item.SourceApp)
		}
	}
	m.Separator()
	removeLabel := "移除"
	if len(items) > 1 {
		removeLabel = fmt.Sprintf("移除 %d 项", len(items))
	}
	if m.Item(removeLabel).Chosen() {
		a.remove(items)
	}
}

// clipWriter is where Shelf's own copies go. Production uses the system
// clipboard; tests substitute a recorder so copying can be exercised
// without a running app (mygo's clipboard needs App.Run).
type clipWriter interface {
	writeText(text string) error
	writeImage(png []byte) error
}

// systemClipboard is the production clipWriter.
type systemClipboard struct{}

func (systemClipboard) writeText(text string) error {
	return mygo.Clipboard.Write(transfer.TextData(text))
}

func (systemClipboard) writeImage(png []byte) error { return mygo.Clipboard.WriteImage(png) }

// writer is where a copy goes: the system clipboard unless a test
// substituted a recorder (a.write).
func (a *shelf) writer() clipWriter {
	if a.write != nil {
		return a.write
	}
	return systemClipboard{}
}

// copyText puts text on the clipboard without recording it again and
// reports whether the write went through; callers that hide the shelf
// afterwards use it to stay open when the copy failed.
func (a *shelf) copyText(text string) bool {
	if err := a.writer().writeText(text); err != nil {
		a.report(err)
		return false
	}
	a.clip.skipCurrent()
	a.toastStatus("已复制 · 在其他应用按 ⌘V 粘贴", time.Now().Add(3*time.Second), false)
	return true
}

// copyItem puts one item on the clipboard as ⌘C on it would: text as text,
// an image as PNG. It reports whether it copied; a failure is reported.
func (a *shelf) copyItem(item store.Item) bool {
	switch item.Kind {
	case store.KindText:
		return a.copyText(item.Text)
	case store.KindImage:
		png, err := os.ReadFile(item.Path)
		if err != nil {
			a.report(err)
			return false
		}
		if err := a.writer().writeImage(png); err != nil {
			a.report(err)
			return false
		}
		a.clip.skipCurrent()
		a.toastStatus("已复制图片 · 在其他应用按 ⌘V 粘贴", time.Now().Add(3*time.Second), false)
		return true
	}
	a.report(errors.New("这一项不能复制到剪贴板"))
	return false
}

// setPinned pins or unpins every target; each item is saved on its own
// (the store's API), and the first failure stops the rest and is reported.
func (a *shelf) setPinned(items []store.Item, pinned bool) {
	for _, it := range items {
		if err := a.store.SetPinned(it.ID, pinned); err != nil {
			a.report(err)
			return
		}
	}
	if a.win != nil {
		a.win.Invalidate()
	}
}

// appName resolves and caches the display name of a bundle identifier. The
// context menu's exclude item must read the same when the menu opens and
// when it is chosen, so a name resolved once cannot change under it, and
// the lookup (running apps, then the bundle on disk) runs once per app.
func (a *shelf) appName(bundleID string) string {
	if name, ok := a.appNames[bundleID]; ok {
		return name
	}
	name := mac.AppNameForBundleID(bundleID)
	if name == "" {
		name = bundleID
	}
	if a.appNames == nil {
		a.appNames = map[string]string{}
	}
	a.appNames[bundleID] = name
	return name
}

// excludeClipboardApp stops recording copies from bundleID: the identifier
// is appended to the excludedApps setting and saved at once, so the
// clipboard module skips it from the next copy on. Disabled while
// settings.json could not be read, so a broken file is never overwritten.
func (a *shelf) excludeClipboardApp(bundleID string) {
	if bundleID == "" || a.settingsBroken {
		return
	}
	name := a.appName(bundleID)
	if !slices.Contains(a.settings.ExcludedApps, bundleID) {
		next := a.settings
		next.ExcludedApps = append(slices.Clone(a.settings.ExcludedApps), bundleID)
		if !a.applySettings(next) {
			return
		}
	}
	a.toastStatus("已不再记录来自「"+name+"」的内容", time.Now().Add(3*time.Second), false)
}

// removeExcludedApp takes bundleID out of the excludedApps setting — the
// per-row remove button of the Settings window's 排除的应用 list — and
// saves at once, so the clipboard module records the app from the next
// copy on. Every entry equal to it goes (case-insensitive, as Launch
// Services compares bundle identifiers). Disabled while settings.json
// could not be read, so a broken file is never overwritten.
func (a *shelf) removeExcludedApp(bundleID string) {
	if bundleID == "" || a.settingsBroken {
		return
	}
	next := a.settings
	next.ExcludedApps = slices.DeleteFunc(slices.Clone(a.settings.ExcludedApps),
		func(id string) bool { return strings.EqualFold(strings.TrimSpace(id), bundleID) })
	if slices.Equal(next.ExcludedApps, a.settings.ExcludedApps) {
		return
	}
	a.applySettings(next)
}

// remove takes items off the shelf; original files stay. On the tray
// page, removing a clipboard capture only hides it from the tray — the
// clipboard page keeps it and only its own 移除/清空 truly deletes —
// while every other item is deleted outright. On the clipboard page all
// of them are deleted.
func (a *shelf) remove(items []store.Item) {
	var hide, del []string
	for _, it := range items {
		if a.tab == tabTray && it.Source == store.SourceClipboard {
			hide = append(hide, it.ID)
		} else {
			del = append(del, it.ID)
		}
	}
	hidden := 0
	if len(hide) > 0 {
		n, err := a.store.HideFromTray(hide)
		if err != nil {
			a.report(err)
		}
		hidden = n
	}
	n := 0
	if len(del) > 0 {
		var err error
		n, err = a.store.RemoveMany(del)
		if err != nil {
			// n > 0: removed and saved, but an image file could not be deleted.
			a.report(err)
		}
	}
	if hidden == 0 && n == 0 {
		return
	}
	for _, id := range del {
		a.invalidateThumbnail(id)
	}
	a.selection.keepExisting(a.visibleItems())
	if a.win != nil {
		a.win.Invalidate()
	}
}

// quickLook previews paths in the system Quick Look panel.
func (a *shelf) quickLook(paths []string) {
	if len(paths) == 0 {
		return
	}
	if err := mac.QuickLook(paths); err != nil {
		a.report(err)
	}
}

// toggleQuickLook is Space while the shelf is in use: it closes an open
// preview, or previews the selected files and images.
func (a *shelf) toggleQuickLook() {
	if mac.QuickLookVisible() {
		mac.CloseQuickLook()
		return
	}
	if a.store == nil || !a.visible {
		return
	}
	paths, _ := usable(a.selection.chosen(a.visibleItems()))
	a.quickLook(paths)
}

// dragOut is what dragging item carries: every selected item when item is
// part of a multi-item selection, otherwise item alone.
func (a *shelf) dragOut(item store.Item) transfer.Data {
	if !a.selection.has(item.ID) || a.selection.count() < 2 {
		return a.dragData(item)
	}
	var all []transfer.Item
	for _, it := range a.selection.chosen(a.visibleItems()) {
		if it.Missing {
			continue
		}
		all = append(all, a.dragData(it).Items()...)
	}
	if len(all) == 0 {
		a.report(errors.New("选中的项目都已无法读取"))
		return transfer.Data{}
	}
	return transfer.New(all...)
}

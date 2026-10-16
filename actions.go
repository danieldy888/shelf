//go:build darwin

package main

import (
	"errors"
	"fmt"
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
		return a.selection.chosen(a.store.Items)
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
	m.Separator()
	removeLabel := "移除"
	if len(items) > 1 {
		removeLabel = fmt.Sprintf("移除 %d 项", len(items))
	}
	if m.Item(removeLabel).Chosen() {
		a.remove(items)
	}
}

// copyText puts text on the clipboard without recording it again.
func (a *shelf) copyText(text string) {
	if err := mygo.Clipboard.Write(transfer.TextData(text)); err != nil {
		a.report(err)
		return
	}
	a.clip.skipCurrent()
	a.status, a.statusUntil = "已复制 · 在其他应用按 ⌘V 粘贴", time.Now().Add(3*time.Second)
	if a.win != nil {
		a.win.Invalidate()
	}
}

// remove takes items off the shelf in one save; original files stay.
func (a *shelf) remove(items []store.Item) {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	n, err := a.store.RemoveMany(ids)
	if err != nil {
		// n > 0: removed and saved, but an image file could not be deleted.
		a.report(err)
	}
	if n == 0 {
		return
	}
	for _, id := range ids {
		a.invalidateThumbnail(id)
	}
	a.selection.keepExisting(a.store.Items)
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
	paths, _ := usable(a.selection.chosen(a.store.Items))
	a.quickLook(paths)
}

// dragOut is what dragging item carries: every selected item when item is
// part of a multi-item selection, otherwise item alone.
func (a *shelf) dragOut(item store.Item) transfer.Data {
	if !a.selection.has(item.ID) || a.selection.count() < 2 {
		return a.dragData(item)
	}
	var all []transfer.Item
	for _, it := range a.selection.chosen(a.store.Items) {
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

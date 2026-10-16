//go:build darwin

package main

// Finder 右键“服务/快速操作”菜单与应用内“服务”菜单的落点：
// “添加到 Shelf”把选中的文件、文件夹或文字放进托盘。

import (
	"time"

	"github.com/egoist/mygo"

	"shelf/internal/mac"
)

// serviceSeq 标记最近一次由服务唤出的闪现；用户在闪现后接管面板
// （点进面板拿走 Esc、选中条目、拖出）则自动收起失效。
var serviceSeq int

// installServices registers "添加到 Shelf" so Finder and any app's Services
// menu can send selected files or text into the shelf. A failure is a
// startup notice, not fatal: the shelf still works through drag, paste and
// the clipboard module.
func (a *shelf) installServices() {
	err := mac.InstallServiceProvider(func(paths []string, text string) {
		mygo.RunOnMain(func() { a.serviceReceive(paths, text) })
	})
	if err != nil {
		a.notice("服务菜单不可用：" + err.Error())
	}
}

// serviceReceive stores one service payload on the main thread. Files win
// over text, matching receive(); both empty means the provider already
// faulted the request. The panel flashes briefly so the user sees the item
// land, then retracts itself unless they start using it — less intrusive
// than leaving the panel open after a menu action elsewhere, and without
// holding Esc or Space while it flashes.
func (a *shelf) serviceReceive(paths []string, text string) {
	if a.store == nil {
		return
	}
	var err error
	switch {
	case len(paths) > 0:
		err = a.store.AddFiles(paths)
	case text != "":
		err = a.store.AddText(text)
	default:
		return
	}
	if err == nil {
		a.status = "已放入 · 拖出后仍会保留"
		a.statusUntil = time.Now().Add(3 * time.Second)
	}
	a.changed(err)
	// 托盘已显示时用户正在看它，交给现有状态栏，不再闪动。
	if a.visible {
		return
	}
	serviceSeq++
	seq := serviceSeq
	a.show(false)
	a.releaseKeys()
	time.AfterFunc(2*time.Second, func() {
		mygo.RunOnMain(func() {
			if seq != serviceSeq || !a.visible || a.quitting || a.dragging || a.pointerDrag || a.escHeld || a.selection.count() > 0 {
				return
			}
			a.hide()
		})
	})
}

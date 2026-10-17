//go:build darwin

package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/settings"
	"shelf/internal/store"
)

func main() {
	dataDir := flag.String("data-dir", "", "本地数据目录（默认 Library/Application Support/Shelf）")
	show := flag.Bool("show", false, "启动时显示托盘")
	flag.Parse()
	if *dataDir == "" {
		root, err := store.DefaultRoot()
		if err != nil {
			log.Fatal(err)
		}
		*dataDir = root
	}
	s, err := store.Open(*dataDir)
	if err != nil {
		// 历史未成功打开时不创建托盘、快捷键或监控，也不改写原件。
		log.Printf("Shelf: %v", err)
		message := "无法读取 Shelf 历史"
		detail := "历史文件保持原样，本次启动已停止。请先备份并检查数据目录中的 state.json。\n\n数据目录：" + *dataDir + "\n\n" + err.Error()
		if errors.Is(err, store.ErrLocked) {
			message = "Shelf 已在运行"
			detail = "同一个数据目录同时只能被一个实例使用，本实例将退出，不会改动已有历史。\n\n数据目录：" + *dataDir
		}
		mygo.App.SetName(appName)
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
		mygo.App.WhenReady(func() {
			go func() {
				_, _ = mygo.Dialog.Message(mygo.MessageOptions{
					Type:    mygo.MessageError,
					Title:   appName,
					Message: message,
					Detail:  detail,
				})
				mygo.App.Quit()
			}()
		})
		if runErr := mygo.App.Run(); runErr != nil {
			log.Fatal(runErr)
		}
		return
	}
	if s != nil {
		if merr := s.MigrationError(); merr != nil {
			// 备份失败时存储已停止写入；这里只做启动日志，界面状态在 ready 中显示。
			log.Printf("Shelf: 迁移前备份失败，已停止写入以免覆盖可恢复原件：%v", merr)
		}
	}
	prefs, perr := settings.Load(*dataDir)
	a := &shelf{store: s, settings: prefs, dataDir: *dataDir, stop: make(chan struct{}), thumbnails: make(map[string]*ui.Bitmap), thumbPending: make(map[string]bool)}
	// 剪贴板文字限额来自设置；小于 1 的值由 store 回落到默认 30。
	s.SetTextLimit(prefs.ClipboardLimit)
	if perr != nil {
		a.settingsBroken = true
		a.notice("设置文件无法读取，本次使用默认设置：" + perr.Error())
	}
	mygo.App.SetName(appName)
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quitting = true })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		close(a.stop)
		a.stopDragGuard()
		mygo.GlobalShortcut.UnregisterAll()
		if a.stopMouseObservation != nil {
			a.stopMouseObservation()
		}
		if a.stopDragObservation != nil {
			a.stopDragObservation()
		}
		if a.tray != nil {
			a.tray.Destroy()
		}
		a.store.Close()
	})
	mygo.App.WhenReady(func() {
		if err := mac.Init(); err != nil {
			log.Fatal(err)
		}
		a.ready(*show)
	})
	mygo.App.OnActivate(func(bool) {
		// macOS reopen（如 open -a / Finder 再次启动已运行的实例）：设置
		// 窗口开着就归还给它；否则等价于菜单栏图标点击——切换而非强制
		// 展开，避免常驻窗口随激活事件自行弹出。
		if a.settingsWin != nil {
			mac.ActivateSelf()
			a.settingsWin.Focus()
			return
		}
		if a.win != nil {
			a.toggle()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *shelf) ready(show bool) {
	a.newNotchWindow()
	a.stopMouseObservation = mac.ObserveMouseDown(a.win.NativeHandle(), func(inside bool) {
		a.mouseDown(inside, mygo.Screen.CursorScreenPoint())
	})
	a.stopDragObservation = mac.ObserveLeftMouseDragged(a.pointerDragged)
	a.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			a.hide(hideExplicit)
		}
	})
	var err error
	a.tray, err = mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(), IconIsTemplate: true, ToolTip: "Shelf · 临时托盘"})
	if err != nil {
		log.Fatal(err)
	}
	a.tray.OnClick(a.toggle)
	a.tray.OnRightClick(func() {
		a.tray.SetMenu(a.trayMenu())
		defer a.tray.SetMenu(nil)
		a.tray.PopUpMenu()
	})
	if err := mygo.GlobalShortcut.Register(trayShortcut, a.toggle); err != nil {
		a.notice(fmt.Sprintf("快捷键被占用；可点击菜单栏图标。%v", err))
	}
	if err := mygo.GlobalShortcut.Register(clipboardShortcut, a.openClipboardSearch); err != nil {
		a.notice(fmt.Sprintf("剪贴板搜索快捷键被占用；可点击菜单栏图标。%v", err))
	}
	a.applyClipboardHistory()
	a.modules = append(a.modules, &retentionModule{host: a})
	a.np = &nowPlayingModule{host: a}
	a.modules = append(a.modules, a.np)
	_, a.dragSerial = mac.DragState()
	mygo.Screen.OnDisplaysChanged(func() {
		// 常驻窗口跟随光标所在显示器重新定位（收起态无感）。
		a.place(mygo.Screen.CursorScreenPoint())
	})
	if a.store != nil {
		if merr := a.store.MigrationError(); merr != nil {
			a.notice("迁移前备份失败，已停止写入以保护原文件：" + merr.Error())
		}
	}
	if show {
		a.show(false)
	} else {
		// Keep the same top-anchored transparent strip when idle or peeking;
		// only its drawing and mouse pass-through change on hover.
		a.place(mygo.Screen.CursorScreenPoint())
		a.syncMousePassThrough()
		a.win.ShowInactive()
	}
	a.installServices()
	a.startMonitoring()
	log.Print("Shelf ready")
}

// newNotchWindow creates the persistent notch window: one transparent,
// frameless, non-activating panel that the three states redraw. The smoke
// test builds the same window, so the ⌃⌥V focus behaviour is checked
// against what production shows.
func (a *shelf) newNotchWindow() {
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title: appName, Width: panelWidth, Height: panelHeight,
		Hidden: true, Frameless: true, Transparent: true, DisableShadow: true,
		AlwaysOnTop: true, SkipTaskbar: true, Panel: true,
		DisableMove: true, DisableResize: true, DisableMinimize: true, DisableMaximize: true, DisableFullScreen: true,
		Content: ui.View(a.view),
	})
	a.win.SetVisibleOnAllWorkspaces(true)
	mac.ConfigureNotchWindow(a.win.NativeHandle())
	a.win.OnMove(a.windowGeometryChanged)
	a.win.OnResize(a.windowGeometryChanged)
	log.Printf("window collectionBehavior=0x%x", mac.CollectionBehavior(a.win.NativeHandle()))
}

// trayMenu is rebuilt on every right-click so it reflects the current
// Accessibility state, which the user changes in System Settings, and the
// current settings.
func (a *shelf) trayMenu() *mygo.Menu {
	guard := &mygo.MenuItem{Label: "刘海拖放保护已开启", Disabled: true}
	if !mac.AccessibilityTrusted() {
		guard = &mygo.MenuItem{Label: "开启刘海拖放保护…", Click: func(*mygo.MenuItem, *mygo.Window) {
			mac.RequestAccessibility()
		}}
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "显示 / 收起 Shelf", Click: func(*mygo.MenuItem, *mygo.Window) { a.toggle() }},
		guard,
		{Label: "自动清理", Type: mygo.MenuItemSubmenu, Submenu: a.retentionMenu()},
		{Label: "剪贴板记录", Type: mygo.MenuItemSubmenu, Submenu: a.clipboardMenu()},
		{Label: "正在播放", Type: mygo.MenuItemCheckbox, Checked: a.settings.IslandNowPlaying, Disabled: a.settingsBroken,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.setNowPlaying(func(s *settings.Settings) { s.IslandNowPlaying = !s.IslandNowPlaying })
			}},
		mygo.Separator(),
		{Label: "设置…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings() }},
		a.updateMenu(),
		mygo.Separator(),
		{Label: "退出 Shelf", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
}

// retentionMenu offers the retention periods. It is disabled while
// settings.json could not be read, so a broken file is never overwritten.
func (a *shelf) retentionMenu() []*mygo.MenuItem {
	choice := func(label string, days int) *mygo.MenuItem {
		current := a.settings.RetentionDays
		if current < 0 {
			current = 0
		}
		return &mygo.MenuItem{
			Label: label, Type: mygo.MenuItemRadio, Checked: current == days, Disabled: a.settingsBroken,
			Click: func(*mygo.MenuItem, *mygo.Window) { a.setRetention(days) },
		}
	}
	items := []*mygo.MenuItem{choice("不自动清理", 0)}
	for _, d := range retentionChoices {
		items = append(items, choice(fmt.Sprintf("清理超过 %d 天的内容", d), d))
	}
	return items
}

// setRetention saves the new period and applies it at once.
func (a *shelf) setRetention(days int) {
	if a.settings.RetentionDays == days {
		return
	}
	next := a.settings
	next.RetentionDays = days
	if !a.applySettings(next) {
		return
	}
	a.removeExpired(time.Now())
}

// clipboardMenu offers the clipboard history toggle and the text limits.
// It is disabled while settings.json could not be read, so a broken file is
// never overwritten. Both act at once: the module starts or stops, and the
// new limit applies on the next copy.
func (a *shelf) clipboardMenu() []*mygo.MenuItem {
	items := []*mygo.MenuItem{{
		Label: "记录剪贴板", Type: mygo.MenuItemCheckbox, Checked: a.settings.ClipboardHistory, Disabled: a.settingsBroken,
		Click: func(*mygo.MenuItem, *mygo.Window) { a.setClipboardHistory(!a.settings.ClipboardHistory) },
	}}
	items = append(items, mygo.Separator())
	limit := a.settings.ClipboardLimit
	if limit < 1 {
		limit = settings.Default().ClipboardLimit
	}
	for _, n := range clipboardLimitChoices {
		items = append(items, &mygo.MenuItem{
			Label: fmt.Sprintf("保留 %d 条", n), Type: mygo.MenuItemRadio, Checked: limit == n, Disabled: a.settingsBroken,
			Click: func(*mygo.MenuItem, *mygo.Window) { a.setClipboardLimit(n) },
		})
	}
	return items
}

// setNowPlaying saves the now-playing preference; the module reads its
// setting on every tick, so a change applies at once without a restart.
// Disabled while settings.json could not be read, so a broken file is
// never overwritten.
func (a *shelf) setNowPlaying(mutate func(*settings.Settings)) {
	next := a.settings
	mutate(&next)
	a.applySettings(next)
}

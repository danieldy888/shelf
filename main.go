//go:build darwin

package main

import (
	"errors"
	"flag"
	"fmt"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
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
	a := &shelf{store: s, stop: make(chan struct{}), thumbnails: make(map[string]*ui.Bitmap), thumbPending: make(map[string]bool)}
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
		// macOS reopen（如 open -a / Finder 再次启动已运行的实例）等价于
		// 菜单栏图标点击：切换而非强制展开，避免常驻窗口随激活事件自行弹出。
		if a.win != nil {
			a.toggle()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *shelf) ready(show bool) {
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
	a.stopMouseObservation = mac.ObserveMouseDown(a.win.NativeHandle(), func(inside bool) {
		a.mouseDown(inside, mygo.Screen.CursorScreenPoint())
	})
	a.stopDragObservation = mac.ObserveLeftMouseDragged(a.pointerDragged)
	a.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			a.hide()
		}
	})
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "显示 / 收起 Shelf", Click: func(*mygo.MenuItem, *mygo.Window) { a.toggle() }},
		mygo.Separator(),
		{Label: "退出 Shelf", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
	var err error
	a.tray, err = mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(), IconIsTemplate: true, ToolTip: "Shelf · 临时托盘"})
	if err != nil {
		log.Fatal(err)
	}
	a.tray.OnClick(a.toggle)
	a.tray.OnRightClick(func() {
		a.tray.SetMenu(menu)
		defer a.tray.SetMenu(nil)
		a.tray.PopUpMenu()
	})
	if err := mygo.GlobalShortcut.Register(trayShortcut, a.toggle); err != nil {
		a.status = fmt.Sprintf("快捷键被占用；可点击菜单栏图标。%v", err)
		log.Print(a.status)
	}
	a.clipboardCount = mac.ClipboardChangeCount()
	_, a.dragSerial = mac.DragState()
	mygo.Screen.OnDisplaysChanged(func() {
		// 常驻窗口跟随光标所在显示器重新定位（收起态无感）。
		a.place(mygo.Screen.CursorScreenPoint())
	})
	if a.store != nil {
		if merr := a.store.MigrationError(); merr != nil {
			a.status = "迁移前备份失败，已停止写入以保护原文件：" + merr.Error()
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
	a.startMonitoring()
	log.Print("Shelf ready")
}

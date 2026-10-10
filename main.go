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
	if errors.Is(err, store.ErrLocked) {
		// 同一数据目录只允许一个实例。第二个实例报告原因后安全退出，
		// 绝不共享写入，避免覆盖已有历史。
		log.Printf("Shelf: %v", err)
		mygo.App.SetName(appName)
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
		mygo.App.WhenReady(func() {
			go func() {
				_, _ = mygo.Dialog.Message(mygo.MessageOptions{
					Type:    mygo.MessageError,
					Title:   appName,
					Message: "Shelf 已在运行",
					Detail:  "同一个数据目录同时只能被一个实例使用，本实例将退出，不会改动已有历史。\n\n数据目录：" + *dataDir,
				})
				mygo.App.Quit()
			}()
		})
		if runErr := mygo.App.Run(); runErr != nil {
			log.Fatal(runErr)
		}
		return
	}
	if err != nil {
		// 不重置损坏的历史；将错误留在界面中，原件保持不变。
		log.Printf("load history: %v", err)
	}
	if s != nil {
		if merr := s.MigrationError(); merr != nil {
			// 备份失败时存储已停止写入；这里只做启动日志，界面状态在 ready 中显示。
			log.Printf("Shelf: 迁移前备份失败，已停止写入以免覆盖可恢复原件：%v", merr)
		}
	}
	a := &shelf{store: s, stop: make(chan struct{}), thumbnails: make(map[string]*ui.Bitmap), thumbPending: make(map[string]bool), loadErr: err}
	mygo.App.SetName(appName)
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quitting = true })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		close(a.stop)
		mygo.GlobalShortcut.UnregisterAll()
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
		if a.win != nil {
			a.show(false)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *shelf) ready(show bool) {
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title: appName, Width: panelWidth, Height: panelHeight,
		Hidden: true, Frameless: true, AlwaysOnTop: true, SkipTaskbar: true,
		DisableResize: true, DisableMinimize: true, DisableMaximize: true, DisableFullScreen: true,
		Content: ui.View(a.view),
	})
	a.win.SetVisibleOnAllWorkspaces(true)
	log.Printf("window collectionBehavior=0x%x", mac.JoinFullScreenSpaces(a.win.NativeHandle()))
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
	a.displays = mygo.Screen.Displays()
	mygo.Screen.OnDisplaysChanged(func() {
		a.displays = mygo.Screen.Displays()
		if a.visible {
			a.place(mygo.Screen.CursorScreenPoint())
			a.win.SetPosition(a.x, a.y)
		}
	})
	if a.loadErr != nil {
		a.status = "历史读取失败，未覆盖原文件。请检查 state.json：" + a.loadErr.Error()
	} else if a.store != nil {
		if merr := a.store.MigrationError(); merr != nil {
			a.status = "迁移前备份失败，已停止写入以保护原文件：" + merr.Error()
		}
	}
	if show {
		a.show(false)
	}
	a.startMonitoring()
	log.Print("Shelf ready")
}

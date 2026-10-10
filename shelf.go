//go:build darwin

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/store"
)

// UI、历史与本机 API 由 mygo 主线程串行拥有。
type shelf struct {
	win                                     *mygo.Window
	tray                                    *mygo.Tray
	store                                   *store.Store
	loadErr                                 error
	stop                                    chan struct{}
	quitting, visible, edgeOpened, received bool
	status                                  string
	statusUntil                             time.Time
	x, y                                    int
	displays                                []mygo.Display
	animationStart                          time.Time
	animationFrom, progress                 float32
	clipboardCount                          int64
	lastClipboard                           time.Time
	dragSerial                              int64
	dragging, suppressed                    bool
	edgeSince, leftSince                    time.Time
	thumbnails                              map[string]*ui.Bitmap
	thumbPending                            map[string]bool
}

func (a *shelf) toggle() {
	if a.visible {
		a.hide()
	} else {
		a.show(false)
	}
}

func (a *shelf) place(p mygo.Point) {
	d := mygo.Screen.DisplayNearestPoint(p)
	h := min(panelHeight, max(240, d.WorkArea.Height-24))
	a.win.SetSize(panelWidth, h)
	a.x = d.Bounds.X
	a.y = d.WorkArea.Y + (d.WorkArea.Height-h)/2
}

func (a *shelf) show(edge bool) {
	if a.visible {
		return
	}
	a.place(mygo.Screen.CursorScreenPoint())
	if a.store != nil && a.loadErr == nil {
		a.store.Refresh()
	}
	a.visible, a.edgeOpened, a.received = true, edge, false
	a.animationFrom, a.animationStart = a.progress, time.Now()
	a.win.SetPosition(a.x-int(float32(panelWidth)*(1-a.progress)), a.y)
	a.win.SetOpacity(float64(a.progress))
	a.win.ShowInactive()
	// ShowInactive 不夺焦；仅显示期间注册 Esc，隐藏后立即释放。
	if err := mygo.GlobalShortcut.Register("Escape", a.hide); err != nil {
		a.report(fmt.Errorf("Esc 被其他应用占用，请用快捷键或收起按钮：%w", err))
	}
	a.win.Invalidate()
}

func (a *shelf) hide() {
	if !a.visible {
		return
	}
	a.visible, a.edgeOpened = false, false
	a.animationFrom, a.animationStart = a.progress, time.Now()
	mygo.GlobalShortcut.Unregister("Escape")
	// 当前拖拽不再次触发；松开鼠标或出现新会话后解除。
	a.suppressed = true
}

func (a *shelf) animate(now time.Time) {
	if a.animationStart.IsZero() {
		return
	}
	k := min(float32(now.Sub(a.animationStart))/float32(panelAnimation), 1)
	target := float32(0)
	if a.visible {
		target = 1
	}
	a.progress = a.animationFrom + (target-a.animationFrom)*ui.EaseOut(k)
	a.win.SetPosition(a.x-int((1-a.progress)*float32(panelWidth)), a.y)
	a.win.SetOpacity(float64(a.progress))
	if k >= 1 {
		a.animationStart = time.Time{}
		if !a.visible {
			a.win.Hide()
		}
	}
}

func (a *shelf) startMonitoring() {
	go func() {
		ticker := time.NewTicker(pointerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.stop:
				return
			case now := <-ticker.C:
				mygo.RunOnMain(func() {
					if a.quitting {
						return
					}
					a.animate(now)
					a.monitorDrag(now)
					if now.Sub(a.lastClipboard) >= clipboardInterval {
						a.lastClipboard = now
						a.monitorClipboard()
					}
					if !a.statusUntil.IsZero() && now.After(a.statusUntil) {
						a.status, a.statusUntil = "", time.Time{}
						a.win.Invalidate()
					}
				})
			}
		}
	}()
}

func (a *shelf) monitorClipboard() {
	count := mac.ClipboardChangeCount()
	if count == a.clipboardCount {
		return
	}
	a.clipboardCount = count
	if a.store == nil || a.loadErr != nil {
		return
	}
	// 图片优先，避免截图附带的文本表示造成双条目。
	if png := mygo.Clipboard.ReadImage(); len(png) > 0 {
		a.changed(a.store.AddImage(png))
	} else if text := mygo.Clipboard.ReadText(); text != "" {
		a.changed(a.store.AddText(text))
	}
}

func (a *shelf) monitorDrag(now time.Time) {
	active, serial := mac.DragState()
	if serial != a.dragSerial {
		a.dragSerial = serial
		a.suppressed = false
	}
	a.dragging = active
	if !active {
		a.suppressed = false
		a.edgeSince = time.Time{}
	}
	p := mygo.Screen.CursorScreenPoint()
	nearEdge := false
	for _, d := range a.displays {
		if p.X >= d.Bounds.X && p.X <= d.Bounds.X+edgeWidth && p.Y >= d.Bounds.Y && p.Y < d.Bounds.Y+d.Bounds.Height {
			nearEdge = true
			break
		}
	}
	if active && !a.suppressed && !a.visible && nearEdge {
		if a.edgeSince.IsZero() {
			a.edgeSince = now
		}
		if now.Sub(a.edgeSince) >= edgeDwell {
			a.show(true)
			a.edgeSince = time.Time{}
		}
	} else {
		a.edgeSince = time.Time{}
	}
	if !a.visible || !a.edgeOpened || a.received {
		a.leftSince = time.Time{}
		return
	}
	b := a.win.Bounds()
	inside := p.X >= a.x && p.X < a.x+b.Width+24 && p.Y >= a.y-24 && p.Y < a.y+b.Height+24
	// 动画途中不判离开；松开后的宽限给 native drop 回调时间。
	if (!inside && !nearEdge || !active) && a.animationStart.IsZero() {
		if a.leftSince.IsZero() {
			a.leftSince = now
		}
		if now.Sub(a.leftSince) >= edgeLeaveDelay {
			a.hide()
		}
	} else {
		a.leftSince = time.Time{}
	}
}

func (a *shelf) receive(data transfer.Data) {
	if a.store == nil || a.loadErr != nil {
		return
	}
	var err error
	if paths, e := data.Files(); e == nil && len(paths) > 0 {
		for i, path := range paths {
			paths[i], err = mac.ResolveFilePath(path)
			if err != nil {
				a.report(err)
				return
			}
		}
		err = a.store.AddFiles(paths)
	} else if b, e := data.Read(transfer.PNG); e == nil && len(b) > 0 {
		err = a.store.AddImage(b)
	} else if b, e := data.Read(transfer.Text); e == nil && len(b) > 0 {
		err = a.store.AddText(string(b))
	} else {
		a.report(fmt.Errorf("此内容不能放入托盘；请拖入本地文件、文字或图片"))
		return
	}
	if err == nil {
		a.received = true
		a.status = "已放入 · 拖出后仍会保留"
		a.statusUntil = time.Now().Add(3 * time.Second)
	}
	a.changed(err)
}

func (a *shelf) changed(err error) {
	if err != nil {
		a.report(err)
		return
	}
	a.win.Invalidate()
}
func (a *shelf) report(err error) {
	a.status = "操作失败：" + err.Error()
	a.statusUntil = time.Time{}
	log.Print(a.status)
	if a.win != nil {
		a.win.Invalidate()
	}
}
func (a *shelf) completed(r transfer.Result) {
	if r.Err != nil {
		a.report(r.Err)
		return
	}
	if !r.Canceled && r.Operation != transfer.None {
		a.hide()
	}
}

//go:build darwin

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/settings"
	"shelf/internal/store"
)

// UI、历史与本机 API 由 mygo 主线程串行拥有。
type shelf struct {
	win                                       *mygo.Window
	tray                                      *mygo.Tray
	store                                     *store.Store
	stop                                      chan struct{}
	quitting, visible, edgeOpened, received   bool
	status                                    string
	statusUntil                               time.Time
	x, y                                      int
	dispBounds                                mygo.Rectangle
	frame                                     mygo.Rectangle // 常驻窗口当前实际映射的屏幕矩形（desiredFrame 最近一次应用值）
	settingFrame                              bool
	animationStart                            time.Time
	animationFrom, progress                   float32
	settings                                  settings.Settings
	modules                                   []module
	clip                                      *clipboardModule // nil while clipboard history is off
	dragSerial                                int64
	dragging, suppressed, pointerDrag         bool
	guard                                     dragGuard
	escHeld                                   bool
	stopMouseObservation, stopDragObservation func()
	leftSince                                 time.Time
	h, topInset                               int
	notchX, notchY, notchW, notchH            int
	hasNotch                                  bool
	peeking                                   bool
	peekProgress, peekFrom                    float32
	peekAnimStart                             time.Time
	ignoringMouse                             bool
	thumbnails                                map[string]*ui.Bitmap
	thumbPending                              map[string]bool
	thumbDiscard                              map[string]bool
	thumbActive                               int
}

func (a *shelf) toggle() {
	if a.visible {
		a.hide()
	} else {
		a.show(false)
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
					for _, m := range a.modules {
						m.tick(now)
					}
					if !a.statusUntil.IsZero() && now.After(a.statusUntil) {
						a.status, a.statusUntil = "", time.Time{}
						if a.win != nil {
							a.win.Invalidate()
						}
					}
				})
			}
		}
	}()
}

func (a *shelf) receive(data transfer.Data) {
	if a.store == nil {
		return
	}
	var err error
	if paths, e := data.Files(); e == nil && len(paths) > 0 {
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
	if a.store != nil && len(a.store.Items) > 0 {
		a.invalidateThumbnail(a.store.Items[0].ID)
	}
	if a.win != nil {
		a.win.Invalidate()
	}
}
func (a *shelf) report(err error) {
	a.status = "操作失败：" + err.Error()
	a.statusUntil = time.Time{}
	log.Print(a.status)
	if a.win != nil {
		a.win.Invalidate()
	}
}

// notice adds a startup message to the panel footer without hiding an
// earlier one (settings, shortcut and migration problems can all occur).
func (a *shelf) notice(msg string) {
	log.Print(msg)
	if a.status != "" {
		a.status += "\n"
	}
	a.status += msg
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

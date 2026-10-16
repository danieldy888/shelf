//go:build darwin

package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/settings"
	"shelf/internal/store"
)

// UI、历史与本机 API 由 mygo 主线程串行拥有。
type shelf struct {
	win                                        *mygo.Window
	tray                                       *mygo.Tray
	store                                      *store.Store
	stop                                       chan struct{}
	quitting, visible, edgeOpened, received    bool
	status                                     string
	statusUntil                                time.Time
	x, y                                       int
	dispBounds                                 mygo.Rectangle
	frame                                      mygo.Rectangle // 常驻窗口当前实际映射的屏幕矩形（desiredFrame 最近一次应用值）
	settingFrame                               bool
	animationStart                             time.Time
	animationFrom, progress                    float32
	settings                                   settings.Settings
	modules                                    []module
	clip                                       *clipboardModule  // nil while clipboard history is off
	tab                                        int               // panelTab: which list the expanded panel shows
	search                                     string            // clipboard tab's search text
	reveal                                     string            // row to keep in view after a keyboard move
	focusMode                                  bool              // the window took the keyboard for clipboard search
	prevAppPID                                 int               // app to hand the keyboard back to when focusMode ends
	appNames                                   map[string]string // bundle id → display name, cached for menu labels
	write                                      clipWriter        // where copies go; nil = the system clipboard
	dragSerial                                 int64
	dragging, suppressed, pointerDrag, ownDrag bool
	dataDir                                    string // where state.json and settings.json live
	settingsBroken                             bool   // settings.json unreadable: never overwrite it
	guard                                      dragGuard
	escHeld, spaceHeld                         bool
	selection                                  selection
	stopMouseObservation, stopDragObservation  func()
	leftSince                                  time.Time
	h, topInset                                int
	notchX, notchY, notchW, notchH             int
	hasNotch                                   bool
	peeking                                    bool
	peekProgress, peekFrom                     float32
	peekAnimStart                              time.Time
	island                                     activity  // activity whose wings are shown (ID empty = none)
	islandNext                                 activity  // activity swapped in when the fold completes
	islandK                                    float32   // wings' grow-out progress
	islandFrom                                 float32   // islandK the wings' animation started from
	islandStart                                time.Time // when islandK's animation started (zero = idle)
	islandSwap                                 bool      // folding the old wings shut before showing a new activity
	islandFitKey, islandFitText                string    // trailing-text truncation cache
	ignoringMouse                              bool
	thumbnails                                 map[string]*ui.Bitmap
	shake                                      shakeDetector // 摇晃召唤：拖拽中左右晃动检测
	basket                                     basketState   // 摇晃召唤的拖放篮
	thumbPending                               map[string]bool
	thumbDiscard                               map[string]bool
	thumbActive                                int
}

func (a *shelf) toggle() {
	if a.visible {
		a.hide(hideExplicit)
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
					for _, m := range a.modules {
						m.tick(now)
					}
					// 刘海岛读取各模块刚更新过的活动状态，再推进动画。
					a.islandTick(now)
					a.animate(now)
					a.monitorDrag(now)
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

func (a *shelf) receive(data transfer.Data) error {
	if a.store == nil {
		return errors.New("没有可写入的历史存储")
	}
	var err error
	if paths, e := data.Files(); e == nil && len(paths) > 0 {
		err = a.store.AddFiles(paths)
	} else if b, e := data.Read(transfer.PNG); e == nil && len(b) > 0 {
		err = a.store.AddImage(b)
	} else if b, e := data.Read(transfer.Text); e == nil && len(b) > 0 {
		err = a.store.AddText(string(b))
	} else {
		err = fmt.Errorf("此内容不能放入托盘；请拖入本地文件、文字或图片")
		a.report(err)
		return err
	}
	if err == nil {
		a.received = true
		a.status = "已放入 · 拖出后仍会保留"
		a.statusUntil = time.Now().Add(3 * time.Second)
	}
	a.changed(err)
	return err
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
	a.ownDrag = false
	if r.Err != nil {
		a.report(r.Err)
		return
	}
	if !r.Canceled && r.Operation != transfer.None {
		a.hide(hidePassive)
	}
}

// setClipboardHistory saves the clipboardHistory setting and starts or
// stops the clipboard module at once, without a restart. Disabled while
// settings.json could not be read, so a broken file is never overwritten.
func (a *shelf) setClipboardHistory(on bool) {
	if a.settingsBroken || a.settings.ClipboardHistory == on {
		return
	}
	next := a.settings
	next.ClipboardHistory = on
	if err := settings.Save(a.dataDir, next); err != nil {
		a.report(err)
		return
	}
	a.settings = next
	a.applyClipboardHistory()
	if a.win != nil {
		a.win.Invalidate()
	}
}

// applyClipboardHistory makes the running modules match the setting: the
// clipboard module records while clipboardHistory is on, and stops (its
// poll simply no longer runs) while it is off. Called on the main thread
// from setClipboardHistory and ready.
func (a *shelf) applyClipboardHistory() {
	if a.settings.ClipboardHistory {
		if a.clip == nil {
			a.clip = newClipboardModule(a)
			a.modules = append(a.modules, a.clip)
		}
		return
	}
	if a.clip == nil {
		return
	}
	rest := a.modules[:0]
	for _, m := range a.modules {
		if m == module(a.clip) {
			continue
		}
		rest = append(rest, m)
	}
	a.modules = rest
	a.clip = nil
}

// setClipboardLimit saves the new clipboard text limit and applies it to
// the store at once (the next add trims to it). Disabled while
// settings.json could not be read, so a broken file is never overwritten.
func (a *shelf) setClipboardLimit(n int) {
	if a.settingsBroken {
		return
	}
	if n < 1 {
		n = settings.Default().ClipboardLimit
	}
	next := a.settings
	next.ClipboardLimit = n
	if err := settings.Save(a.dataDir, next); err != nil {
		a.report(err)
		return
	}
	a.settings = next
	if a.store != nil {
		a.store.SetTextLimit(n)
	}
}

// hideExit is how a hide was reached. Restoring the keyboard to the app
// the user copied from is right only for exits the user chose while using
// the shelf — Esc, Enter-copy, the collapse button, the tray/shortcut
// toggle. A click outside or a finished drag-out means the user moved on
// to another app themselves; stealing it back would fight that choice.
type hideExit int

const (
	hideExplicit hideExit = iota // the user dismissed the shelf itself
	hidePassive                  // click elsewhere, a finished drag-out, a flash timer
)

// shouldRestoreApp reports whether hide wants the remembered frontmost app
// re-activated: only when the shelf had the keyboard (focusMode) and the
// exit was the user's own dismiss. Pure; unit-tested.
func (a *shelf) shouldRestoreApp(exit hideExit) bool {
	return a.focusMode && exit == hideExplicit
}

// openClipboardSearch opens the shelf on the clipboard tab with the search
// field focused and ready for typing (the clipboard hotkey). The app is
// activated so the window takes the keyboard, and the app the user came
// from is remembered so hiding hands the keyboard back to it. Only this
// keyboard flow activates the app; hover, drag, tray and service opens keep
// their ShowInactive behaviour.
func (a *shelf) openClipboardSearch() {
	// This open takes the panel over: a queued services flash or a
	// drag-edge auto-collapse must never hide the search underneath.
	a.takeServiceOwnership()
	a.edgeOpened = false
	if a.win == nil {
		return
	}
	if !a.focusMode {
		if pid := mac.FrontmostPID(); pid > 0 && pid != mac.OwnPID() {
			a.prevAppPID = pid
		}
	}
	a.tab, a.search = tabClipboard, ""
	a.focusMode = true
	// The window must sit below the IM candidate band before the search
	// field takes the keyboard — even when the panel was already open
	// and show() returns early. syncMousePassThrough applies windowLevel.
	a.syncMousePassThrough()
	a.show(false)
	mac.ActivateSelf()
	a.win.Focus()
	// 键盘已经在这个窗口里：Esc 与空格交给窗口处理，全局注册会把
	// 按键从搜索框手里抢走。
	a.releaseKeys()
}

// activatePreviousApp hands the keyboard back to the app the user came
// from when the shelf was opened for clipboard search. It runs only while
// this app is still the active one — a user who switched away already
// must not be pulled back — and if the remembered app is gone, Shelf's
// own activation is resigned rather than leaving a hidden app in front.
func (a *shelf) activatePreviousApp(pid int) {
	if pid <= 0 || pid == mac.OwnPID() || !mac.AppActive() {
		return
	}
	if !mac.ActivateApp(pid) {
		mac.DeactivateSelf()
	}
}

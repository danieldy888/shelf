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
	statusUntil, statusShown                   time.Time // statusShown: 首次渲染时刻（toast 生命周期起点）
	statusErr                                  bool      // report() 写入的错误状态（toast 带红点）
	x, y                                       int
	dispBounds                                 mygo.Rectangle
	frame                                      mygo.Rectangle // 常驻窗口当前实际映射的屏幕矩形（desiredFrame 最近一次应用值）
	settingFrame                               bool
	animationStart                             time.Time
	animationFrom, progress                    float32
	openGen                                    int     // counts openings; keys the panel's height animation
	shownH                                     float32 // panel height the shell is drawn at (eases toward panelH)
	settings                                   settings.Settings
	modules                                    []module
	np                                         *nowPlayingModule     // the peek player reads its state
	conv                                       *converter            // image conversion jobs; nil until the app starts
	usage                                      *usageFeature         // nil until the AI usage page registers itself
	clip                                       *clipboardModule      // nil while clipboard history is off
	awake                                      *awakeModule          // nil until the app starts
	pages                                      []panelPage           // registered pages in ear order (built-ins first)
	pagesReady                                 bool                  // built-in pages have been registered
	tab                                        pageID                // current page of the expanded panel (registry in pages.go)
	tabAt                                      time.Time             // when the tab last switched; drives the pages' crossfade
	tabFrom                                    pageID                // the page being left by the last switch, drawn fading out
	search                                     string                // clipboard page's search text
	reveal                                     string                // row to keep in view after a keyboard move
	focusMode                                  bool                  // the window took the keyboard for clipboard search
	prevAppPID                                 int                   // app to hand the keyboard back to when focusMode ends
	appNames                                   map[string]string     // bundle id → display name, cached for menu labels
	appIcons                                   map[string]*ui.Bitmap // bundle id → icon, cached for clipboard rows
	write                                      clipWriter            // where copies go; nil = the system clipboard
	dragSerial                                 int64
	dragging, suppressed, pointerDrag, ownDrag bool
	dataDir                                    string         // where state.json and settings.json live
	settingsBroken                             bool           // settings.json unreadable: never overwrite it
	settingsWin                                *mygo.Window   // the 设置 window; nil while closed
	settingsPrevPID                            int            // app to hand the keyboard back to when it closes
	loginOn                                    bool           // 开机时自动启动，read back each time the window opens
	loginErr                                   string         // last SetOpenAtLogin failure, shown inline
	rn                                         *renameUI      // 重新命名 window session; nil until first use
	settingsScroll                             ui.ScrollState // the Settings window's scroll position
	update                                     *mygo.Update   // version the feed offers, waiting for the user
	updateChecking, updateInstalling           bool
	updateInstalled                            bool    // swapped into place; runs at the next launch
	updateProgress                             float64 // download progress while installing
	updateNext                                 time.Time
	guard                                      dragGuard
	escHeld, spaceHeld, cmdCHeld               bool
	selection                                  selection
	stopMouseObservation, stopDragObservation  func()
	leftSince                                  time.Time
	h, topInset                                int
	notchX, notchY, notchW, notchH             int
	hasNotch                                   bool
	peeking                                    bool
	peekProgress, peekFrom                     float32
	peekAnimStart                              time.Time
	ignoringMouse                              bool
	thumbnails                                 map[string]*ui.Bitmap
	shake                                      shakeDetector // 摇晃召唤：拖拽中左右晃动检测
	basket                                     basketState   // 摇晃召唤的拖放篮
	thumbPending                               map[string]bool
	thumbDiscard                               map[string]bool
	thumbActive                                int

	// 拖出后从托盘移除：当前拖出会话从托盘带走的项目（剪贴板页为 nil），
	// 以及已拖出、等待最终移除的托盘项目及其放下时刻。
	dragOutIDs []string
	leaving    map[string]time.Time
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
					a.animate(now)
					a.monitorDrag(now)
					// 内容高度随项目数/正在播放卡片/搜索状态变化：
					// desiredFrame 未变时是恒等比较，不触发原生缩放。
					a.syncWindowFrame()
					a.tickUpdates(now)
					if !a.statusUntil.IsZero() && now.After(a.statusUntil) {
						a.status, a.statusUntil, a.statusShown, a.statusErr = "", time.Time{}, time.Time{}, false
						if a.win != nil {
							a.win.Invalidate()
						}
					}
					// statusToast 给无期限状态（报错、启动提示）计时：
					// 从首次渲染起 toastLife 后清掉。
					if a.status != "" && a.statusUntil.IsZero() && !a.statusShown.IsZero() &&
						now.After(a.statusShown.Add(toastLife)) {
						a.status, a.statusShown, a.statusErr = "", time.Time{}, false
						if a.win != nil {
							a.win.Invalidate()
						}
					}
				})
			}
		}
	}()
}

// toastStatus posts a transient status pill at the panel's bottom centre:
// msg until the deadline (or toastLife from its first paint when until is
// zero); isErr gives it the small red dot of error toasts.
func (a *shelf) toastStatus(msg string, until time.Time, isErr bool) {
	a.status = msg
	a.statusUntil, a.statusShown, a.statusErr = until, time.Time{}, isErr
	if a.win != nil {
		a.win.Invalidate()
	}
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
		a.toastStatus("已放入 · 拖出后仍会保留", time.Now().Add(3*time.Second), false)
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
	a.toastStatus("操作失败："+err.Error(), time.Time{}, true)
	log.Print(a.status)
	if a.win != nil {
		a.win.Invalidate()
	}
}

// notice adds a startup message to the status toast without hiding an
// earlier one (settings, shortcut and migration problems can all occur).
// Notices are problems, so they show the error dot and live a little
// longer than a confirmation.
func (a *shelf) notice(msg string) {
	log.Print(msg)
	if a.status != "" {
		a.status += "\n"
	}
	a.toastStatus(a.status+msg, time.Now().Add(8*time.Second), true)
}
func (a *shelf) completed(r transfer.Result) {
	a.ownDrag = false
	ids := a.dragOutIDs
	a.dragOutIDs = nil
	if r.Err != nil {
		a.report(r.Err)
		return
	}
	if !r.Canceled && r.Operation != transfer.None {
		a.leaveTray(ids, time.Now())
		a.hide(hidePassive)
	}
}

// applySettings is the only path that changes a preference: it saves next
// to settings.json first and only then replaces the running copy, so a
// failed write leaves the running setting unchanged and a broken file is
// never overwritten. The tray menus and the Settings window both go
// through it, so they can never disagree. After the save both windows
// repaint, which keeps an open Settings window in sync with menu choices.
// Side effects that follow a changed field (module start/stop, store
// limit, an immediate cleanup pass) run in each setter after this returns.
func (a *shelf) applySettings(next settings.Settings) bool {
	if a.settingsBroken {
		return false
	}
	if err := settings.Save(a.dataDir, next); err != nil {
		a.report(err)
		return false
	}
	a.settings = next
	if a.win != nil {
		a.win.Invalidate()
	}
	a.repaintSettings()
	return true
}

// setClipboardHistory saves the clipboardHistory setting and starts or
// stops the clipboard module at once, without a restart. Disabled while
// settings.json could not be read, so a broken file is never overwritten.
func (a *shelf) setClipboardHistory(on bool) {
	if a.settings.ClipboardHistory == on {
		return
	}
	next := a.settings
	next.ClipboardHistory = on
	if !a.applySettings(next) {
		return
	}
	a.applyClipboardHistory()
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
	if n < 1 {
		n = settings.Default().ClipboardLimit
	}
	if a.settings.ClipboardLimit == n {
		return
	}
	next := a.settings
	next.ClipboardLimit = n
	if !a.applySettings(next) {
		return
	}
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
	a.switchTab(pageClipboard) // leaving pageUsage retires its work the same way setTab does
	a.search = ""
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

// switchTab changes the current page without a fade: the search hotkey's
// direct jump. setTab keeps its own copy because it also records tabFrom
// for the crossfade; both retire the AI usage page's background work the
// moment it is left.
func (a *shelf) switchTab(id pageID) {
	if a.tab == pageUsage && id != pageUsage && a.usage != nil {
		a.usage.stopWork()
	}
	a.tab = id
}

// Keyboard handback seams: tests stub these (mac's own calls need the app
// running) to prove who gets the keyboard when a keyboard-taking surface
// closes.
var (
	appIsActive = mac.AppActive
	activateApp = mac.ActivateApp
	resignSelf  = mac.DeactivateSelf
)

// activatePreviousApp hands the keyboard back to the app the user came
// from when the shelf was opened for clipboard search. It runs only while
// this app is still the active one — a user who switched away already
// must not be pulled back — and if the remembered app is gone, Shelf's
// own activation is resigned rather than leaving a hidden app in front.
func (a *shelf) activatePreviousApp(pid int) { a.handBack(pid, true) }

// handBackApp is activatePreviousApp without the resign fallback: the
// Settings window hands the keyboard back the same way, but must never
// hide this app (NSApp hide would take the notch window with it).
func (a *shelf) handBackApp(pid int) { a.handBack(pid, false) }

func (a *shelf) handBack(pid int, resignWhenGone bool) {
	if pid <= 0 || pid == mac.OwnPID() || !appIsActive() {
		return
	}
	if !activateApp(pid) && resignWhenGone {
		resignSelf()
	}
}

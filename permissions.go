//go:build darwin

package main

// The one list of system permissions Shelf needs and the one entry point
// that walks the user through granting them. The tray menu, the Settings
// window's 系统权限 group and the 全部开启 flow all read systemPermissions;
// none of them duplicates a title, an order or a probe.
//
// Nothing here changes a permission by itself: the enabled probes are
// read-only (AXIsProcessTrusted, CGPreflightScreenCaptureAccess), and only
// the user-initiated request functions reach the existing paths that clear
// a stale ad-hoc record while the grant is absent and then open the
// matching System Settings pane.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
)

const (
	accessibilityPermissionID = "accessibility"
	screenCapturePermissionID = "screencapture"

	// permissionPollInterval is how often the flow re-reads the
	// Accessibility grant while the user is in System Settings.
	permissionPollInterval = time.Second
	// permissionScreenHandoff is how long the screen-recording step keeps
	// its 「正在开启」 heading before it turns into 「还差最后一步」. The
	// request opens the pane first; the pause lets the user see which step
	// they are on while the pane comes forward.
	permissionScreenHandoff = 1500 * time.Millisecond
)

// Seams for the headless tests. The probes never prompt; the request
// functions are the user-initiated paths.
var (
	accessibilityProbe = mac.AccessibilityTrusted
	// ScreenCapturePreflight, not ScreenCaptureAllowed: showing state must
	// never run the stale-record reset that belongs to the request path.
	screenCaptureProbe   = mac.ScreenCapturePreflight
	screenCaptureRequest = mac.RequestScreenCapture
	// screenCaptureWindowTitles is a non-cached, non-prompting hint: when
	// WindowServer returns other apps' window titles, the screen-recording
	// grant is in effect even though this process's preflight answer is
	// cached false. A false answer only means "not proven".
	screenCaptureWindowTitles = mac.ScreenCaptureGrantedByWindowTitles

	permissionAfterFunc = func(d time.Duration, fn func()) permissionTimer {
		return time.AfterFunc(d, fn)
	}
	permissionDispatch = mygo.RunOnMain
)

type permissionTimer interface {
	Stop() bool
}

// systemPermission is one row of the unified entry: the probe that reports
// the live state and the user-initiated request for that one permission.
type systemPermission struct {
	id      string
	title   string
	detail  string
	icon    *ui.SVG
	enabled func() bool
	request func() bool
}

// systemPermissions is the single source of the list, in the order the
// 全部开启 flow walks it.
func systemPermissions() []systemPermission {
	return []systemPermission{
		{
			id:     accessibilityPermissionID,
			title:  "刘海拖放保护 · 辅助功能",
			detail: "拖文件到刘海时，挡住系统弹出的桌面栏",
			icon:   accessSymbol,
			enabled: func() bool {
				return accessibilityProbe()
			},
			request: func() bool {
				return accessibilityRequest()
			},
		},
		{
			id:     screenCapturePermissionID,
			title:  "框选文字 · 屏幕录制",
			detail: "只在你框选时截取所选区域，在本机识别",
			icon:   ocrSymbol,
			enabled: func() bool {
				return screenCaptureProbe()
			},
			request: func() bool {
				return screenCaptureRequest()
			},
		},
	}
}

func permissionByID(id string) (systemPermission, bool) {
	for _, p := range systemPermissions() {
		if p.id == id {
			return p, true
		}
	}
	return systemPermission{}, false
}

func missingSystemPermissions() []systemPermission {
	var missing []systemPermission
	for _, p := range systemPermissions() {
		if !p.enabled() {
			missing = append(missing, p)
		}
	}
	return missing
}

// accessSymbol is the hand glyph for the Accessibility row, drawn with the
// same 24pt stroke grid as the other panel symbols.
var accessSymbol = symbol(`<path d="M8 12V6.5a1.5 1.5 0 0 1 3 0V11"/><path d="M11 11V4.5a1.5 1.5 0 0 1 3 0V11"/><path d="M14 11V7a1.5 1.5 0 0 1 3 0v6a7 7 0 0 1-7 7h-1a6 6 0 0 1-5-2.7L3.6 15a1.5 1.5 0 0 1 2.4-1.8L8 14"/>`)

// permissionMenuItem is the tray item that replaces the old single
// permission entry. It counts what is missing at menu-build time, because
// the menu is rebuilt on every right-click.
func (a *shelf) permissionMenuItem() *mygo.MenuItem {
	missing := missingSystemPermissions()
	if len(missing) == 0 {
		return &mygo.MenuItem{Label: "系统权限已全部开启", Disabled: true}
	}
	return &mygo.MenuItem{
		Label: fmt.Sprintf("开启系统权限（还差 %d 项）…", len(missing)),
		Click: func(*mygo.MenuItem, *mygo.Window) { a.showPermissions() },
	}
}

// showPermissions opens (or focuses) the Settings window and immediately
// starts the guided flow.
func (a *shelf) showPermissions() {
	a.openSettings()
	a.startPermissionFlow()
}

// permissionStage is the guided flow's state. The zero value is idle.
type permissionStage int

const (
	permissionIdle permissionStage = iota
	// permissionWaiting: the current item's request has been made and the
	// flow is waiting for its grant. Accessibility is polled; the screen
	// recording step hands over to permissionRelaunch after a pause.
	permissionWaiting
	// permissionRelaunch: screen recording was requested, or is already
	// granted but invisible to the cached preflight, and Shelf must be
	// relaunched before the grant takes effect.
	permissionRelaunch
)

// permissionFlow is the state of the 全部开启 walk. ids is the snapshot of
// what was missing when the flow started, so a grant landing mid-flow
// cannot shift the step index.
type permissionFlow struct {
	stage         permissionStage
	ids           []string
	step          int
	screenGranted bool
	timer         permissionTimer
}

// stopPermissionFlow ends the walk and leaves no timer behind. Closing the
// Settings window calls it too.
func (a *shelf) stopPermissionFlow() {
	if a.perm.timer != nil {
		a.perm.timer.Stop()
		a.perm.timer = nil
	}
	a.perm = permissionFlow{}
}

// startPermissionFlow begins (or restarts) the walk over the permissions
// that are missing right now. Already-granted permissions are skipped by
// beginPermissionStep and never requested, so their TCC record is never
// cleared.
func (a *shelf) startPermissionFlow() {
	a.stopPermissionFlow()
	missing := missingSystemPermissions()
	if len(missing) == 0 {
		a.repaintSettings()
		return
	}
	ids := make([]string, len(missing))
	for i, p := range missing {
		ids[i] = p.id
	}
	a.perm = permissionFlow{stage: permissionWaiting, ids: ids}
	a.repaintSettings()
	a.beginPermissionStep()
}

func (a *shelf) permissionFlowCurrent() (systemPermission, bool) {
	if a.perm.step < 0 || a.perm.step >= len(a.perm.ids) {
		return systemPermission{}, false
	}
	return permissionByID(a.perm.ids[a.perm.step])
}

// beginPermissionStep skips everything already granted, then issues the
// current item's request and arms its wait.
func (a *shelf) beginPermissionStep() {
	for a.perm.step < len(a.perm.ids) {
		p, ok := a.permissionFlowCurrent()
		if !ok {
			a.perm.step++
			continue
		}
		if p.enabled() {
			a.perm.step++
			continue
		}
		p.request()
		a.repaintSettings()
		if p.id == screenCapturePermissionID {
			a.armPermissionTimer(permissionScreenHandoff)
		} else {
			a.armPermissionTimer(permissionPollInterval)
		}
		return
	}
	// Nothing left: the flow ends in the ordinary all-granted state.
	a.stopPermissionFlow()
	a.repaintSettings()
}

func (a *shelf) armPermissionTimer(d time.Duration) {
	if a.perm.timer != nil {
		a.perm.timer.Stop()
	}
	a.perm.timer = permissionAfterFunc(d, func() {
		permissionDispatch(func() { a.permissionTimerFired() })
	})
}

// permissionTimerFired advances the walk on the UI thread. A callback that
// lands after 停止 or after the window closed finds the flow idle and does
// nothing.
func (a *shelf) permissionTimerFired() {
	a.perm.timer = nil
	switch a.perm.stage {
	case permissionWaiting:
		p, ok := a.permissionFlowCurrent()
		if !ok {
			a.stopPermissionFlow()
			a.repaintSettings()
			return
		}
		if p.id == screenCapturePermissionID {
			a.perm.stage = permissionRelaunch
			a.perm.screenGranted = screenCaptureWindowTitles()
			a.repaintSettings()
			a.armPermissionTimer(permissionPollInterval)
			return
		}
		if p.enabled() {
			a.perm.step++
			a.beginPermissionStep()
			return
		}
		a.armPermissionTimer(permissionPollInterval)
	case permissionRelaunch:
		if !a.perm.screenGranted && screenCaptureWindowTitles() {
			a.perm.screenGranted = true
			a.repaintSettings()
		}
		a.armPermissionTimer(permissionPollInterval)
	}
}

// relaunchPlan is how the Settings window restarts this exact app after the
// old process has released the data-directory lock.
type relaunchPlan struct {
	Exec string
	Args []string
}

// relaunchPlanFor builds a detached /bin/sh wait-then-exec: the new process
// starts only after pid exits, so the single-instance flock is free and the
// user never sees the 「Shelf 已在运行」 dialog. The original arguments are
// preserved, including --data-dir.
func relaunchPlanFor(exe string, args []string, pid int) relaunchPlan {
	var b strings.Builder
	fmt.Fprintf(&b, "while kill -0 %d 2>/dev/null; do sleep 0.1; done; sleep 0.3; exec %s", pid, shellQuote(exe))
	for _, arg := range args {
		b.WriteByte(' ')
		b.WriteString(shellQuote(arg))
	}
	return relaunchPlan{Exec: "/bin/sh", Args: []string{"-c", b.String()}}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Seams for the relaunch tests: nothing in a test may start a process or
// quit the app.
var (
	relaunchExecutable = os.Executable
	relaunchStart      = func(p relaunchPlan) error {
		cmd := exec.Command(p.Exec, p.Args...)
		// Detach from this process so quitting the app does not take the
		// waiter down with it.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		return cmd.Start()
	}
	relaunchQuit = mygo.App.Quit
)

// relaunchShelf restarts Shelf with the same arguments so the cached
// screen-recording answer is re-read. It starts the waiter first and quits
// only once that succeeded; a failure stays visible in the window.
func (a *shelf) relaunchShelf() {
	exe, err := relaunchExecutable()
	if err != nil {
		a.report(err)
		return
	}
	plan := relaunchPlanFor(exe, os.Args[1:], os.Getpid())
	if err := relaunchStart(plan); err != nil {
		a.report(err)
		return
	}
	a.stopPermissionFlow()
	relaunchQuit()
}

// The palette for the permission rows. Orange matches the panel's warning
// tone; green is the settings theme's success color.
var permWarn = ui.Hex("#FF9F0A")

// settingsPermissionsSection is the 系统权限 group, first in the window.
// It is never tied to settings.json, so it keeps working while the file is
// broken — exactly like the old 刘海拖放保护 row did.
func (a *shelf) settingsPermissionsSection(c *ui.Context) {
	permissions := systemPermissions()
	missing := 0
	for _, p := range permissions {
		if !p.enabled() {
			missing++
		}
	}
	title, caption := fmt.Sprintf("Shelf 需要 %d 项系统权限", len(permissions)), fmt.Sprintf("还差 %d 项", missing)
	if missing == 0 {
		caption = "已全部开启"
	}
	if a.perm.stage == permissionWaiting {
		title = fmt.Sprintf("正在开启：第 %d 项，共 %d 项", a.perm.step+1, len(a.perm.ids))
		caption = "在「系统设置」里打开 Shelf 的开关，这里会自动往下走"
	} else if a.perm.stage == permissionRelaunch {
		title = "还差最后一步"
		caption = "屏幕录制要重新打开 Shelf 才生效"
	}
	settingsSection(c, "系统权限", "第一次使用，或在系统设置里关掉过开关时，在这里重新开启。", false, func() {
		ui.Row(c.Key("perm-head")).AlignItems(ui.Center).Gap(10).MinHeight(48).Padding(0, 12).FillWidth().Children(func() {
			ui.Column(c.Key("perm-head-k")).Gap(2).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, title).SingleLine().FontSize(13).FontWeight(600).TextColor(setInk)
				ui.Text(c, caption).FontSize(11).TextColor(setInk3)
			})
			switch a.perm.stage {
			case permissionRelaunch:
				settingsAccentButton(c, "relaunch", "重新打开 Shelf", func() { a.relaunchShelf() })
			case permissionWaiting:
				settingsQuietButton(c, "perm-stop", "停止", func() {
					a.stopPermissionFlow()
					a.repaintSettings()
				})
			default:
				if missing > 0 {
					settingsAccentButton(c, "perm-all", "全部开启…", func() { a.startPermissionFlow() })
				}
			}
		})
		for _, p := range permissions {
			settingsSep(c, "perm-"+p.id)
			a.settingsPermissionRow(c, p)
		}
	})
}

func (a *shelf) settingsPermissionRow(c *ui.Context, p systemPermission) {
	enabled := p.enabled()
	current, currentOK := a.permissionFlowCurrent()
	isCurrent := currentOK && current.id == p.id
	waiting := a.perm.stage == permissionWaiting && isCurrent
	relaunch := a.perm.stage == permissionRelaunch && p.id == screenCapturePermissionID
	ui.Row(c.Key("perm-row-"+p.id)).AlignItems(ui.Center).Gap(10).MinHeight(48).Padding(0, 12).FillWidth().Children(func() {
		chip := ui.Column(c.Key("perm-ic-"+p.id)).Size(22, 22).Radius(6).
			Background(ui.RGBA(255, 255, 255, 0.08)).Center()
		chip.Children(func() {
			ui.Icon(c, p.icon).Size(13, 13).TextColor(setInk2)
		})
		ui.Column(c.Key("perm-k-" + p.id)).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, p.title).SingleLine().FontSize(13).TextColor(setInk)
			ui.Text(c, p.detail).FontSize(11).TextColor(setInk3)
		})
		switch {
		case enabled:
			ui.Text(c, "✓ 已开启").SingleLine().FontSize(12).TextColor(settingsTheme.Success)
		case waiting:
			ui.Row(c.Key("perm-wait-" + p.id)).AlignItems(ui.Center).Gap(5).Children(func() {
				ui.Box(c.Key("perm-dot-"+p.id)).Size(6, 6).Radius(3).Background(permWarn)
				ui.Text(c, "等你在系统设置里打开").SingleLine().FontSize(11).TextColor(permWarn)
			})
		case relaunch:
			if a.perm.screenGranted {
				ui.Text(c, "重新打开后生效").SingleLine().FontSize(11).TextColor(permWarn)
			} else {
				ui.Row(c.Key("perm-wait-" + p.id)).AlignItems(ui.Center).Gap(5).Children(func() {
					ui.Box(c.Key("perm-dot-"+p.id)).Size(6, 6).Radius(3).Background(permWarn)
					ui.Text(c, "等你在系统设置里打开").SingleLine().FontSize(11).TextColor(permWarn)
				})
			}
		case a.perm.stage == permissionIdle:
			ui.Row(c.Key("perm-off-" + p.id)).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "未开启").SingleLine().FontSize(12).TextColor(setInk3)
				if ui.Button(c, "开启…").FontSize(12).Label("开启 " + p.title).Clicked() {
					p.request()
					a.repaintSettings()
				}
			})
		default:
			ui.Text(c, "未开启").SingleLine().FontSize(12).TextColor(setInk3)
		}
	})
}

// settingsAccentButton is the mock's compact accent pill used by the
// permission header.
func settingsAccentButton(c *ui.Context, key, label string, fn func()) {
	b := ui.ButtonBase(c.Key("perm-"+key)).Height(24).Radius(12).Padding(0, 10, 0, 10).
		Background(settingsTheme.Accent).Center().Label(label)
	if b.Pressed() {
		b.Background(settingsTheme.AccentPressed)
	} else if b.Hovered() {
		b.Background(settingsTheme.AccentHover)
	}
	b.Children(func() {
		ui.Text(c, label).SingleLine().FontSize(12).FontWeight(600).TextColor(ui.RGB(255, 255, 255))
	})
	if b.Clicked() {
		fn()
	}
}

// settingsQuietButton is the same pill without the accent fill, for 停止.
func settingsQuietButton(c *ui.Context, key, label string, fn func()) {
	b := ui.ButtonBase(c.Key("perm-"+key)).Height(24).Radius(12).Padding(0, 10, 0, 10).
		Background(ui.RGBA(255, 255, 255, 0.10)).Center().Label(label)
	if b.Hovered() {
		b.Background(ui.RGBA(255, 255, 255, 0.16))
	}
	b.Children(func() {
		ui.Text(c, label).SingleLine().FontSize(12).FontWeight(600).TextColor(setInk)
	})
	if b.Clicked() {
		fn()
	}
}

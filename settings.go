//go:build darwin

package main

// The Settings window gathers every preference the tray submenus and
// settings.json spread over the app into one normal titled window. Its
// controls call the same setters the menus do — applySettings is the single
// save-and-apply path — so window and menu can never diverge; a change made
// in one place repaints the other while it is open.

import (
	"fmt"
	"runtime/debug"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/settings"
)

// Seams around app-level calls so the headless view tests exercise the real
// controls without a running application.
var (
	openAtLogin          = mygo.App.OpenAtLogin
	setOpenAtLogin       = mygo.App.SetOpenAtLogin
	openPath             = mygo.Shell.OpenPath
	accessibilityRequest = mac.RequestAccessibility
	appVersionProbe      = mygo.App.Version
)

// settingsTheme is the one dark theme the window repaints with: Shelf's UI
// lives on the pure black notch shell, so Settings is always dark no matter
// the system appearance. The palette reuses panel.go's colors.
var settingsTheme = ui.DarkTheme()

// openSettings shows the Settings window, creating it the first time, or
// re-focuses the one already open (single instance). The window is a normal
// titled window — not the notch panel — centered on the cursor's display.
// Opening takes the app active and the keyboard, exactly like the
// clipboard-search open: the shelf gives its global Esc/Space back while
// the window is key, and closing hands the keyboard to wherever it belongs
// (settingsReleaseKeyboard) without ever hiding this app — NSApp hide would
// take the notch window away too. The open-at-login state is read back from
// the system every time the window opens or is re-focused, so the switch
// reflects changes the user made in System Settings too.
func (a *shelf) openSettings() {
	if a.settingsWin != nil {
		a.loginOn, a.loginErr = openAtLogin(), ""
		a.repaintSettings()
		mac.ActivateSelf()
		a.settingsWin.Show()
		a.settingsWin.Focus()
		return
	}
	a.settingsTakeKeyboard()
	a.loginOn, a.loginErr = openAtLogin(), ""
	frame := settingsFrame(mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea)
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: appName + " 设置", Width: settingsWidth, Height: settingsHeight,
		X: frame.X, Y: frame.Y,
		Hidden: true, BackgroundColor: "#000000",
		DisableResize: true, DisableMaximize: true, DisableFullScreen: true,
		Content: ui.View(a.settingsView),
	})
	w.SetBounds(frame)
	w.OnClosed(a.settingsClosed)
	a.settingsWin = w
	w.Show()
	mac.ActivateSelf()
	w.Focus()
}

// settingsTakeKeyboard prepares the handover when the Settings window opens:
// the shelf releases Esc and Space (the window's own Esc/⌘W and its controls
// handle them), and the app the user came from is remembered so closing can
// give the keyboard back.
func (a *shelf) settingsTakeKeyboard() {
	if pid := mac.FrontmostPID(); pid > 0 && pid != mac.OwnPID() {
		a.settingsPrevPID = pid
	}
	a.releaseKeys()
}

// settingsClosed runs when the Settings window has closed: the pointer goes
// first (a repaint must never touch a destroyed window), then the keyboard
// goes back to whoever had it before the window opened. A quit closes the
// window too; nothing may be re-registered or re-activated then.
func (a *shelf) settingsClosed() {
	a.settingsWin = nil
	if a.quitting {
		return
	}
	a.settingsReleaseKeyboard()
}

// settingsKeyboardExit is who gets the keyboard when the Settings window
// closes: the panel when it holds it for clipboard search, the shelf itself
// (Esc/Space again) when the panel is merely open, else the app the user
// came from. Pure; unit-tested.
type settingsKeyboardExit int

const (
	settingsToPanel settingsKeyboardExit = iota
	settingsToShelf
	settingsToPrevApp
)

func (a *shelf) settingsKeyboardExit() settingsKeyboardExit {
	switch {
	case a.focusMode:
		return settingsToPanel
	case a.visible:
		return settingsToShelf
	default:
		return settingsToPrevApp
	}
}

// settingsReleaseKeyboard gives the keyboard back when the Settings window
// closes, and never hides this app: an NSApp hide would take the notch
// window with it.
func (a *shelf) settingsReleaseKeyboard() {
	prev := a.settingsPrevPID
	a.settingsPrevPID = 0
	switch a.settingsKeyboardExit() {
	case settingsToPanel:
		// Clipboard search: the panel's search field must keep the
		// keyboard, so the app we came from must not take it back.
		if a.win != nil {
			a.win.Focus()
		}
		return
	case settingsToShelf:
		a.holdKeys()
	}
	a.handBackApp(prev)
}

// repaintSettings redraws the Settings window when it is open, used after a
// change that did not go through applySettings (login item, accessibility).
func (a *shelf) repaintSettings() {
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
}

// settingsFrame centers the settings window inside the cursor display's
// work area (menu bar and Dock stay clear). Pure; unit-tested.
func settingsFrame(work mygo.Rectangle) mygo.Rectangle {
	x := work.X + (work.Width-settingsWidth)/2
	y := work.Y + (work.Height-settingsHeight)/2
	x = min(max(x, work.X), max(work.X, work.X+work.Width-settingsWidth))
	y = min(max(y, work.Y), max(work.Y, work.Y+work.Height-settingsHeight))
	return mygo.Rectangle{X: x, Y: y, Width: settingsWidth, Height: settingsHeight}
}

// settingsView draws the whole Settings window each frame: sections of
// labeled rows on the black shell, then a footer with the version and the
// data-folder button. While settings.json is broken a banner explains it
// and every preference control is disabled so the file is never rewritten.
func (a *shelf) settingsView(c *ui.Context) {
	c.SetTheme(settingsTheme)
	c.Root().Background(ui.Hex("#000000"))
	if a.settingsWin != nil {
		w := a.settingsWin
		c.OnShortcut(0, ui.KeyEscape, w.Close)
		c.OnShortcut(ui.Cmd, ui.KeyW, w.Close)
	}
	ui.Scroll(c.Key("settings")).Fill().TrackScroll(&a.settingsScroll).Children(func() {
		ui.Column(c.Key("sections")).Padding(16, 18, 12, 18).Gap(16).FillWidth().Children(func() {
			if a.settingsBroken {
				ui.Column(c.Key("broken")).Padding(10, 12).Gap(4).Radius(8).
					Background(ui.RGBA(255, 69, 58, 0.14)).Children(func() {
					ui.Text(c, "settings.json 无法读取").FontSize(12).FontWeight(600).TextColor(settingsTheme.Danger)
					ui.Text(c, "以下设置已按默认值运行，且被锁定以免覆盖原文件。请备份并修复数据文件夹中的 settings.json 后重新打开 Shelf。").
						FontSize(11).TextColor(textMuted)
				})
			}
			a.settingsGeneralSection(c)
			a.settingsClipboardSection(c)
			a.settingsStorageSection(c)
			a.settingsNowPlayingSection(c)
			a.settingsShortcutsSection(c)
			ui.Divider(c).Background(hairline)
			a.settingsFooter(c)
		})
	})
}

// settingsSection renders one section: a muted Chinese heading over a dark
// card holding the section's rows. disabled greys and locks the whole card.
func settingsSection(c *ui.Context, title string, disabled bool, fn func()) {
	ui.Column(c.Key("sec-" + title)).Gap(6).FillWidth().Children(func() {
		ui.Text(c, title).SingleLine().FontSize(12).FontWeight(600).TextColor(textMuted).Padding(0, 0, 0, 2)
		ui.Column(c.Key("card")).Gap(9).Padding(11, 13).Radius(10).
			Disabled(disabled).Background(cardSurface).FillWidth().Children(fn)
	})
}

// settingsToggle is one switch row: the label on the left, a macOS-style
// switch on the right. The row itself is the toggle — a click anywhere on
// it flips the switch — and the inner switch mirrors the same value, so the
// two bindings can never fight (a click has exactly one target). apply
// receives the new state only when the user actually changed it.
func settingsToggle(c *ui.Context, key, label string, on bool, apply func(bool)) {
	row := ui.ToggleBase(c.Key("toggle-"+key), &on).FillWidth().AlignItems(ui.Center).Gap(10).Padding(1, 0).Label(label)
	var sw ui.Element
	row.Children(func() {
		ui.Text(c, label).SingleLine().FontSize(13).TextColor(textPrimary).Grow(1)
		sw = ui.Switch(c, &on).Label(label)
	})
	if row.Changed() || sw.Changed() {
		apply(on)
	}
}

// settingsRow is a plain label/value line inside a section.
func settingsRow(c *ui.Context, key, label string, fn func()) {
	ui.Row(c.Key("row-" + key)).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
		ui.Text(c, label).SingleLine().FontSize(13).TextColor(textPrimary).Grow(1)
		fn()
	})
}

// choiceSegments turns an integer preference and its usual choices into a
// segmented control's data: labels, the values they stand for (sorted, with
// the current value inserted if the user set something the presets do not
// offer), and the selected index. Pure; unit-tested.
func choiceSegments(current int, choices []int, format func(int) string) (labels []string, values []int, selected int) {
	values = slices.Clone(choices)
	if !slices.Contains(values, current) {
		values = append(values, current)
	}
	slices.Sort(values)
	selected = slices.Index(values, current)
	for _, v := range values {
		labels = append(labels, format(v))
	}
	return labels, values, selected
}

// settingsGeneralSection: 开机时自动启动 and 刘海拖放保护. Neither lives in
// settings.json, so the section stays editable while the file is broken.
func (a *shelf) settingsGeneralSection(c *ui.Context) {
	settingsSection(c, "通用", false, func() {
		settingsToggle(c, "login", "开机时自动启动", a.loginOn, a.applyOpenAtLogin)
		if a.loginErr != "" {
			ui.Text(c, a.loginErr).FontSize(11).TextColor(settingsTheme.Danger).Padding(0, 0, 0, 1)
		}
		settingsRow(c, "guard", "刘海拖放保护", func() {
			if mac.AccessibilityTrusted() {
				ui.Text(c, "已开启").SingleLine().FontSize(12).TextColor(textMuted)
				return
			}
			if ui.Button(c, "开启…").Label("开启刘海拖放保护").Clicked() {
				accessibilityRequest()
				a.repaintSettings()
			}
		})
		ui.Text(c, "拖拽靠近刘海时把指针挡在托盘接收区内；需要辅助功能授权，每次更新后要重新开启。").
			FontSize(11).TextColor(textMuted)
		// 自动检查更新存 settings.json，文件损坏时随其余偏好一起锁定；
		// 登录项与拖放保护不依赖该文件，保持可用。
		ui.Column(c.Key("autocheck")).Disabled(a.settingsBroken).FillWidth().Children(func() {
			settingsToggle(c, "autocheck", "自动检查更新", a.settings.AutoCheckUpdates, a.setAutoCheckUpdates)
		})
		ui.Text(c, "开启后每天向 GitHub Releases 查询一次新版本（不发送任何个人数据）；关闭后仍可用菜单里的「检查更新…」。").
			FontSize(11).TextColor(textMuted)

	})
}

// applyOpenAtLogin switches the login item through SMAppService (macOS 13+)
// and keeps the shown state and any failure visible inline — running from
// outside a signed app bundle, for example, reports the error here instead
// of silently reverting the switch. Whatever the call returned, the switch
// then shows what the system actually reports (an SMAppService registration
// may still need the user's approval in System Settings), so it can never
// claim a state that is not real.
func (a *shelf) applyOpenAtLogin(on bool) {
	err := setOpenAtLogin(on)
	a.loginOn = openAtLogin()
	if err != nil {
		a.loginErr = err.Error()
	} else {
		a.loginErr = ""
	}
	a.repaintSettings()
}

// settingsClipboardSection: 记录剪贴板, the 保留条数 segments and the
// 排除的应用 list, all through the same setters the tray menu uses.
func (a *shelf) settingsClipboardSection(c *ui.Context) {
	settingsSection(c, "剪贴板", a.settingsBroken, func() {
		settingsToggle(c, "clip", "记录剪贴板", a.settings.ClipboardHistory, a.setClipboardHistory)
		limit := a.settings.ClipboardLimit
		if limit < 1 {
			limit = settings.Default().ClipboardLimit
		}
		labels, values, selected := choiceSegments(limit, clipboardLimitChoices, func(n int) string {
			return fmt.Sprintf("%d 条", n)
		})
		settingsRow(c, "limit", "保留条数", func() {
			if sg := ui.Segmented(c, &selected, labels...).Label("保留条数"); sg.Changed() {
				a.setClipboardLimit(values[selected])
			}
		})
		ui.Text(c, "排除的应用").SingleLine().FontSize(11).TextColor(textMuted)
		if len(a.settings.ExcludedApps) == 0 {
			ui.Text(c, "无 · 右键剪贴板条目可将来源应用加入此列表").FontSize(11).TextColor(textMuted)
			return
		}
		for _, id := range a.settings.ExcludedApps {
			bundleID := id
			ui.Row(c.Key("excl-" + bundleID)).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
				ui.Column(c.Key("id")).Gap(1).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, a.appName(bundleID)).SingleLine().FontSize(13).TextColor(textPrimary)
					ui.Text(c, bundleID).SingleLine().FontSize(11).TextColor(textMuted)
				})
				if ui.Button(c, "移除").Label("移除 " + bundleID).Clicked() {
					a.removeExcludedApp(bundleID)
				}
			})
		}
	})
}

// settingsStorageSection: 自动清理 periods, same setter as the tray's
// 自动清理 submenu.
func (a *shelf) settingsStorageSection(c *ui.Context) {
	settingsSection(c, "存放", a.settingsBroken, func() {
		days := max(0, a.settings.RetentionDays)
		labels, values, selected := choiceSegments(days, retentionChoices, func(n int) string {
			if n <= 0 {
				return "不清理"
			}
			return fmt.Sprintf("%d 天", n)
		})
		settingsRow(c, "retention", "自动清理", func() {
			if sg := ui.Segmented(c, &selected, labels...).Label("自动清理"); sg.Changed() {
				a.setRetention(values[selected])
			}
		})
		ui.Text(c, "超过期限的内容每小时检查一次；选择后立即执行一次。").FontSize(11).TextColor(textMuted)
	})
}

// settingsNowPlayingSection: 正在播放 — the peek player and panel card.
func (a *shelf) settingsNowPlayingSection(c *ui.Context) {
	settingsSection(c, "正在播放", a.settingsBroken, func() {
		settingsToggle(c, "np", "探头播放器与面板卡片", a.settings.IslandNowPlaying, func(on bool) {
			a.setNowPlaying(func(s *settings.Settings) { s.IslandNowPlaying = on })
		})
		ui.Text(c, "悬停刘海时在探头胶囊里显示封面与播放/暂停，展开面板顶部是同一张完整卡片。").
			FontSize(11).TextColor(textMuted)
	})
}

// settingsShortcutsSection lists the global shortcuts read-only; they are
// compiled in (config.go), not preferences.
func (a *shelf) settingsShortcutsSection(c *ui.Context) {
	settingsSection(c, "快捷键", false, func() {
		settingsRow(c, "sc-tray", "显示 / 收起", func() {
			ui.Text(c, shortcutHint).SingleLine().FontSize(13).TextColor(textMuted)
		})
		settingsRow(c, "sc-clip", "剪贴板搜索", func() {
			ui.Text(c, clipboardHint).SingleLine().FontSize(13).TextColor(textMuted)
		})
	})
}

// settingsFooter: the app version (from the bundle's Info.plist when
// packaged) and the button that opens the data folder in Finder.
func (a *shelf) settingsFooter(c *ui.Context) {
	ui.Row(c.Key("footer")).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
		ui.Text(c, appName+" "+appVersion()).SingleLine().FontSize(11).TextColor(textMuted).Grow(1)
		if ui.Button(c, "打开数据文件夹").Clicked() {
			if err := openPath(a.dataDir); err != nil {
				a.report(err)
			}
		}
	})
}

// appVersion is the bundle version (mygo.json's version → Info.plist) when
// packaged, else the module version of a source build, else "dev". Pure
// apart from the package-level probe; unit-tested.
func appVersion() string {
	if v := appVersionProbe(); v != "" {
		return v
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

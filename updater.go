//go:build darwin

package main

// Signed auto-updates through mygo.Updater. `mygo build` bakes the update
// feed (the latest release of updates.github in mygo.json) and the public
// key into the app; the release itself holds the signed archive and the
// update-darwin-arm64.json manifest. This file owns the scheduling and the
// non-intrusive notice: checks never prompt, a found version waits as a
// panel-footer line and a tray item until the user installs it.
//
// The check and the install run on their own goroutines (network and disk
// must not stall the UI); their results come back through dispatchMain onto
// the main thread, which owns every a.update* field.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"
)

const (
	// updateFirstDelay is when the first automatic check runs after launch.
	updateFirstDelay = 10 * time.Second
	// updateInterval is how often automatic checks run.
	updateInterval = 24 * time.Hour
	// updateRetry is the delay after a failed automatic check.
	updateRetry = time.Hour
)

// Seams around the app-level calls so the scheduling and state transitions
// run in headless tests without a packaged, updater-enabled build.
var (
	updateEnabled = func() bool { return mygo.Updater.Enabled() }
	updateCheck   = func(ctx context.Context) (*mygo.Update, error) { return mygo.Updater.Check(ctx) }
	runInstall    = func(ctx context.Context, up *mygo.Update, progress func(downloaded, total int64)) error {
		return up.Install(ctx, progress)
	}
	dispatchMain = mygo.RunOnMain
	// goWork runs the slow part of a check or install off the main thread.
	goWork      = func(fn func()) { go fn() }
	relaunchApp = func() { mygo.App.Relaunch() }
	// Timeouts bound the network work: go's HTTP client has none, so a
	// stalled GitHub request would otherwise leave updateChecking or
	// updateInstalling set forever and silence every later check. Variables
	// so tests can shrink them.
	updateCheckTimeout   = 30 * time.Second
	updateInstallTimeout = 10 * time.Minute
)

// nextAutoCheck returns when the next automatic check is due after one ran
// at now: a day after a successful check, sooner after a failure so a
// transient network problem does not mute updates for a day. Pure;
// unit-tested.
func nextAutoCheck(now time.Time, ok bool) time.Time {
	if ok {
		return now.Add(updateInterval)
	}
	return now.Add(updateRetry)
}

// tickUpdates drives the automatic checks from the monitoring loop (main
// thread): the first tick arms the launch check, later ticks fire when
// updateNext is due. The autoCheckUpdates setting is read on every tick, so
// turning it off stops future checks without a restart.
func (a *shelf) tickUpdates(now time.Time) {
	if !a.settings.AutoCheckUpdates || a.updateChecking || a.updateInstalling {
		return
	}
	if a.updateNext.IsZero() {
		a.updateNext = now.Add(updateFirstDelay)
		return
	}
	if now.Before(a.updateNext) {
		return
	}
	if !updateEnabled() {
		// Development and unsigned builds cannot update: check the setting
		// again tomorrow instead of every tick.
		a.updateNext = nextAutoCheck(now, true)
		return
	}
	a.checkUpdates(false)
}

// checkUpdates starts one update check on a background goroutine; user is
// true when the user asked from the tray menu and gets answers in the
// status line. A check or install already under way is never doubled.
func (a *shelf) checkUpdates(user bool) {
	if a.updateChecking || a.updateInstalling {
		return
	}
	a.updateChecking = true
	if user {
		// In-progress note: the check is fast, so the toast outlives it rarely.
		a.toastStatus("正在检查更新…", time.Now().Add(6*time.Second), false)
	}
	goWork(func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		up, err := updateCheck(ctx)
		dispatchMain(func() { a.checkedUpdate(user, up, err) })
	})
}

// checkedUpdate records the outcome of a check on the main thread. A found
// version is only announced (footer + tray item); nothing downloads until
// the user picks 安装更新.
func (a *shelf) checkedUpdate(user bool, up *mygo.Update, err error) {
	a.updateChecking = false
	// A check is a check: a manual one counts toward the daily schedule too.
	a.updateNext = nextAutoCheck(time.Now(), err == nil)
	if err != nil {
		log.Printf("Shelf: 检查更新失败：%v", err)
		if user {
			if errors.Is(err, mygo.ErrUpdatesDisabled) {
				a.toastStatus("此构建未启用自动更新（开发构建或未签名的安装）", time.Now().Add(8*time.Second), false)
			} else {
				a.toastStatus("检查更新失败："+err.Error(), time.Now().Add(8*time.Second), true)
			}
		}
		return
	}
	if up == nil {
		a.update = nil
		if user {
			a.toastStatus("已是最新版本", time.Now().Add(4*time.Second), false)
			if a.win != nil {
				a.win.Invalidate()
			}
		}
		return
	}
	// An install in flight or a version already swapped in keeps its state.
	if a.updateInstalling || (a.updateInstalled && a.update != nil && a.update.Version == up.Version) {
		return
	}
	a.update, a.updateInstalled, a.updateProgress = up, false, 0
	log.Printf("Shelf: 发现新版本 v%s", up.Version)
	if a.win != nil {
		a.win.Invalidate()
	}
	if os.Getenv("SHELF_UPDATE_SELFTEST") == "1" {
		// End-to-end test hook: the gated update smoke test uses it to take
		// the same install path the menu item takes; it never triggers a
		// relaunch, the test kills the process itself.
		a.installUpdate()
	}
}

// installUpdate runs when the user chooses 安装更新: the signed archive (or
// delta) downloads and replaces the bundle in the background while the
// footer shows progress. The new version takes over at the next launch or
// through 重新启动 Shelf 完成更新.
func (a *shelf) installUpdate() {
	up := a.update
	if up == nil || a.updateInstalling || a.updateInstalled {
		return
	}
	a.updateInstalling, a.updateProgress = true, 0
	a.toastStatus(fmt.Sprintf("正在下载更新 v%s…", up.Version), time.Now().Add(updateInstallTimeout), false)
	goWork(func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateInstallTimeout)
		defer cancel()
		lastPct := -1
		err := runInstall(ctx, up, func(n, total int64) {
			if total <= 0 {
				return
			}
			pct := int(n * 100 / total)
			if pct == lastPct {
				return
			}
			lastPct = pct
			dispatchMain(func() {
				if a.updateInstalling {
					a.updateProgress = float64(pct) / 100
					if a.win != nil {
						a.win.Invalidate()
					}
				}
			})
		})
		dispatchMain(func() { a.installedUpdate(up, err) })
	})
}

// installedUpdate records an install's outcome on the main thread.
func (a *shelf) installedUpdate(up *mygo.Update, err error) {
	a.updateInstalling, a.updateProgress = false, 0
	if err != nil {
		log.Printf("Shelf: 安装更新失败：%v", err)
		a.toastStatus("安装更新失败："+err.Error(), time.Now().Add(8*time.Second), true)
	} else {
		a.updateInstalled = true
		log.Printf("Shelf: 更新 v%s 已安装，重启后生效", up.Version)
		a.toastStatus(fmt.Sprintf("v%s 已安装 · 重新启动 Shelf 后生效", up.Version), time.Now().Add(8*time.Second), false)
	}
}

// updateMenu returns the update entry of the tray menu: 检查更新… normally,
// 安装更新（vX）… while a version waits, a progress line while it installs
// and 重新启动 once it is in place.
func (a *shelf) updateMenu() *mygo.MenuItem {
	switch {
	case a.updateInstalled:
		return &mygo.MenuItem{Label: "重新启动 Shelf 完成更新", Click: func(*mygo.MenuItem, *mygo.Window) {
			relaunchApp()
		}}
	case a.updateInstalling:
		label := fmt.Sprintf("正在安装更新（v%s）…", a.update.Version)
		if a.updateProgress > 0 {
			label = fmt.Sprintf("正在安装更新（v%s · %d%%）", a.update.Version, int(a.updateProgress*100))
		}
		return &mygo.MenuItem{Label: label, Disabled: true}
	case a.update != nil:
		return &mygo.MenuItem{Label: fmt.Sprintf("安装更新（v%s）…", a.update.Version), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.installUpdate()
		}}
	case a.updateChecking:
		return &mygo.MenuItem{Label: "正在检查更新…", Disabled: true}
	default:
		return &mygo.MenuItem{Label: "检查更新…", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.checkUpdates(true)
		}}
	}
}

// updateStatus is the persistent update notice of the panel footer, ""
// while there is nothing to say. It is a line of its own so it never
// overwrites status messages.
func (a *shelf) updateStatus() string {
	switch {
	case a.updateInstalling:
		if a.updateProgress > 0 {
			return fmt.Sprintf("正在下载更新 v%s · %d%%", a.update.Version, int(a.updateProgress*100))
		}
		return fmt.Sprintf("正在下载更新 v%s…", a.update.Version)
	case a.updateInstalled:
		return fmt.Sprintf("v%s 已安装 · 重新启动 Shelf 后生效", a.update.Version)
	case a.update != nil:
		return fmt.Sprintf("发现新版本 v%s · 从菜单栏图标的菜单安装", a.update.Version)
	}
	return ""
}

// setAutoCheckUpdates saves the autoCheckUpdates preference through the
// single applySettings path and, when it was just turned on, checks at the
// next monitor tick instead of waiting a day.
func (a *shelf) setAutoCheckUpdates(on bool) {
	if a.settings.AutoCheckUpdates == on {
		return
	}
	next := a.settings
	next.AutoCheckUpdates = on
	if !a.applySettings(next) {
		return
	}
	if on {
		a.updateNext = time.Now()
	}
}

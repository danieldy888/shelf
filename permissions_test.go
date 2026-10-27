//go:build darwin

package main

// Tests for the 系统权限 unified entry: the tray item's count, the Settings
// group's states, the 全部开启 walk (skipping granted items, moving from
// Accessibility to screen recording, the relaunch step, stopping), and the
// relaunch command's arguments. Every probe and request is a seam; no test
// reads or changes a real permission.

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/settings"
)

type permStubs struct {
	ax, screen bool
	titles     bool
	axRequests int
	scRequests int
}

type fakePermissionTimer struct{ stopped bool }

func (f *fakePermissionTimer) Stop() bool {
	was := !f.stopped
	f.stopped = true
	return was
}

type permissionClock struct {
	timers []*fakePermissionTimer
	fns    []func()
	delays []time.Duration
}

func (c *permissionClock) after(d time.Duration, fn func()) permissionTimer {
	t := &fakePermissionTimer{}
	c.timers = append(c.timers, t)
	c.fns = append(c.fns, fn)
	c.delays = append(c.delays, d)
	return t
}

func (c *permissionClock) fireLast(t *testing.T) {
	t.Helper()
	if len(c.fns) == 0 {
		t.Fatal("no permission timer was armed")
	}
	c.fns[len(c.fns)-1]()
}

func (c *permissionClock) last() *fakePermissionTimer {
	if len(c.timers) == 0 {
		return nil
	}
	return c.timers[len(c.timers)-1]
}

func stubPermissions(t *testing.T) (*permStubs, *permissionClock) {
	t.Helper()
	st := &permStubs{}
	clk := &permissionClock{}
	origAXProbe, origSCProbe := accessibilityProbe, screenCaptureProbe
	origAXReq, origSCReq := accessibilityRequest, screenCaptureRequest
	origTitles := screenCaptureWindowTitles
	origAfter, origDispatch := permissionAfterFunc, permissionDispatch
	accessibilityProbe = func() bool { return st.ax }
	screenCaptureProbe = func() bool { return st.screen }
	accessibilityRequest = func() bool { st.axRequests++; return st.ax }
	screenCaptureRequest = func() bool { st.scRequests++; return st.screen }
	screenCaptureWindowTitles = func() bool { return st.titles }
	permissionAfterFunc = clk.after
	permissionDispatch = func(fn func()) { fn() }
	t.Cleanup(func() {
		accessibilityProbe, screenCaptureProbe = origAXProbe, origSCProbe
		accessibilityRequest, screenCaptureRequest = origAXReq, origSCReq
		screenCaptureWindowTitles = origTitles
		permissionAfterFunc, permissionDispatch = origAfter, origDispatch
	})
	return st, clk
}

func permissionTrayLabels(t *testing.T) []string {
	t.Helper()
	labels := []string{}
	for _, it := range (&shelf{settings: settings.Default()}).trayMenu().Items() {
		labels = append(labels, it.Label)
	}
	return labels
}

func TestPermissionTrayItemCountsMissing(t *testing.T) {
	for _, tt := range []struct {
		name        string
		ax, screen  bool
		want        string
		wantDisable bool
	}{
		{"both missing", false, false, "开启系统权限（还差 2 项）…", false},
		{"screen missing", true, false, "开启系统权限（还差 1 项）…", false},
		{"ax missing", false, true, "开启系统权限（还差 1 项）…", false},
		{"all granted", true, true, "系统权限已全部开启", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st, _ := stubPermissions(t)
			st.ax, st.screen = tt.ax, tt.screen
			a := &shelf{settings: settings.Default()}
			var item = a.permissionMenuItem()
			if item.Label != tt.want {
				t.Fatalf("tray label = %q, want %q", item.Label, tt.want)
			}
			if item.Disabled != tt.wantDisable {
				t.Fatalf("tray disabled = %v, want %v", item.Disabled, tt.wantDisable)
			}
			// The item replaces the old guard entry in the same position.
			labels := permissionTrayLabels(t)
			if len(labels) < 2 || labels[1] != tt.want {
				t.Fatalf("permission item is not second in the tray: %v", labels)
			}
		})
	}
}

func TestSettingsPermissionsSectionIdle(t *testing.T) {
	st, _ := stubPermissions(t)
	_, view := newSettingsShelf(t)
	if !view.HasText("系统权限") || !view.HasText("ShelfU 需要 2 项系统权限") || !view.HasText("还差 2 项") {
		t.Fatalf("idle header missing; texts=%v", view.Texts())
	}
	for _, want := range []string{"刘海拖放保护 · 辅助功能", "框选文字 · 屏幕录制", "未开启", "全部开启…"} {
		if !view.HasText(want) {
			t.Fatalf("idle section missing %q; texts=%v", want, view.Texts())
		}
	}
	// The old 通用 row is gone, and the 系统权限 group is first.
	st.ax, st.screen = true, true
	view.Frame()
	texts := view.Texts()
	permAt, generalAt := slices.Index(texts, "系统权限"), slices.Index(texts, "通用")
	if permAt < 0 || generalAt < 0 || permAt > generalAt {
		t.Fatalf("系统权限 must precede 通用: perm=%d general=%d", permAt, generalAt)
	}
	if !view.HasText("已全部开启") || !view.HasText("✓ 已开启") {
		t.Fatalf("all-granted state missing; texts=%v", view.Texts())
	}
	if view.HasText("全部开启…") {
		t.Fatal("all-granted state still offers 全部开启…")
	}
}

func TestPermissionFlowSkipsGrantedAndStops(t *testing.T) {
	st, clk := stubPermissions(t)
	st.ax, st.screen = true, false
	a, view := newSettingsShelf(t)
	if err := view.Click("全部开启…"); err != nil {
		t.Fatal(err)
	}
	view.Frame()
	if st.axRequests != 0 {
		t.Fatalf("granted Accessibility was requested: %d", st.axRequests)
	}
	if st.scRequests != 1 {
		t.Fatalf("screen recording was not requested: %d", st.scRequests)
	}
	if !view.HasText("正在开启：第 1 项，共 1 项") {
		t.Fatalf("missing count must cover only the missing item; texts=%v", view.Texts())
	}
	if !view.HasText("停止") {
		t.Fatal("the walk must offer 停止")
	}
	if err := view.Click("停止"); err != nil {
		t.Fatal(err)
	}
	if clk.last() == nil || !clk.last().stopped {
		t.Fatal("停止 left its timer armed")
	}
	if a.perm.stage != permissionIdle {
		t.Fatalf("停止 left the flow at %v", a.perm.stage)
	}
	view.Frame()
	if !view.HasText("ShelfU 需要 2 项系统权限") || !view.HasText("全部开启…") {
		t.Fatalf("停止 did not return to the ordinary state; texts=%v", view.Texts())
	}
	// A callback that was already in flight when 停止 ran must do nothing.
	clk.fireLast(t)
	if a.perm.stage != permissionIdle || st.scRequests != 1 {
		t.Fatalf("a stale timer advanced a stopped flow: stage=%v requests=%d", a.perm.stage, st.scRequests)
	}
}

func TestPermissionFlowAdvancesFromAccessibilityToScreen(t *testing.T) {
	st, clk := stubPermissions(t)
	a, view := newSettingsShelf(t)
	if err := view.Click("全部开启…"); err != nil {
		t.Fatal(err)
	}
	view.Frame()
	if st.axRequests != 1 || st.scRequests != 0 {
		t.Fatalf("first step wrong: ax=%d screen=%d", st.axRequests, st.scRequests)
	}
	if !view.HasText("正在开启：第 1 项，共 2 项") {
		t.Fatalf("first heading missing; texts=%v", view.Texts())
	}
	if !view.HasText("等你在系统设置里打开") {
		t.Fatalf("the waiting row is missing; texts=%v", view.Texts())
	}
	// A poll while the switch is still off must stay on step 1.
	clk.fireLast(t)
	view.Frame()
	if st.scRequests != 0 || !view.HasText("正在开启：第 1 项，共 2 项") {
		t.Fatalf("an ungranted poll advanced the flow: screen=%d texts=%v", st.scRequests, view.Texts())
	}
	// The grant lands in System Settings; the poll sees it and moves on.
	st.ax = true
	clk.fireLast(t)
	view.Frame()
	if st.scRequests != 1 {
		t.Fatalf("screen recording not requested after Accessibility: %d", st.scRequests)
	}
	if !view.HasText("正在开启：第 2 项，共 2 项") {
		t.Fatalf("second heading missing; texts=%v", view.Texts())
	}
	if !view.HasText("✓ 已开启") {
		t.Fatalf("the granted Accessibility row is not green; texts=%v", view.Texts())
	}
	// The screen step hands over to the relaunch step.
	clk.fireLast(t)
	if a.perm.stage != permissionRelaunch {
		t.Fatalf("screen step did not reach the relaunch stage: %v", a.perm.stage)
	}
	view.Frame()
	if !view.HasText("还差最后一步") || !view.HasText("重新打开 ShelfU") {
		t.Fatalf("relaunch step missing; texts=%v", view.Texts())
	}
}

func TestPermissionFlowRelaunchRowUsesWindowTitles(t *testing.T) {
	st, _ := stubPermissions(t)
	a, view := newSettingsShelf(t)
	a.perm = permissionFlow{stage: permissionRelaunch, ids: []string{screenCapturePermissionID}}
	st.titles = false
	view.Frame()
	if !view.HasText("等你在系统设置里打开") {
		t.Fatalf("unproven screen grant must stay honest; texts=%v", view.Texts())
	}
	// The relaunch poll stores a positive WindowServer answer and repaints.
	st.titles = true
	a.permissionTimerFired()
	view.Frame()
	if !a.perm.screenGranted || !view.HasText("重新打开后生效") {
		t.Fatalf("a proven grant must show 重新打开后生效; texts=%v", view.Texts())
	}
}

func TestSettingsCloseStopsPermissionFlow(t *testing.T) {
	st, clk := stubPermissions(t)
	a, view := newSettingsShelf(t)
	if err := view.Click("全部开启…"); err != nil {
		t.Fatal(err)
	}
	if a.perm.stage != permissionWaiting {
		t.Fatal("flow did not start")
	}
	a.settingsClosed()
	if clk.last() == nil || !clk.last().stopped {
		t.Fatal("closing the window left the poll armed")
	}
	if a.perm.stage != permissionIdle {
		t.Fatalf("closing left the flow at %v", a.perm.stage)
	}
	before := st.axRequests
	clk.fireLast(t)
	if st.axRequests != before || a.perm.stage != permissionIdle {
		t.Fatal("a poll after close still ran")
	}
}

func TestPermissionFlowNeverRequestsGrantedItems(t *testing.T) {
	st, clk := stubPermissions(t)
	st.ax, st.screen = true, true
	a := &shelf{settings: settings.Default()}
	a.startPermissionFlow()
	if a.perm.stage != permissionIdle || st.axRequests != 0 || st.scRequests != 0 {
		t.Fatalf("all granted must not request anything: stage=%v ax=%d screen=%d",
			a.perm.stage, st.axRequests, st.scRequests)
	}
	if len(clk.timers) != 0 {
		t.Fatalf("all granted armed %d timers", len(clk.timers))
	}
}

// A broken settings.json locks the preference groups; the permission group
// is independent of that file and must keep working.
func TestPermissionsSectionWorksWhileSettingsBroken(t *testing.T) {
	st, _ := stubPermissions(t)
	dir := t.TempDir()
	a := &shelf{dataDir: dir, settings: settings.Default(), settingsBroken: true,
		thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}, appNames: map[string]string{}}
	view := ui.NewTester(a.settingsView, settingsWidth, settingsHeight)
	if !view.HasText("全部开启…") || !view.HasText("settings.json 无法读取") {
		t.Fatalf("permission group is not usable while settings are broken; texts=%v", view.Texts())
	}
	if err := view.Click("开启 刘海拖放保护 · 辅助功能"); err != nil {
		t.Fatal(err)
	}
	if st.axRequests != 1 {
		t.Fatalf("broken settings blocked the permission request: %d", st.axRequests)
	}
	if !view.HasText("第一次使用，或在系统设置里关掉过开关时，在这里重新开启。") {
		t.Fatal("permission footnote is missing")
	}
	// The old 通用 row (and its update wording) is gone.
	if view.HasText("每次更新后重新开启") {
		t.Fatal("the obsolete 每次更新 wording is still shown")
	}
}

func TestPermissionRowOpensOnePermission(t *testing.T) {
	st, _ := stubPermissions(t)
	_, view := newSettingsShelf(t)
	if err := view.Click("开启 框选文字 · 屏幕录制"); err != nil {
		t.Fatal(err)
	}
	if st.scRequests != 1 || st.axRequests != 0 {
		t.Fatalf("row button requested the wrong items: ax=%d screen=%d", st.axRequests, st.scRequests)
	}
	if st.screen {
		t.Fatal("stub granted a permission it never had")
	}
}

func TestRelaunchPlanUsesLaunchServicesInsideBundle(t *testing.T) {
	plan := relaunchPlanFor("/Applications/ShelfU App.app/Contents/MacOS/ShelfU",
		[]string{"--data-dir", "/tmp/a b", "--show"}, 4321)
	if plan.Exec != "/bin/sh" || len(plan.Args) != 2 || plan.Args[0] != "-c" {
		t.Fatalf("relaunch plan is not a shell waiter: %+v", plan)
	}
	script := plan.Args[1]
	for _, want := range []string{
		"kill -0 4321", "sleep 0.1", "sleep 0.3",
		"exec /usr/bin/open -n '/Applications/ShelfU App.app'",
		"--args '--data-dir' '/tmp/a b' '--show'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("relaunch script lacks %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "Contents/MacOS") {
		t.Fatalf("a bundled relaunch must not exec the binary directly:\n%s", script)
	}
	// No arguments: open is called without an empty --args.
	noArgs := relaunchPlanFor("/tmp/My.app/Contents/MacOS/My", nil, 9).Args[1]
	if !strings.Contains(noArgs, "exec /usr/bin/open -n '/tmp/My.app'") || strings.Contains(noArgs, "--args") {
		t.Fatalf("empty argument list produced --args:\n%s", noArgs)
	}
}

func TestRelaunchPlanFallsBackOutsideBundle(t *testing.T) {
	plan := relaunchPlanFor("/tmp/ShelfU", []string{"--data-dir", "/tmp/a b"}, 4321)
	script := plan.Args[1]
	for _, want := range []string{
		"kill -0 4321",
		"exec '/tmp/ShelfU' '--data-dir' '/tmp/a b'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("fallback script lacks %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "/usr/bin/open") {
		t.Fatalf("a bare executable must be started directly:\n%s", script)
	}
	// A path that merely looks app-ish but is not Contents/MacOS also falls
	// back rather than opening the wrong bundle.
	odd := relaunchPlanFor("/tmp/Not.app/tool", nil, 7).Args[1]
	if !strings.Contains(odd, "exec '/tmp/Not.app/tool'") {
		t.Fatalf("odd path was treated as a bundle:\n%s", odd)
	}
	quoted := relaunchPlanFor("/tmp/it's", nil, 7).Args[1]
	if !strings.Contains(quoted, `exec '/tmp/it'\''s'`) {
		t.Fatalf("single quote was not escaped: %s", quoted)
	}
}

func TestAppBundleForExecutable(t *testing.T) {
	for _, tt := range []struct {
		exe  string
		want string
		ok   bool
	}{
		{"/Applications/ShelfU.app/Contents/MacOS/ShelfU", "/Applications/ShelfU.app", true},
		{"/tmp/a b/My App.app/Contents/MacOS/My App", "/tmp/a b/My App.app", true},
		{"/tmp/ShelfU", "", false},
		{"/tmp/Not.app/tool", "", false},
		{"/Contents/MacOS/ShelfU", "", false},
		{"", "", false},
	} {
		got, ok := appBundleForExecutable(tt.exe)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("appBundleForExecutable(%q) = %q,%v; want %q,%v", tt.exe, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRelaunchShelfUsesSameArgsAndQuits(t *testing.T) {
	st, _ := stubPermissions(t)
	st.ax, st.screen = true, true
	oldArgs := os.Args
	os.Args = []string{"/tmp/ShelfU", "--data-dir", "/tmp/data dir"}
	defer func() { os.Args = oldArgs }()
	origExe, origStart, origQuit := relaunchExecutable, relaunchStart, relaunchQuit
	relaunchExecutable = func() (string, error) { return "/tmp/ShelfU", nil }
	var got relaunchPlan
	relaunchStart = func(p relaunchPlan) error { got = p; return nil }
	quit := false
	relaunchQuit = func() { quit = true }
	t.Cleanup(func() { relaunchExecutable, relaunchStart, relaunchQuit = origExe, origStart, origQuit })

	a, _ := newSettingsShelf(t)
	a.perm = permissionFlow{stage: permissionRelaunch, ids: []string{screenCapturePermissionID}}
	a.relaunchShelf()
	if !quit {
		t.Fatal("relaunch did not quit the running app")
	}
	script := got.Args[1]
	if !strings.Contains(script, "'--data-dir' '/tmp/data dir'") {
		t.Fatalf("--data-dir was not preserved: %s", script)
	}
	if a.perm.stage != permissionIdle {
		t.Fatalf("relaunch left the flow at %v", a.perm.stage)
	}
}

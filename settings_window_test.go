//go:build darwin

package main

// Tests for the Settings window: headless ui.Tester coverage of the view
// (toggles apply through the same save path as the tray menu, a broken
// settings.json locks editing, excluded apps can be removed) plus pure
// tests for the geometry/segment helpers, plus a gated native-window smoke
// test (SHELF_BASKET_SMOKE=1) that screenshots the real window.

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/settings"
)

// stubSettingsSeams replaces the app-level seams and returns a restore func.
func stubSettingsSeams(t *testing.T, loginOn bool, setErr error, opened *[]string) {
	t.Helper()
	origOpen, origSet, origPath, origAx := openAtLogin, setOpenAtLogin, openPath, accessibilityRequest
	openAtLogin = func() bool { return loginOn }
	setOpenAtLogin = func(on bool) error { loginOnHolder := on; _ = loginOnHolder; return setErr }
	openPath = func(p string) error {
		if opened != nil {
			*opened = append(*opened, p)
		}
		return nil
	}
	accessibilityRequest = func() bool { return true }
	t.Cleanup(func() {
		openAtLogin, setOpenAtLogin, openPath, accessibilityRequest = origOpen, origSet, origPath, origAx
	})
}

func newSettingsShelf(t *testing.T) (*shelf, *ui.Tester) {
	t.Helper()
	dir := t.TempDir()
	a := &shelf{
		dataDir: dir, settings: settings.Default(),
		thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{},
		appNames: map[string]string{},
	}
	return a, ui.NewTester(a.settingsView, settingsWidth, settingsHeight)
}

func TestSettingsFrameCentersAndClamps(t *testing.T) {
	work := rectHelper(0, 25, 1512, 955)
	f := settingsFrame(work)
	if f.Width != settingsWidth || f.Height != settingsHeight {
		t.Fatalf("frame size changed: %+v", f)
	}
	cx, cy := f.X+f.Width/2, f.Y+f.Height/2
	wx, wy := work.X+work.Width/2, work.Y+work.Height/2
	if cx != wx || cy != wy {
		t.Fatalf("window not centered: center=(%d,%d) want (%d,%d)", cx, cy, wx, wy)
	}
	// A work area smaller than the window must clamp, not go negative.
	tiny := rectHelper(100, 50, 200, 150)
	f2 := settingsFrame(tiny)
	if f2.X < tiny.X || f2.Y < tiny.Y {
		t.Fatalf("frame left the work area: %+v vs %+v", f2, tiny)
	}
}

func rectHelper(x, y, w, h int) mygo.Rectangle {
	return mygo.Rectangle{X: x, Y: y, Width: w, Height: h}
}

// clickVisible scrolls the settings view until the element showing label is
// inside the viewport, then clicks it: the window is 520pt tall and the
// content scrolls, so a target may start below the fold.
func clickVisible(t *testing.T, view *ui.Tester, label string) {
	t.Helper()
	for range 30 {
		if r, ok := view.Find(label); ok && r.W > 0 && r.H > 0 &&
			r.Y >= 0 && r.Y+r.H <= settingsHeight {
			if err := view.Click(label); err != nil {
				t.Fatal(err)
			}
			return
		}
		view.Scroll(settingsWidth/2, settingsHeight/2, 0, 120)
	}
	t.Fatalf("%q never scrolled into view", label)
}

func TestTrayMenuOffersSettings(t *testing.T) {
	stubSettingsSeams(t, false, nil, nil)
	a, _ := newSettingsShelf(t)
	labels := []string{}
	for _, it := range a.trayMenu().Items() {
		labels = append(labels, it.Label)
	}
	for _, want := range []string{"显示 / 收起 Shelf", "自动清理", "剪贴板记录", "刘海岛", "设置…", "退出 Shelf"} {
		if !slices.Contains(labels, want) {
			t.Fatalf("tray menu lacks %q; labels=%v", want, labels)
		}
	}
}

func TestChoiceSegments(t *testing.T) {
	labels, values, sel := choiceSegments(7, []int{1, 7, 30}, func(n int) string { return fmt.Sprintf("%dd", n) })
	if sel != 1 || len(labels) != 3 || values[0] != 1 {
		t.Fatalf("bad segments: %v %v sel=%d", labels, values, sel)
	}
	// A hand-edited value not among the presets must appear, sorted in.
	labels, values, sel = choiceSegments(14, []int{1, 7, 30}, func(n int) string { return fmt.Sprintf("%dd", n) })
	if sel != 2 || values[2] != 14 || labels[2] != "14d" {
		t.Fatalf("foreign value not inserted in order: %v %v sel=%d", labels, values, sel)
	}
}

func TestSettingsViewShowsSections(t *testing.T) {
	stubSettingsSeams(t, false, nil, nil)
	_, view := newSettingsShelf(t)
	for _, s := range []string{"通用", "剪贴板", "存放", "刘海岛", "快捷键", "开机时自动启动", "记录剪贴板", "保留条数", "自动清理", "正在播放", "电量与充电", "音量", "显示 / 收起", "剪贴板搜索", "打开数据文件夹"} {
		if !view.HasText(s) {
			t.Fatalf("settings view missing %q; texts=%v", s, view.Texts())
		}
	}
}

func TestSettingsViewToggleAppliesAndSaves(t *testing.T) {
	stubSettingsSeams(t, false, nil, nil)
	a, view := newSettingsShelf(t)
	clickVisible(t, view, "记录剪贴板")
	if a.settings.ClipboardHistory {
		t.Fatal("toggle did not flip the running setting")
	}
	saved, err := settings.Load(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ClipboardHistory {
		t.Fatal("settings.json was not updated by the window toggle")
	}
	if a.clip != nil {
		t.Fatal("turning recording off must stop the clipboard module")
	}
}

func TestSettingsViewIslandToggleSaves(t *testing.T) {
	stubSettingsSeams(t, false, nil, nil)
	a, view := newSettingsShelf(t)
	clickVisible(t, view, "音量")
	if a.settings.IslandVolume {
		t.Fatal("island volume toggle did not apply")
	}
	saved, _ := settings.Load(a.dataDir)
	if saved.IslandVolume || !saved.IslandNowPlaying || !saved.IslandBattery {
		t.Fatalf("saved flags wrong: %+v", saved)
	}
}

func TestSettingsViewSegmentedApplies(t *testing.T) {
	stubSettingsSeams(t, false, nil, nil)
	a, view := newSettingsShelf(t)
	clickVisible(t, view, "100 条")
	if a.settings.ClipboardLimit != 100 {
		t.Fatalf("limit not applied: %d", a.settings.ClipboardLimit)
	}
	saved, _ := settings.Load(a.dataDir)
	if saved.ClipboardLimit != 100 {
		t.Fatalf("limit not saved: %+v", saved)
	}
	clickVisible(t, view, "7 天")
	saved, _ = settings.Load(a.dataDir)
	if saved.RetentionDays != 7 {
		t.Fatalf("retention not saved: %+v", saved)
	}
}

func TestSettingsViewBrokenDisablesEditing(t *testing.T) {
	stubSettingsSeams(t, false, nil, nil)
	dir := t.TempDir()
	a := &shelf{
		dataDir: dir, settings: settings.Default(), settingsBroken: true,
		thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{},
		appNames: map[string]string{},
	}
	a.settings.ExcludedApps = []string{"com.example.One"}
	view := ui.NewTester(a.settingsView, settingsWidth, settingsHeight)
	if !view.HasText("settings.json 无法读取") {
		t.Fatalf("broken banner missing; texts=%v", view.Texts())
	}
	// Clicks on disabled controls must change nothing and write nothing.
	clickVisible(t, view, "记录剪贴板")
	clickVisible(t, view, "100 条")
	clickVisible(t, view, "移除 com.example.One")
	if !a.settings.ClipboardHistory || a.settings.ClipboardLimit != 30 || len(a.settings.ExcludedApps) != 1 {
		t.Fatalf("broken settings were mutated: %+v", a.settings)
	}
	if _, err := os.Stat(filepath.Join(dir, settings.FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("broken settings.json was written: %v", err)
	}
}

func TestSettingsViewExcludedAppRemoval(t *testing.T) {
	stubSettingsSeams(t, false, nil, nil)
	a, view := newSettingsShelf(t)
	a.settings.ExcludedApps = []string{"com.example.One", "com.example.Two"}
	view = ui.NewTester(a.settingsView, settingsWidth, settingsHeight)
	if !view.HasText("com.example.Two") {
		t.Fatal("excluded app not listed")
	}
	clickVisible(t, view, "移除 com.example.One")
	if len(a.settings.ExcludedApps) != 1 || a.settings.ExcludedApps[0] != "com.example.Two" {
		t.Fatalf("wrong remaining exclusions: %v", a.settings.ExcludedApps)
	}
	saved, _ := settings.Load(a.dataDir)
	if len(saved.ExcludedApps) != 1 || saved.ExcludedApps[0] != "com.example.Two" {
		t.Fatalf("exclusion removal not saved: %+v", saved.ExcludedApps)
	}
	if view.HasText("com.example.One") {
		t.Fatal("removed app still rendered")
	}
}

func TestSettingsViewLoginSwitch(t *testing.T) {
	// Failing backend shows the error inline and does not flip the state.
	stubSettingsSeams(t, false, errors.New("app must be installed in /Applications"), nil)
	a, view := newSettingsShelf(t)
	clickVisible(t, view, "开机时自动启动")
	if a.loginOn {
		t.Fatal("login flag flipped despite backend failure")
	}
	if a.loginErr == "" || !view.HasText("/Applications") {
		t.Fatalf("inline login error missing: %q", a.loginErr)
	}
	// A working backend flips and clears the error.
	stubSettingsSeams(t, false, nil, nil)
	setOpenAtLogin = func(on bool) error { a.loginOn = on; return nil }
	clickVisible(t, view, "开机时自动启动")
	if a.loginErr != "" {
		t.Fatalf("error not cleared after success: %q", a.loginErr)
	}
}

func TestSettingsViewOpenDataFolder(t *testing.T) {
	var opened []string
	stubSettingsSeams(t, false, nil, &opened)
	a, view := newSettingsShelf(t)
	clickVisible(t, view, "打开数据文件夹")
	if len(opened) != 1 || opened[0] != a.dataDir {
		t.Fatalf("data dir not opened: %v", opened)
	}
}

func TestAppVersionFallback(t *testing.T) {
	orig := appVersionProbe
	defer func() { appVersionProbe = orig }()
	appVersionProbe = func() string { return "1.2.3" }
	if v := appVersion(); v != "1.2.3" {
		t.Fatalf("packaged version not used: %q", v)
	}
	appVersionProbe = func() string { return "" }
	if v := appVersion(); v == "" {
		t.Fatal("empty version")
	}
}

// --- Gated native-window smoke test -------------------------------------

// TestSettingsWindowSmoke opens the real Settings window in the live app
// loop, verifies it is a real WindowServer window centered on the cursor's
// display, screenshots it with `screencapture -l <id>` and checks the shot
// is a dark window with bright text. SHELF_SETTINGS_HOLD=1 keeps the window
// up for an external screenshot. Run with SHELF_BASKET_SMOKE=1.
func TestSettingsWindowSmoke(t *testing.T) {
	if os.Getenv("SHELF_BASKET_SMOKE") != "1" {
		t.Skip("set SHELF_BASKET_SMOKE=1 to run the native settings window smoke test")
	}
	a := smokeShelf
	// Give the smoke instance a writable data dir and real settings.
	dir := t.TempDir()
	a.dataDir = dir
	prefs, _ := settings.Load(dir)
	a.settings = prefs
	var first *mygo.Window
	onMain(func() {
		a.openSettings()
		first = a.settingsWin
	})
	if first == nil {
		t.Fatal("settings window was not created")
	}
	defer onMain(func() { first.Destroy() })

	// Single instance: a second open must not create another window.
	var second *mygo.Window
	onMain(func() {
		a.openSettings()
		second = a.settingsWin
	})
	if second != first {
		t.Fatal("openSettings created a second window — not single instance")
	}

	num := mac.WindowNumber(first.NativeHandle())
	if num == 0 {
		t.Fatal("settings window has no CGWindowID")
	}
	// WindowServer must show it at the centered frame computed from the
	// cursor display's work area.
	want := settingsFrame(mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea)
	var got mac.NSRect
	settled := false
	for range 60 {
		onMain(func() {
			var ok bool
			got, ok = mac.WindowScreenRect(num)
			settled = ok && int(got.Size.Width) == want.Width && int(got.Size.Height) == want.Height &&
				int(got.Origin.X) == want.X && int(got.Origin.Y) == want.Y
		})
		if settled {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !settled {
		t.Fatalf("settings window %d never settled to %+v; last seen %+v", num, want, got)
	}
	t.Logf("settings CGWindowID=%d frame=%+v — screencapture -l %d shots it", num, want, num)
	if os.Getenv("SHELF_SETTINGS_HOLD") != "" {
		time.Sleep(8 * time.Second)
	}
	time.Sleep(400 * time.Millisecond) // let the frame settle

	shot := filepath.Join(os.TempDir(), "shelf-settings-smoke.png")
	out, err := exec.Command("screencapture", "-x", "-o", fmt.Sprintf("-l%d", num), shot).CombinedOutput()
	if err != nil {
		t.Fatalf("screencapture failed (%v): %s", err, out)
	}
	f, err := os.Open(shot)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatalf("decode %s: %v", shot, err)
	}
	box := opaqueBounds(img)
	if box.Dx() < settingsWidth || box.Dy() < settingsHeight {
		t.Fatalf("screenshot %s shows only %v of paint; want ~%dx%d", shot, box, settingsWidth, settingsHeight)
	}
	// The shot must be a dark window with light text on top.
	dark, light := 0, 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bl, _ := img.At(x, y).RGBA()
			brightness := int(r>>8) + int(g>>8) + int(bl>>8)
			if brightness < 90 {
				dark++
			} else if brightness > 450 {
				light++
			}
		}
	}
	if dark == 0 || light == 0 {
		t.Fatalf("shot has no dark surface or no light text: dark=%d light=%d", dark, light)
	}
	t.Logf("settings shot=%s box=%v dark=%d light=%d", shot, box, dark, light)
}

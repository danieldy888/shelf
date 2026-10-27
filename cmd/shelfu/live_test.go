//go:build darwin

package main

// The realtime activities' rules: one notice at a time in arrival order,
// the panel cancels what is queued, charging posts each threshold once per
// cycle, headphone and download text, and the switches in settings.json
// silence their activity.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/settings"
	"shelf/internal/store"
)

func liveShelf() *shelf {
	a := motionShelf()
	a.settings = settings.Default()
	return a
}

func TestLiveQueueIsOneAtATime(t *testing.T) {
	a := liveShelf()
	now := time.Now()
	a.pushLive(liveEvent{kind: liveCharging, label: "正在充电", pct: 80}, time.Second, now)
	a.pushLive(liveEvent{kind: liveHeadset, label: "AirPods Pro"}, 2*time.Second, now.Add(500*time.Millisecond))

	ev, ok := a.liveNow(now, true)
	if !ok || ev.label != "正在充电" {
		t.Fatalf("first event = %+v ok=%v, want 正在充电", ev, ok)
	}
	if ev, ok = a.liveNow(now.Add(900*time.Millisecond), true); !ok || ev.label != "正在充电" {
		t.Fatalf("the first hold ended early: %+v ok=%v", ev, ok)
	}
	if ev, ok = a.liveNow(now.Add(time.Second), true); !ok || ev.label != "AirPods Pro" {
		t.Fatalf("second event = %+v ok=%v, want AirPods Pro", ev, ok)
	}
	if ev, ok = a.liveNow(now.Add(2500*time.Millisecond), true); ok {
		t.Fatalf("second event outlived its deadline: %+v", ev)
	}
}

func TestLiveWaitingDoesNotExtendHold(t *testing.T) {
	a := liveShelf()
	now := time.Now()
	a.pushLive(liveEvent{kind: liveCharging, label: "正在充电", pct: 30}, time.Second, now)
	a.pushLive(liveEvent{kind: liveLow, label: "电量低", pct: 10}, time.Second, now.Add(time.Second))
	if ev, ok := a.liveNow(now.Add(time.Second), true); !ok || ev.kind != liveLow {
		t.Fatalf("second event = %+v ok=%v", ev, ok)
	}
	// Its hold is measured from arrival: it is gone at arrival + 1s.
	if ev, ok := a.liveNow(now.Add(2*time.Second), true); ok {
		t.Fatalf("the waited event stretched its hold: %+v", ev)
	}
}

func TestLivePanelCancelsQueue(t *testing.T) {
	a := liveShelf()
	a.pushLive(liveEvent{kind: liveCharging, label: "正在充电", pct: 50}, 3*time.Second, time.Now())
	if _, ok := a.liveNow(time.Now(), true); !ok {
		t.Fatal("the event was not shown")
	}
	a.show(false) // opening clears the queue
	if len(a.live.queued) != 0 || a.live.shown {
		t.Fatalf("the queue survived the panel opening: %+v", a.live)
	}
	// Nothing posted while the panel is open pops out after closing.
	a.pushLive(liveEvent{kind: liveHeadset, label: "AirPods"}, liveHoldHeadset, time.Now())
	if len(a.live.queued) != 0 {
		t.Fatalf("an event was queued behind the open panel")
	}
}

func TestLiveCoversNowPlayingAndReturns(t *testing.T) {
	a := liveShelf()
	fakeTrack(a, true)
	a.settings.IslandNowPlaying = true
	now := time.Now()
	a.pushLive(liveEvent{kind: liveCharging, label: "正在充电", pct: 80}, liveHoldCharge, now)

	w, h := a.peekSize()
	if w <= a.wingsSizeW() || int(h) != max(32, a.notchH)+npWingsBody {
		t.Fatalf("charging shape = %gx%g, want wider than the wings", w, h)
	}
	// End the notice: the track's own wings come back.
	a.live.until = now.Add(-time.Second)
	w, h = a.peekSize()
	if w != a.wingsSizeW() {
		t.Fatalf("wings came back as %gx%g, want width %g", w, h, a.wingsSizeW())
	}
}

func (a *shelf) wingsSizeW() float32 {
	w, _ := a.wingsSize()
	return w
}

func TestLiveSwitchSilencesActivity(t *testing.T) {
	a := liveShelf()
	a.settings.LiveActivities.Charging = false
	a.pushLive(liveEvent{kind: liveCharging, label: "正在充电", pct: 20}, liveHoldCharge, time.Now())
	if len(a.live.queued) != 0 {
		t.Fatal("a switched-off charging notice was queued")
	}
	// A notice already up disappears when its switch turns off.
	a.settings.LiveActivities.Charging = true
	a.pushLive(liveEvent{kind: liveCharging, label: "正在充电", pct: 20}, liveHoldCharge, time.Now())
	a.settings.LiveActivities.Charging = false
	if _, ok := a.liveNow(time.Now(), true); ok {
		t.Fatal("the switch did not hide the notice")
	}
}

func TestPowerEventsOncePerThreshold(t *testing.T) {
	a := liveShelf()
	m := &powerModule{host: a, now: time.Now}
	var st mac.BatteryStatus
	ok := true
	m.probe = func() (mac.BatteryStatus, bool) { return st, ok }
	step := func(s mac.BatteryStatus, at time.Time) {
		st = s
		m.next = time.Time{}
		m.tick(at)
	}
	now := time.Now()
	step(mac.BatteryStatus{Percent: 50}, now) // seed on battery
	if len(a.live.queued) != 0 {
		t.Fatalf("the launch state posted %d notices", len(a.live.queued))
	}

	step(mac.BatteryStatus{Percent: 80, External: true}, now.Add(2*time.Second))
	if len(a.live.queued) != 1 || a.live.queued[0].kind != liveCharging {
		t.Fatalf("plugging in queued %+v", a.live.queued)
	}
	step(mac.BatteryStatus{Percent: 80, External: true, Charging: true}, now.Add(4*time.Second))
	if len(a.live.queued) != 1 {
		t.Fatalf("a steady charge posted again: %+v", a.live.queued)
	}
	step(mac.BatteryStatus{Percent: 100, External: true}, now.Add(6*time.Second))
	if len(a.live.queued) != 2 || a.live.queued[1].kind != liveCharged {
		t.Fatalf("full battery queued %+v", a.live.queued)
	}
	step(mac.BatteryStatus{Percent: 100, External: true}, now.Add(8*time.Second))
	if len(a.live.queued) != 2 {
		t.Fatalf("full battery repeated: %+v", a.live.queued)
	}

	// Discharge: each threshold once, then a new cycle brings them back.
	a.clearLive()
	step(mac.BatteryStatus{Percent: 30}, now.Add(10*time.Second))
	if len(a.live.queued) != 0 {
		t.Fatalf("unplugging above the threshold posted %+v", a.live.queued)
	}
	step(mac.BatteryStatus{Percent: 20}, now.Add(12*time.Second))
	step(mac.BatteryStatus{Percent: 19}, now.Add(14*time.Second))
	step(mac.BatteryStatus{Percent: 10}, now.Add(16*time.Second))
	step(mac.BatteryStatus{Percent: 9}, now.Add(18*time.Second))
	if len(a.live.queued) != 2 {
		t.Fatalf("low battery queued %d notices, want the 20%% and 10%% thresholds", len(a.live.queued))
	}
	if a.live.queued[0].pct != 20 || a.live.queued[1].pct != 10 {
		t.Fatalf("low notices %+v, want 20 then 10", a.live.queued)
	}
	a.clearLive()
	step(mac.BatteryStatus{Percent: 40, External: true}, now.Add(20*time.Second))
	a.clearLive() // drop the charging notice
	step(mac.BatteryStatus{Percent: 40}, now.Add(22*time.Second))
	step(mac.BatteryStatus{Percent: 20}, now.Add(24*time.Second))
	if len(a.live.queued) != 1 {
		t.Fatalf("the new discharge cycle did not repeat 20%%: %+v", a.live.queued)
	}

	// A Mac without a battery never speaks.
	a.clearLive()
	ok = false
	step(mac.BatteryStatus{Percent: 5}, now.Add(26*time.Second))
	if len(a.live.queued) != 0 {
		t.Fatalf("a battery-less Mac posted %+v", a.live.queued)
	}
}

func TestAudioConnectAndDisconnect(t *testing.T) {
	a := liveShelf()
	devs := []mac.AudioOutput{
		{Name: "扬声器", UID: "built-in"},
		{Name: "AirPods Pro", UID: "airpods", Bluetooth: true},
	}
	m := &audioModule{host: a, list: func() []mac.AudioOutput { return devs }, now: time.Now, known: map[string]string{}}
	now := time.Now()
	m.tick(now) // seed with the AirPods already connected
	if len(a.live.queued) != 0 {
		t.Fatalf("the launch state announced %+v", a.live.queued)
	}
	devs = []mac.AudioOutput{{Name: "扬声器", UID: "built-in"}}
	m.next = time.Time{}
	m.tick(now.Add(time.Second))
	if len(a.live.queued) != 1 || a.live.queued[0].kind != liveHeadsetOff || a.live.queued[0].label != "AirPods Pro" {
		t.Fatalf("disconnect queued %+v", a.live.queued)
	}
	devs = append(devs, mac.AudioOutput{Name: "AirPods Pro", UID: "airpods", Bluetooth: true})
	m.next = time.Time{}
	m.tick(now.Add(2 * time.Second))
	if len(a.live.queued) != 2 || a.live.queued[1].kind != liveHeadset {
		t.Fatalf("reconnect queued %+v", a.live.queued)
	}
	// A keyboard is not an output and must never be announced.
	devs = append(devs, mac.AudioOutput{Name: "键盘", UID: "keys"})
	m.next = time.Time{}
	m.tick(now.Add(3 * time.Second))
	if len(a.live.queued) != 2 {
		t.Fatalf("a non-audio device was announced: %+v", a.live.queued)
	}
}

func TestBluetoothLevelParsing(t *testing.T) {
	report := []byte(`{"SPBluetoothDataType":[{"device_connected":[{"AirPods Pro":{"device_batteryLevelLeft":"80%","device_batteryLevelRight":"78 %","device_batteryLevelCase":"50%"}},{"Sony WH-1000XM5":{"device_batteryLevelMain":"65%"}}]}]}`)
	levels := parseBluetoothLevels(report, "AirPods Pro")
	want := []string{"左 80%", "右 78%", "盒 50%"}
	if fmt.Sprint(levels) != fmt.Sprint(want) {
		t.Fatalf("levels = %v, want %v", levels, want)
	}
	if got := parseBluetoothLevels(report, "Sony WH-1000XM5"); fmt.Sprint(got) != "[电量 65%]" {
		t.Fatalf("single level = %v", got)
	}
	if got := parseBluetoothLevels(report, "Unknown"); got != nil {
		t.Fatalf("unknown device = %v", got)
	}
}

func TestDownloadScanIdentifiesBrowsers(t *testing.T) {
	dir := t.TempDir()
	writeLiveFile(t, filepath.Join(dir, "movie.mp4.crdownload"), make([]byte, 700))
	writeLiveFile(t, filepath.Join(dir, "linux.iso.part"), make([]byte, 1300))
	bundle := filepath.Join(dir, "design.zip.download")
	if err := os.Mkdir(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "design.zip")
	writeLiveFile(t, filepath.Join(bundle, "Info.plist"), []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>DownloadEntryProgressBytesSoFar</key><integer>250</integer>
<key>DownloadEntryProgressTotalToLoad</key><integer>1000</integer>
<key>DownloadEntryPath</key><string>%s</string>
</dict></plist>`, target)))

	files, err := scanDownloadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("scan found %d downloads: %+v", len(files), files)
	}
	byName := map[string]downloadFile{}
	for _, f := range files {
		byName[f.name] = f
	}
	if f := byName["movie.mp4"]; f.done != 700 || f.total != 0 {
		t.Fatalf("Chrome download = %+v", f)
	}
	if f := byName["linux.iso"]; f.done != 1300 || f.total != 0 {
		t.Fatalf("Firefox download = %+v", f)
	}
	f := byName["design.zip"]
	if f.done != 250 || f.total != 1000 || f.target != target {
		t.Fatalf("Safari download = %+v", f)
	}
	if got := downloadPercent(f); got != "25%" {
		t.Fatalf("Safari percent = %q", got)
	}
	if got := downloadPercent(byName["movie.mp4"]); got != "" {
		t.Fatalf("unknown total reported %q", got)
	}
	if got := downloadMegabytes(700); got != "700 B" {
		t.Fatalf("bytes = %q", got)
	}
}

func TestDownloadCompletionPostsCapsule(t *testing.T) {
	dir := t.TempDir()
	temp := filepath.Join(dir, "notes.txt.crdownload")
	target := filepath.Join(dir, "notes.txt")
	writeLiveFile(t, temp, make([]byte, 64))
	a := liveShelf()
	m := &downloadsModule{
		host: a, dir: func() (string, error) { return dir, nil }, scan: scanDownloadDir,
		allowed: func() bool { return true }, plist: readXMLPlist,
		now: time.Now, prev: map[string]downloadFile{},
	}
	now := time.Now()
	m.tick(now)
	if !m.active() {
		t.Fatal("the running download is not active")
	}
	// Chrome removes its temp file and renames it into place.
	if err := os.Remove(temp); err != nil {
		t.Fatal(err)
	}
	writeLiveFile(t, target, make([]byte, 64))
	m.next = time.Time{}
	m.tick(now.Add(time.Second))
	if m.active() {
		t.Fatal("the finished download still owns the wings")
	}
	ev, ok := a.liveNow(now.Add(time.Second), true)
	if !ok || ev.kind != liveDownloadDone || ev.path != target || ev.label != "notes.txt" {
		t.Fatalf("completion = %+v ok=%v", ev, ok)
	}
}

func TestDownloadSwitchAndUnreadableFolder(t *testing.T) {
	dir := t.TempDir()
	writeLiveFile(t, filepath.Join(dir, "a.bin.part"), make([]byte, 10))
	a := liveShelf()
	a.settings.LiveActivities.Downloads = false
	m := &downloadsModule{
		host: a, dir: func() (string, error) { return dir, nil }, scan: scanDownloadDir,
		allowed: func() bool { return true }, plist: readXMLPlist,
		now: time.Now, prev: map[string]downloadFile{},
	}
	m.tick(time.Now())
	if m.active() || len(a.live.queued) != 0 {
		t.Fatal("downloads ran with their switch off")
	}
	a.settings.LiveActivities.Downloads = true
	m.next = time.Time{}
	m.tick(time.Now().Add(time.Second))
	if !m.active() {
		t.Fatal("re-enabling did not start the watcher")
	}
	// Full Disk Access denied: nothing may be read.
	m = &downloadsModule{
		host: a, dir: func() (string, error) { return dir, nil }, scan: scanDownloadDir,
		allowed: func() bool { return false }, plist: readXMLPlist,
		now: time.Now, prev: map[string]downloadFile{},
	}
	m.tick(time.Now())
	if m.active() {
		t.Fatal("downloads were read without Full Disk Access")
	}
}

func TestLiveViewsDrawTheirWords(t *testing.T) {
	a := liveShelf()
	a.peeking = true
	a.peekProgress, a.peekFrom = 1, 1
	view := ui.NewTester(a.view, sheetW, sheetH)
	now := time.Now()
	a.pushLive(liveEvent{kind: liveCharging, label: "正在充电", pct: 80}, liveHoldCharge, now)
	view.Frame()
	if !view.HasText("正在充电") || !view.HasText("80") {
		t.Fatalf("charging wings lost their words: %v", view.Texts())
	}

	a.clearLive()
	a.fx.peekKind = peekChevron
	a.pushLive(liveEvent{kind: liveHeadset, label: "AirPods Pro", levels: []string{"左 80%", "右 78%", "盒 50%"}}, liveHoldHeadset, now.Add(time.Second))
	view2 := ui.NewTester(a.view, sheetW, sheetH)
	view2.Frame()
	for _, want := range []string{"AirPods Pro", "左 80%", "右 78%", "盒 50%"} {
		if !view2.HasText(want) {
			t.Fatalf("headphone capsule lost %q: %v", want, view2.Texts())
		}
	}

	a.clearLive()
	a.fx.peekKind = peekChevron
	a.dl = &downloadsModule{host: a, items: []downloadFile{{name: "movie.mp4", done: 420, total: 1000}}}
	a.settings.LiveActivities.Downloads = true
	view3 := ui.NewTester(a.view, sheetW, sheetH)
	view3.Frame()
	if !view3.HasText("42%") {
		t.Fatalf("download wings lost the percentage: %v", view3.Texts())
	}
}

func TestDownloadCapsuleClickAddsToTray(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "report.pdf")
	writeLiveFile(t, target, []byte("%PDF"))
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := liveShelf()
	a.store = st
	a.dataDir = t.TempDir()
	a.pushLive(liveEvent{kind: liveDownloadDone, label: "report.pdf", path: target}, liveHoldDownload, time.Now())
	ev, ok := a.liveNow(time.Now(), true)
	if !ok {
		t.Fatal("no completion event")
	}
	a.openDownloaded(ev)
	if !a.visible || a.currentLevel() != 2 || a.tab != pageTray {
		t.Fatalf("the tray page did not open: visible=%v level=%d tab=%v", a.visible, a.currentLevel(), a.tab)
	}
	found := false
	for _, it := range st.Items {
		if it.Path == target {
			found = true
		}
	}
	if !found {
		t.Fatalf("the file is not in the tray: %+v", st.Items)
	}
}

func writeLiveFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The signal geometry: both wings keep the bar's whole group inside the
// visible band (the middle of the shell sits behind the camera and has no
// pixels), and the capsules use the completion capsule's 22pt body.
func TestLiveShapesFitTheNotchBand(t *testing.T) {
	a := liveShelf()
	if liveWingPad+liveBarW+10+22 > liveWingW {
		t.Fatalf("the 66pt bar and a three-digit number need %gpt, wing is %d",
			float32(liveWingPad+liveBarW+10+22), liveWingW)
	}
	if liveWingPad+11+9+50 > liveWingW {
		t.Fatalf("the charging words need %gpt, wing is %d",
			float32(liveWingPad+11+9+50), liveWingW)
	}
	w, h := a.liveShape(liveEvent{kind: liveCharging, pct: 80})
	if w != float32(max(180, a.notchW))+2*liveWingW || h != float32(a.notchH)+npWingsBody {
		t.Fatalf("charging shape = %gx%g", w, h)
	}
	if dw, dh := a.downloadShape(); dw != w || dh != h {
		t.Fatalf("download shape = %gx%g, want the same wings %gx%g", dw, dh, w, h)
	}
	for _, ev := range []liveEvent{
		{kind: liveHeadset, label: "AirPods Pro", levels: []string{"左 80%", "右 78%", "盒 50%"}},
		{kind: liveHeadset, label: "Sony WH-1000XM5"},
		{kind: liveHeadsetOff, label: "AirPods Pro"},
		{kind: liveDownloadDone, label: "设计稿-v3.zip"},
	} {
		cw, ch := a.liveShape(ev)
		if ch != float32(a.notchH)+fbBody {
			t.Fatalf("%v: capsule height %g, want %d", ev.label, ch, a.notchH+fbBody)
		}
		if cw < float32(max(180, a.notchW))+60 || cw > fbMaxW {
			t.Fatalf("%v: capsule width %g outside the shell's range", ev.label, cw)
		}
	}
}

// A settings.json written before this feature keeps every activity on.
func TestLiveSettingsDefaultOnForOldFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, settings.FileName), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LiveActivities.Charging || !got.LiveActivities.Headphones || !got.LiveActivities.Downloads {
		t.Fatalf("old settings.json turned activities off: %+v", got.LiveActivities)
	}
}

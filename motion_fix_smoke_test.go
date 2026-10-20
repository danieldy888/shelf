//go:build darwin

package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"shelf/internal/mac"
)

const fixShotDir = "/tmp/shelf-fix"

type motionSample struct {
	ms                  float64
	w, h, openK, peekK  float32
	window, desired     mygo.Rectangle
	ignoring, nativeHit bool
}

// sampleMotion reads the last rendered shell and the actual native frame.
// The 8ms cadence catches display frames without driving the spring clock.
func sampleMotion(a *shelf, span time.Duration) []motionSample {
	start := time.Now()
	var samples []motionSample
	for time.Since(start) < span {
		var row motionSample
		onMain(func() {
			row = motionSample{
				ms: float64(time.Since(start)) / float64(time.Millisecond),
				w:  a.fx.w, h: a.fx.h, openK: a.fx.openK, peekK: a.fx.peekK,
				window: a.win.Bounds(), desired: a.desiredFrame(), ignoring: a.ignoringMouse,
			}
			num := mac.WindowNumber(a.winH)
			screenH := float64(a.dispBounds.Y + a.dispBounds.Height)
			row.nativeHit = mac.WindowAtPoint(float64(a.stripCenter()), float64(a.y+a.notchH+4), screenH) == num
		})
		samples = append(samples, row)
		time.Sleep(8 * time.Millisecond)
	}
	return samples
}

func saveMotion(t *testing.T, name string, samples []motionSample) {
	t.Helper()
	if err := os.MkdirAll(fixShotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixShotDir, name+".csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := csv.NewWriter(f)
	if err := out.Write([]string{"ms", "shell_w", "shell_h", "open_k", "peek_k", "window_x", "window_y", "window_w", "window_h", "desired_w", "desired_h", "ignoring_mouse", "native_hit", "ear_margin"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range samples {
		row := []string{
			fmt.Sprintf("%.3f", s.ms), fmt.Sprintf("%.3f", s.w), fmt.Sprintf("%.3f", s.h),
			fmt.Sprintf("%.5f", s.openK), fmt.Sprintf("%.5f", s.peekK),
			fmt.Sprint(s.window.X), fmt.Sprint(s.window.Y), fmt.Sprint(s.window.Width), fmt.Sprint(s.window.Height),
			fmt.Sprint(s.desired.Width), fmt.Sprint(s.desired.Height), fmt.Sprint(s.ignoring), fmt.Sprint(s.nativeHit),
			fmt.Sprintf("%.3f", (float64(s.window.Width)-float64(s.w))/2-earR),
		}
		if err := out.Write(row); err != nil {
			t.Fatal(err)
		}
	}
	out.Flush()
	if err := out.Error(); err != nil {
		t.Fatal(err)
	}
	t.Log(path)
}

func checkMotion(t *testing.T, name string, samples []motionSample, closing bool, baseH float32) {
	t.Helper()
	minEar, maxGrowth := math.Inf(1), float32(0)
	firstFold, firstPass := -1.0, -1.0
	for i, s := range samples {
		margin := (float64(s.window.Width)-float64(s.w))/2 - earR
		minEar = min(minEar, margin)
		if margin < 0 || float32(s.window.Height) < s.h {
			t.Errorf("%s at %.1fms: actual frame %+v clips shell %.2fx%.2f including its ears", name, s.ms, s.window, s.w, s.h)
			break
		}
		if closing && i > 0 {
			maxGrowth = max(maxGrowth, s.w-samples[i-1].w, s.h-samples[i-1].h)
		}
		if s.h <= baseH+2 && firstFold < 0 {
			firstFold = s.ms
		}
		if s.ignoring && !s.nativeHit && firstPass < 0 {
			firstPass = s.ms
		}
	}
	if closing {
		if maxGrowth > 2 {
			t.Errorf("%s grew %.2fpt after close", name, maxGrowth)
		}
		last := samples[len(samples)-1]
		if !last.ignoring || last.nativeHit {
			t.Errorf("%s: the collapsed native window still receives clicks", name)
		}
		if firstFold < 0 || firstPass < 0 || firstPass-firstFold > 25 {
			t.Errorf("%s: folded at %.1fms, native pass-through at %.1fms", name, firstFold, firstPass)
		}
	}
	t.Logf("%s: ear margin min %.2fpt, outward growth max %.2fpt, folded %.1fms, pass-through %.1fms", name, minEar, maxGrowth, firstFold, firstPass)
}

func TestFixNativeMotionTraces(t *testing.T) {
	a := redesignShelf(t)
	motionClosed(a)
	time.Sleep(600 * time.Millisecond)
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		tick := time.NewTicker(pointerInterval)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-tick.C:
				onMain(func() { a.animate(now) })
			}
		}
	}()
	defer func() { close(stop); <-stopped; motionClosed(a) }()
	baseH := float32(max(32, a.notchH))
	onMain(func() { a.show(false) })
	open := sampleMotion(a, 900*time.Millisecond)
	saveMotion(t, "trace-open", open)
	checkMotion(t, "open", open, false, baseH)
	var peak float32
	for _, s := range open {
		peak = max(peak, s.h)
	}
	if peak <= open[len(open)-1].h+1 {
		t.Errorf("the native opening never overshot: peak %.2f, settled %.2f", peak, open[len(open)-1].h)
	}
	onMain(func() { a.hide(hidePassive) })
	closed := sampleMotion(a, 800*time.Millisecond)
	saveMotion(t, "trace-close", closed)
	checkMotion(t, "close", closed, true, baseH)
	onMain(func() { a.show(false) })
	time.Sleep(100 * time.Millisecond)
	onMain(func() { a.hide(hidePassive) })
	interrupted := sampleMotion(a, 800*time.Millisecond)
	saveMotion(t, "trace-mid-open-close", interrupted)
	checkMotion(t, "mid-open-close", interrupted, true, baseH)
	onMain(func() { a.show(false) })
	time.Sleep(700 * time.Millisecond)
	onMain(func() { a.hide(hidePassive) })
	time.Sleep(100 * time.Millisecond)
	onMain(func() { a.show(false) })
	reopened := sampleMotion(a, 900*time.Millisecond)
	saveMotion(t, "trace-mid-close-open", reopened)
	checkMotion(t, "mid-close-open", reopened, false, baseH)
	if reopened[len(reopened)-1].openK < 0.99 {
		t.Error("the interrupted close never reopened")
	}
}

func fixCapsuleShot(t *testing.T, a *shelf, name string) {
	t.Helper()
	time.Sleep(650 * time.Millisecond)
	var num int
	onMain(func() {
		a.syncWindowFrame()
		num = mac.WindowNumber(a.winH)
		if !a.fb.drawn || (!a.fb.live && !a.fb.armed) || a.fb.retracting {
			t.Errorf("%s: capsule content did not draw (drawn=%v armed=%v retracting=%v)", name, a.fb.drawn, a.fb.armed, a.fb.retracting)
		}
	})
	if err := os.MkdirAll(fixShotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixShotDir, name+".png")
	if out, err := exec.Command("screencapture", "-x", "-o", fmt.Sprintf("-l%d", num), path).CombinedOutput(); err != nil {
		t.Fatalf("capture %s: %v: %s", name, err, out)
	}
	img, err := pngDecode(path)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() < 200 || img.Bounds().Dy() < 40 {
		t.Fatalf("%s: invalid capture size %v", name, img.Bounds())
	}
	t.Log(path)
}

func TestFixNativeCapsuleScreenshots(t *testing.T) {
	a := redesignShelf(t)
	reset := func() {
		onMain(func() {
			a.conv = nil
			a.np = nil
			a.dragging, a.ownDrag = false, false
		})
		motionClosed(a)
		time.Sleep(600 * time.Millisecond)
	}
	defer reset()
	for _, tc := range []struct {
		name, text string
		kind       statusKind
	}{
		{"capsule-success", "已复制 · 在其他应用按 ⌘V 粘贴", statusOK},
		{"capsule-info", "已取消 · 已完成 2 项", statusInfo},
		{"capsule-error", "转换失败：文件无法读取", statusErr},
	} {
		reset()
		onMain(func() { a.toastStatus(tc.text, time.Now().Add(6*time.Second), tc.kind) })
		fixCapsuleShot(t, a, tc.name)
	}
	reset()
	onMain(func() {
		a.conv = &converter{host: a, job: fixLiveJob()}
		a.win.Invalidate()
	})
	fixCapsuleShot(t, a, "capsule-running")
	reset()
	onMain(func() {
		a.settings.IslandNowPlaying = true
		fakeTrack(a, true)
		a.toastStatus("已转换 3 项 · 已选中，可直接拖出", time.Now().Add(6*time.Second), statusOK)
	})
	fixCapsuleShot(t, a, "capsule-success-with-track")
	reset()
	onMain(func() { a.toastStatus("第一条提示", time.Now().Add(6*time.Second), statusOK) })
	time.Sleep(650 * time.Millisecond)
	onMain(func() {
		a.fb.until = time.Now().Add(-time.Millisecond)
		a.win.Invalidate()
	})
	time.Sleep(90 * time.Millisecond)
	onMain(func() {
		if !a.fb.retracting {
			t.Error("the first capsule was not retracting when the second arrived")
		}
		a.toastStatus("第二条提示 · 内容已完成", time.Now().Add(6*time.Second), statusOK)
	})
	fixCapsuleShot(t, a, "capsule-reposted-mid-retract")
}

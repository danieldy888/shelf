//go:build darwin

package main

// Real-window evidence for the back row without a back button: 托盘、
// 剪贴板 and 主页 are screenshotted whole and cropped to the title row, so
// the plain 「名称 数量」 title and the missing ‹ can be checked by eye.
// Run with SHELF_BASKET_SMOKE=1; the PNGs land in /tmp/shelf-noback. The
// test instance uses the smoke data dir and never touches
// ~/Library/Application Support/Shelf.

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"shelf/internal/mac"
)

const noBackShotDir = "/tmp/shelf-noback"

// shotNoBack screenshots the native notch window to name.png in the
// evidence directory and returns its path.
func shotNoBack(t *testing.T, a *shelf, name string) string {
	t.Helper()
	if a.win == nil {
		t.Fatal("no notch window")
	}
	num := mac.WindowNumber(a.win.NativeHandle())
	if num == 0 {
		t.Fatalf("%s: window has no CGWindowID", name)
	}
	time.Sleep(350 * time.Millisecond) // let the drawn frame settle
	if err := os.MkdirAll(noBackShotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(noBackShotDir, name+".png")
	out, err := exec.Command("screencapture", "-x", "-o", fmt.Sprintf("-l%d", num), path).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: screencapture failed (%v): %s", name, err, out)
	}
	t.Logf("shot %s -> %s", name, path)
	return path
}

// cropTop saves the top rowsAtDIP of a shot as name.png: the menu-bar band
// and the back row it sits under, at the screenshot's own scale.
func cropTop(t *testing.T, from, name string, rowsAtDIP, frameW float32) {
	t.Helper()
	file, err := os.Open(from)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	b := img.Bounds()
	scale := float64(b.Dx()) / float64(frameW)
	h := int(math.Ceil(float64(rowsAtDIP) * scale))
	if h <= 0 || h > b.Dy() {
		t.Fatalf("%s: crop %d rows of %d at scale %.2f", name, h, b.Dy(), scale)
	}
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		t.Fatalf("%s: %T does not crop", name, img)
	}
	out, err := os.Create(filepath.Join(noBackShotDir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, sub.SubImage(image.Rect(0, 0, b.Dx(), h))); err != nil {
		t.Fatal(err)
	}
}

func TestSmokeBackRowTitleWithoutBackButton(t *testing.T) {
	a := redesignShelf(t)
	if err := os.MkdirAll(noBackShotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	onMain(func() {
		a.store.SetTextLimit(300) // keep all 43 texts: 12 tray + 31 clipboard
		for i := range 12 {
			if err := a.store.AddText(fmt.Sprintf("托盘内容 %d · 一段足够长的说明文字", i)); err != nil {
				t.Error(err)
			}
		}
		for i := range 31 {
			if err := a.store.AddClipText(fmt.Sprintf("剪贴板内容 %d · 一段足够长的文字用于检查省略", i), "com.apple.Safari"); err != nil {
				t.Error(err)
			}
		}
	})
	t.Cleanup(func() { onMain(func() { a.store.SetTextLimit(0) }) })
	expandShelf(t, a)
	var frame mygo.Rectangle
	onMain(func() { frame = a.desiredFrame() })
	// The band names the window; the back row follows immediately under it.
	rows := float32(bandH + backRowH + contentGap)
	for _, page := range []struct {
		name string
		id   pageID
	}{
		{"tray-title", pageTray},
		{"clipboard-title", pageClipboard},
		{"home-title", pageHome},
	} {
		onMain(func() {
			a.setTab(page.id)
			a.win.Invalidate()
		})
		if page.id != pageTray {
			time.Sleep(400 * time.Millisecond) // the switch's 240ms fade
		}
		shot := shotNoBack(t, a, page.name)
		cropTop(t, shot, page.name+"-row", rows, float32(frame.Width))
	}
}

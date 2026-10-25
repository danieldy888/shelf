//go:build darwin

package main

// Regression: the expanded panel was capped at panelHeight (400), the
// window's initial size, so the tallest page (AI 用量 with quota cards, a
// local summary and the now-playing strip) scrolled inside the stage on a
// display that had room for the design's panelMaxH. The cap is panelMaxH,
// still clamped to the display's work area.

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/aiusage"
	"shelf/internal/mac"
	"shelf/internal/settings"
)

// playingUsageShelf reproduces place() on a 1470×956 notched display whose
// work area is workAreaH tall, with a track playing and the AI 用量 page
// showing both quota cards plus a finished local scan: the tallest panel
// content the app lays out.
func playingUsageShelf(t *testing.T, workAreaH int) (*shelf, *usageFeature) {
	t.Helper()
	stubUsageSeams(t, false)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	a := &shelf{
		dataDir: t.TempDir(), visible: true, settings: settings.Default(),
		thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{},
		appNames: map[string]string{},
	}
	a.settings.AIUsage.Enabled = true
	a.settings.IslandNowPlaying = true
	a.dispBounds = mygo.Rectangle{X: 0, Y: 0, Width: 1470, Height: 956}
	a.notchX, a.notchY, a.notchW, a.notchH, a.hasNotch = 645, 0, 180, 32, true
	a.topInset = a.notchH
	a.x = a.notchX + a.notchW/2 - panelWidth/2
	a.y = 0
	a.h = panelCap(workAreaH)
	f := newUsageFeature(a, usageDeps{
		fetchCodex:  func(context.Context) aiusage.Quota { return okQuota(aiusage.ToolCodex, "Plus", now) },
		fetchClaude: func(context.Context) aiusage.Quota { return okQuota(aiusage.ToolClaude, "Pro", now) },
		pace:        fakePace,
		newScanner:  func() usageScanner { return &fakeUsageScanner{} },
		now:         func() time.Time { return now },
	})
	a.registerPage(f)
	np := &nowPlayingModule{host: a, have: true, last: mac.NowPlayingState{
		Playing: true, Title: "Track", Artist: "Artist", App: "com.apple.Music",
		Duration: 200, Elapsed: 30, Rate: 1, Received: now,
	}}
	a.np = np
	a.modules = []module{np}
	a.setTab(pageUsage)
	f.tick(now) // the local summary lands, so the stage is at its full height
	t.Cleanup(f.stopWork)
	return a, f
}

// TestPanelHeightReachesDesignMaxWithMusicAndUsage is the fix's acceptance:
// with the strip up and the AI 用量 page full, the panel is tall enough for
// its whole content (the stage never scrolls) and stops at panelMaxH.
func TestPanelHeightReachesDesignMaxWithMusicAndUsage(t *testing.T) {
	a, _ := playingUsageShelf(t, 956)
	content := a.panelContentH()
	if content+8 <= panelHeight {
		t.Fatalf("fixture too short to prove the bug: content %d fits the old %d cap", content, panelHeight)
	}
	h := a.panelH()
	if h < content+8 {
		t.Fatalf("panel %d cannot hold its content (%d+8): the stage must scroll", h, content)
	}
	if h > panelMaxH {
		t.Fatalf("panel %d past the design bound %d", h, panelMaxH)
	}
	if frame := a.desiredFrame(); frame.Height != h {
		t.Fatalf("window frame %d does not follow the panel %d", frame.Height, h)
	}
}

// TestPanelCapFollowsTheWorkArea pins the bound itself: the design maximum
// until the display is too short, then the work area minus the menu-bar and
// Dock slack, never below the 240 floor.
func TestPanelCapFollowsTheWorkArea(t *testing.T) {
	for _, tc := range []struct {
		workArea, want int
	}{
		{1200, panelMaxH},
		{956, panelMaxH},
		{584, panelMaxH},
		{583, 559},
		{500, 476},
		{264, 240},
		{180, 240},
	} {
		if got := panelCap(tc.workArea); got != tc.want {
			t.Errorf("panelCap(%d) = %d, want %d", tc.workArea, got, tc.want)
		}
	}
}

// A short work area still wins over the content: the panel is clamped to
// the screen (the stage then scrolls) instead of growing past the Dock.
func TestPanelHeightStaysClampedOnAShortDisplay(t *testing.T) {
	a, _ := playingUsageShelf(t, 500)
	if got := a.panelH(); got != 476 || got > panelCap(500) {
		t.Fatalf("panel %d on a 500pt work area, want the 476 cap", got)
	}
	if frame := a.desiredFrame(); frame.Height != 476 {
		t.Fatalf("window frame %d not the clamped panel 476", frame.Height)
	}
	b, _ := playingUsageShelf(t, 400)
	if got := b.panelH(); got != 376 {
		t.Fatalf("panel %d on a 400pt work area, want 376", got)
	}
	if b.panelH() >= b.panelContentH()+8 {
		t.Fatal("a very short display must clip the stage, not fit it")
	}
}

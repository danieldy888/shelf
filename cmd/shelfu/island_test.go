//go:build darwin

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/settings"
)

// islandShelf is a headless shelf on a 1470 × 956 display whose notch is
// the usual centred 180 × 32 (mac.NotchRect's fallback box). hasNotch
// decides whether the screen has a camera housing; style is the saved
// preference, and applyShellStyle is the same call place() makes.
func islandShelf(hasNotch bool, style settings.ShellStyle) *shelf {
	s := settings.Default()
	s.ShellStyle = style
	a := &shelf{
		dispBounds: mygo.Rectangle{X: 0, Y: 0, Width: 1470, Height: 956},
		notchX:     645, notchY: 0, notchW: 180, notchH: 32,
		x: (1470 - sheetW) / 2, y: 0,
		hasNotch:     hasNotch,
		settings:     s,
		thumbnails:   map[string]*ui.Bitmap{},
		thumbPending: map[string]bool{},
	}
	a.applyShellStyle()
	return a
}

func TestIslandShellChoice(t *testing.T) {
	for _, tt := range []struct {
		name     string
		hasNotch bool
		notchH   int
		style    settings.ShellStyle
		island   bool
		top      int
	}{
		{"notched screen keeps the notch shape by default", true, 32, settings.ShellStyleNotch, false, 0},
		{"notched screen without a saved style keeps the notch", true, 38, "", false, 0},
		{"notched screen switches to the island", true, 32, settings.ShellStyleIsland, true, 38},
		{"a taller housing pushes the island further down", true, 38, settings.ShellStyleIsland, true, 44},
		{"a screen without a housing is always the island", false, 32, settings.ShellStyleNotch, true, islandTopGap},
		{"even when the preference says island", false, 32, settings.ShellStyleIsland, true, islandTopGap},
	} {
		t.Run(tt.name, func(t *testing.T) {
			island, top := islandShell(tt.hasNotch, tt.notchH, tt.style)
			if island != tt.island || top != tt.top {
				t.Fatalf("islandShell(%v, %d, %q) = %v, %d; want %v, %d",
					tt.hasNotch, tt.notchH, tt.style, island, top, tt.island, tt.top)
			}
		})
	}
	// An unknown value in settings.json never paints a shape the app lacks.
	if settings.ShellStyle("island2").Normalized() != settings.ShellStyleNotch {
		t.Fatal("an unknown shell style must fall back to the notch")
	}
}

func TestIslandRestingGeometry(t *testing.T) {
	a := islandShelf(false, settings.ShellStyleNotch)
	if w, h := a.anchorSize(); w != islandIdleW || h != islandIdleH {
		t.Fatalf("anchor size = %.0fx%.0f, want %dx%d", w, h, islandIdleW, islandIdleH)
	}
	r := a.islandRect()
	want := mygo.Rectangle{X: 735 - islandIdleW/2, Y: islandTopGap, Width: islandIdleW, Height: islandIdleH}
	if r != want {
		t.Fatalf("idle capsule = %+v, want %+v", r, want)
	}
	// The resident strip holds the largest peek shape (the chevron), and
	// the shell's top inset is part of the window frame.
	f := a.desiredFrame()
	if f.Y != 0 || f.Height != islandTopGap+islandIdleH+peekBody {
		t.Fatalf("resting frame %+v must start at the display top and hold 7 + 48", f)
	}
	if f.X != 735-int(120+40)/2 || f.Width != 120+40 {
		t.Fatalf("resting frame %+v not centred on the capsule axis", f)
	}
	// Hover exactly where the capsule is; the rows above it are the menu
	// bar's, not a hot zone.
	for _, p := range []mygo.Point{{X: 735, Y: islandTopGap}, {X: 735, Y: islandTopGap + islandIdleH - 1}, {X: 680, Y: 20}} {
		if !a.overAnchor(p) {
			t.Fatalf("capsule point %+v must hover", p)
		}
	}
	for _, p := range []mygo.Point{{X: 735, Y: islandTopGap - notchHoverPadY - 1}, {X: 735, Y: islandTopGap + islandIdleH + notchHoverPadY + 1}, {X: 600, Y: 20}} {
		if a.overAnchor(p) {
			t.Fatalf("point %+v outside the capsule hovered", p)
		}
	}
}

func TestIslandUnderNotchSitsBelowTheHousing(t *testing.T) {
	a := islandShelf(true, settings.ShellStyleIsland)
	if !a.island || a.islandTop != 38 {
		t.Fatalf("notched island style = island %v top %d, want true/38", a.island, a.islandTop)
	}
	r := a.islandRect()
	if r.Y != 38 || r.Width != islandIdleW || r.Height != islandIdleH {
		t.Fatalf("capsule %+v must hang 6pt below the 32pt housing", r)
	}
	// The camera housing is covered by hardware; hovering it must not
	// pretend the island is there.
	if !a.overAnchor(mygo.Point{X: 735, Y: 40}) {
		t.Fatal("capsule below the notch must hover")
	}
	if a.overAnchor(mygo.Point{X: 735, Y: 20}) {
		t.Fatal("the notch itself must not be the island's hot zone")
	}
	if !a.dropBand(mygo.Point{X: 735, Y: 39}) {
		t.Fatal("the capsule's body must enter the drop band")
	}
	if a.dropBand(mygo.Point{X: 735, Y: 20}) {
		t.Fatal("the housing above the capsule must not enter the band")
	}
}

func TestIslandShellShapes(t *testing.T) {
	a := islandShelf(false, settings.ShellStyleNotch)
	if w, h := a.shellSize(0, 0, 0, 0, 0); w != islandIdleW || h != islandIdleH {
		t.Fatalf("folded shell = %.0fx%.0f, want 120x30", w, h)
	}
	if w, h := a.wingsSize(); w != 224 || h != 34 {
		t.Fatalf("wings = %.0fx%.0f, want 224x34 (120 + 2×52, 30 + 4)", w, h)
	}
	a.fb.pillW = 200
	if w, h := a.capsuleSize(); w != 247 || h != islandCapsuleH {
		t.Fatalf("capsule = %.0fx%.0f, want 247x36", w, h)
	}
	if w, h := a.shellSize(1, 0, 0, 0, 0); w != islandW || h != islandH {
		t.Fatalf("level 1 = %.0fx%.0f, want %dx%d", w, h, islandW, islandH)
	}
	if w, h := a.shellSize(1, 1, 0, 0, 0); w != sheetW || h != sheetH {
		t.Fatalf("level 2 = %.0fx%.0f, want %dx%d", w, h, sheetW, sheetH)
	}
}

// The island's window never reserves the notch path's ear margins: an
// expanded 340pt island fits in 346pt (the spring's 1.5%), not 370.
func TestIslandPanelFrameHasNoEars(t *testing.T) {
	a := islandShelf(false, settings.ShellStyleNotch)
	a.visible, a.level = true, 1
	a.fx.w, a.fx.h, a.fx.openMoving = islandW, islandH, true
	f := a.desiredFrame()
	if f.Width != 346 {
		t.Fatalf("island panel frame %d wide, want 346 (no ear margins)", f.Width)
	}
	_, wantH := a.frameRoom(islandW, islandH+islandTopGap)
	if f.Y != 0 || f.Height != wantH {
		t.Fatalf("island panel frame %+v, want top 0 and height %d", f, wantH)
	}
	// The notch style keeps its ears.
	n := islandShelf(true, settings.ShellStyleNotch)
	n.visible, n.level = true, 1
	n.fx.w, n.fx.h, n.fx.openMoving = islandW, islandH, true
	if w := n.desiredFrame().Width; w != 346+2*earR {
		t.Fatalf("notch panel frame %d wide, want %d", w, 346+2*earR)
	}
}

func TestIslandHitShapeIsARoundedRect(t *testing.T) {
	a := islandShelf(false, settings.ShellStyleNotch)
	// The idle capsule is 120 × 30 at y=7: its left edge starts at 675.
	a.fx.w, a.fx.h = islandIdleW, islandIdleH
	if a.overShell(mygo.Point{X: 674, Y: 12}) {
		t.Fatal("a point left of the capsule must not take clicks")
	}
	if !a.overShell(mygo.Point{X: 735, Y: 7}) || !a.overShell(mygo.Point{X: 786, Y: 35}) {
		t.Fatal("points inside the capsule must take clicks")
	}
	// The capsule's ends are half-circle high: at 5pt below its top edge
	// the quadratic corner cuts 2.7pt in.
	capsuleTop := mygo.Point{X: 675, Y: panelTop(a) + 5}
	if a.overShellShape(capsuleTop, islandIdleW, islandIdleH, 0, 0) {
		t.Fatalf("point %+v is outside the capsule's round end", capsuleTop)
	}
	if !a.overShellShape(mygo.Point{X: 679, Y: panelTop(a) + 5}, islandIdleW, islandIdleH, 0, 0) {
		t.Fatal("the capsule's body must take a point just past the corner")
	}
	// A panel's top corners are round: at 5pt below the top edge the island
	// path is 8pt in (radius 22), while the notch path's ear reaches 1.5pt
	// past the flank.
	panel := islandShelf(false, settings.ShellStyleNotch)
	p := mygo.Point{X: 565, Y: panelTop(panel) + 5}
	if panel.overShellShape(p, islandW, islandH, 0, 0) {
		t.Fatalf("point %+v is outside the island's round top-left corner", p)
	}
	if !panel.overShellShape(mygo.Point{X: 574, Y: p.Y}, islandW, islandH, 0, 0) {
		t.Fatal("the island panel must take a point inside its corner")
	}
	notch := islandShelf(true, settings.ShellStyleNotch)
	if !notch.overShellShape(p, islandW, islandH, 0, 0) {
		t.Fatalf("the notch path's ear must still take %+v", p)
	}
	// The sheet's radius is 24, not the island's 22.
	sheet := islandShelf(false, settings.ShellStyleNotch)
	sp := mygo.Point{X: 735 - sheetW/2, Y: panelTop(sheet) + 5}
	if sheet.overShellShape(sp, sheetW, sheetH, 1, 0) {
		t.Fatalf("point %+v is outside the sheet's round corner", sp)
	}
	if !sheet.overShellShape(mygo.Point{X: sp.X + 10, Y: sp.Y}, sheetW, sheetH, 1, 0) {
		t.Fatal("the sheet must take a point inside its 24pt corner")
	}
}

func TestIslandDragBandAndGuard(t *testing.T) {
	a := islandShelf(false, settings.ShellStyleNotch)
	cx := 735
	r := a.islandRect()
	if !a.dropBand(mygo.Point{X: cx, Y: r.Y}) {
		t.Fatal("the island's top row must enter the band")
	}
	if a.dropBand(mygo.Point{X: cx, Y: r.Y + islandIdleH + dropBandH(islandIdleH)}) {
		t.Fatal("one row past the landing range still entered the band")
	}
	if !a.dropBandX(mygo.Point{X: cx, Y: r.Y + dropBandH(islandIdleH) - 1}) {
		t.Fatal("the guard's landing row left the band's hold range")
	}
	if a.dropBandX(mygo.Point{X: r.X - dropBandPadX - 1, Y: r.Y + 10}) {
		t.Fatal("outside the capsule's width still counts as in-band")
	}
	// Resting corridor: it starts at the display's top edge (the Spaces
	// edge) but lands inside the capsule, so a dragged file can reach it.
	g := a.dragGuardRegion()
	if g.Y != 0 || g.Height != islandTopGap+islandIdleH-1 {
		t.Fatalf("guard %+v, want the top edge down to the capsule's last row", g)
	}
	landing := mygo.Point{X: cx, Y: g.Y + g.Height}
	if !a.overShellShape(landing, islandIdleW, islandIdleH, 0, 0) {
		t.Fatalf("guard landing %+v is not on the idle capsule", landing)
	}
	// In-band it reaches the grown drop pill's landing row.
	a.inBand = true
	g = a.dragGuardRegion()
	if g.Height != islandTopGap+dropBandH(islandIdleH)-1 {
		t.Fatalf("in-band guard %d high, want %d", g.Height, islandTopGap+dropBandH(islandIdleH)-1)
	}
	if landing := (mygo.Point{X: cx, Y: g.Y + g.Height}); !a.overShellShape(landing, islandIdleW+dropPillExtraW, islandIdleH+dropPillBody, 0, 0) {
		t.Fatalf("in-band landing %+v is not on the drop pill", landing)
	}
	// The notch path is untouched.
	n := islandShelf(true, settings.ShellStyleNotch)
	if g := n.dragGuardRegion(); g.Height != menuBandH(n.notchH)-1 || g.Y != 0 {
		t.Fatalf("notch guard changed: %+v", g)
	}
}

func TestIslandPeekPillFollowsTheFloatingShell(t *testing.T) {
	a := islandShelf(false, settings.ShellStyleNotch)
	a.fx.w, a.fx.h = islandIdleW, islandIdleH
	pill := a.peekPillRect()
	if pill.Y != islandTopGap {
		t.Fatalf("peek pill Y = %d, want the island's top inset %d", pill.Y, islandTopGap)
	}
	if !a.overPeekPill(mygo.Point{X: 735, Y: pill.Y + 10}) {
		t.Fatal("a press on the idle island must reach the pill")
	}
	// A press on the menu bar above the capsule is not the island's.
	if a.overPeekPill(mygo.Point{X: 735, Y: islandTopGap - 3}) {
		t.Fatal("the gap above the capsule took a press")
	}
}

func TestIdleIslandPlacements(t *testing.T) {
	displays := []islandDisplay{
		{ID: 1, Bounds: mygo.Rectangle{X: 0, Y: 0, Width: 1470, Height: 956}, HasNotch: true},
		{ID: 2, Bounds: mygo.Rectangle{X: 1470, Y: 0, Width: 2560, Height: 1440}},
		{ID: 3, Bounds: mygo.Rectangle{X: 0, Y: 956, Width: 1920, Height: 1080}},
	}
	got := idleIslandPlacements(displays, 1, settings.ShellStyleNotch)
	want := []idlePlacement{
		{DisplayID: 2, Frame: mygo.Rectangle{X: 1470 + (2560-islandIdleW)/2, Y: islandTopGap, Width: islandIdleW, Height: islandIdleH}},
		{DisplayID: 3, Frame: mygo.Rectangle{X: (1920 - islandIdleW) / 2, Y: 956 + islandTopGap, Width: islandIdleW, Height: islandIdleH}},
	}
	if len(got) != len(want) {
		t.Fatalf("placements = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("placement %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	// The display the resident window lives on draws its own capsule; the
	// others keep theirs. A notched screen never gets an extra one.
	onOther := idleIslandPlacements(displays, 2, settings.ShellStyleIsland)
	if len(onOther) != 1 || onOther[0].DisplayID != 3 {
		t.Fatalf("with the shelf on display 2, placements = %+v", onOther)
	}
}

func TestShellStyleSwitchIsImmediateAndSaved(t *testing.T) {
	a := islandShelf(true, settings.ShellStyleNotch)
	a.dataDir = t.TempDir()
	if a.island {
		t.Fatal("a notched screen must start on the notch shape")
	}
	a.setShellStyle(settings.ShellStyleIsland)
	if !a.island || a.islandTop != 38 {
		t.Fatalf("switching to the island left island=%v top=%d", a.island, a.islandTop)
	}
	saved, err := settings.Load(a.dataDir)
	if err != nil || saved.ShellStyle != settings.ShellStyleIsland {
		t.Fatalf("saved style = %q, %v; want island", saved.ShellStyle, err)
	}
	a.setShellStyle(settings.ShellStyleNotch)
	if a.island || a.islandTop != 0 {
		t.Fatalf("switching back left island=%v top=%d", a.island, a.islandTop)
	}
}

func TestSettingsShellStyleRow(t *testing.T) {
	stubSettingsSeams(t, false, nil, nil)
	a, view := newSettingsShelf(t)
	if !view.HasText("刘海样式") || !view.HasText("没有刘海的屏幕总是显示为灵动岛") {
		t.Fatalf("刘海样式 row or its caption is missing: %v", view.Texts())
	}
	clickVisible(t, view, "灵动岛")
	if a.settings.ShellStyle != settings.ShellStyleIsland {
		t.Fatalf("picking 灵动岛 left %q", a.settings.ShellStyle)
	}
	saved, err := settings.Load(a.dataDir)
	if err != nil || saved.ShellStyle != settings.ShellStyleIsland {
		t.Fatalf("settings.json style = %q, %v", saved.ShellStyle, err)
	}
}

// Hovering the idle capsule is the monitor's normal path: the pointer
// enters the anchor, the peek shape grows. Leaving it folds back.
// panelTop is the shell's y inside the window for the geometry assertions.
func panelTop(a *shelf) int { return a.y + a.sheetTop() }

func TestIslandHoverPeekFromMonitor(t *testing.T) {
	a := islandShelf(false, settings.ShellStyleNotch)
	a.settings.IslandNowPlaying = false
	driveMonitor(t, a, mygo.Point{X: 735, Y: islandTopGap + 10}, false, 1, nil)
	if !a.peeking {
		t.Fatal("the pointer on the idle capsule did not peek")
	}
	driveMonitor(t, a, mygo.Point{X: 735, Y: 400}, false, 1, nil)
	if a.peeking {
		t.Fatal("the pointer away from the capsule left it peeked")
	}
}

// A press on the visible idle capsule opens the shelf even when the press
// was delivered to another process (the capsule sits over the desktop).
func TestIslandIdlePressOpens(t *testing.T) {
	a := islandShelf(false, settings.ShellStyleNotch)
	a.fx.w, a.fx.h = islandIdleW, islandIdleH
	a.mouseDown(false, 0, mygo.Point{X: 735, Y: islandTopGap + 5})
	if !a.visible {
		t.Fatal("a press on the idle island did not open the shelf")
	}
	// Transparent margins are not the capsule.
	b := islandShelf(false, settings.ShellStyleNotch)
	b.fx.w, b.fx.h = islandIdleW, islandIdleH
	b.mouseDown(false, 0, mygo.Point{X: 735 - islandIdleW/2 - 5, Y: islandTopGap + 5})
	if b.visible {
		t.Fatal("a press beside the capsule opened the shelf")
	}
}

// Presses on the other screens' idle capsules are recognised by their
// window rects, so the resident shelf can move there.
func TestIdleIslandCapsuleHit(t *testing.T) {
	a := &shelf{idleIslands: []*idleIslandWindow{
		{dispID: 2, frame: mygo.Rectangle{X: 2690, Y: 7, Width: islandIdleW, Height: islandIdleH}},
	}}
	if !a.idleIslandAt(mygo.Point{X: 2750, Y: 20}) {
		t.Fatal("the other screen's capsule was not hit")
	}
	if a.idleIslandAt(mygo.Point{X: 2750, Y: 40}) || a.idleIslandAt(mygo.Point{X: 100, Y: 20}) {
		t.Fatal("a point outside the capsule was hit")
	}
}

// The notched shelf at rest draws nothing over its transparent strip, so
// every press there must go to the menu bar or the app underneath; only
// the island's visible capsule may take an idle press. (A stale drawn
// shell must not widen that rule: regression for the click-through smoke.)
func TestIdleMouseAcceptancePerStyle(t *testing.T) {
	orig := cursorPoint
	defer func() { cursorPoint = orig }()

	notch := islandShelf(true, settings.ShellStyleNotch)
	notch.winH = 1
	notch.fx.w, notch.fx.h = 284, 36 // the last peek shape drawn
	cursorPoint = func() mygo.Point { return mygo.Point{X: 735, Y: 6} }
	if notch.overlayAcceptsMouse() {
		t.Fatal("the idle notch strip took a press on its stale drawn shell")
	}

	island := islandShelf(false, settings.ShellStyleNotch)
	island.winH = 1
	island.fx.w, island.fx.h = islandIdleW, islandIdleH
	cursorPoint = func() mygo.Point { return mygo.Point{X: 735, Y: islandTopGap + 10} }
	if !island.overlayAcceptsMouse() {
		t.Fatal("the idle island capsule must take a press")
	}
	cursorPoint = func() mygo.Point { return mygo.Point{X: 735, Y: islandTopGap - 3} }
	if island.overlayAcceptsMouse() {
		t.Fatal("the gap above the island capsule took a press")
	}
}

// The realtime activities keep A's notch geometry there, and use the
// island's continuous pill on the island: 88pt per side (the preview's
// value; no camera gap to route around), a 36pt capsule, and the same
// queue the power/audio/download modules push to.
func TestIslandLiveShapes(t *testing.T) {
	a := islandShelf(false, settings.ShellStyleNotch)
	if a.liveWingW() != islandLiveWingW {
		t.Fatalf("island wing = %d, want %d", a.liveWingW(), islandLiveWingW)
	}
	if w, h := a.liveShape(liveEvent{kind: liveCharging, label: "正在充电", pct: 80}); w != 296 || h != 34 {
		t.Fatalf("island charging shape = %.0fx%.0f, want 296x34", w, h)
	}
	if w, h := a.liveShape(liveEvent{kind: liveLow, label: "电量低", pct: 10}); w != 296 || h != 34 {
		t.Fatalf("island low-battery shape = %.0fx%.0f, want 296x34", w, h)
	}
	done := liveEvent{kind: liveDownloadDone, label: "设计稿-v3.zip"}
	a.pushLive(done, time.Second, time.Now())
	if w, h := a.liveShape(done); h != islandCapsuleH || w < 280 || w > fbMaxW {
		t.Fatalf("island download-done shape = %.0fx%.0f, want a 36pt capsule", w, h)
	}
	if w, h := a.liveShape(liveEvent{kind: liveHeadset, label: "AirPods Pro", levels: []string{"左 80%", "右 78%", "盒 50%"}}); h != 36 || w < 330 {
		t.Fatalf("island headphone shape = %.0fx%.0f, want a >=330pt 36pt capsule", w, h)
	}

	n := islandShelf(true, settings.ShellStyleNotch)
	if n.liveWingW() != liveWingW {
		t.Fatalf("notch wing = %d, want %d", n.liveWingW(), liveWingW)
	}
	if w, h := n.liveShape(liveEvent{kind: liveCharging, label: "正在充电", pct: 80}); w != 180+2*liveWingW || h != 36 {
		t.Fatalf("notch charging shape = %.0fx%.0f, want %dx36", w, h, 180+2*liveWingW)
	}
	if _, h := n.liveShape(liveEvent{kind: liveHeadset, label: "AirPods Pro"}); h != 32+fbBody {
		t.Fatalf("notch headphone capsule %.0f high, want %d", h, 32+fbBody)
	}
	if w, h := n.downloadShape(); w != 180+2*liveWingW || h != 36 {
		t.Fatalf("notch download shape = %.0fx%.0f", w, h)
	}
	if w, h := a.downloadShape(); w != 296 || h != 34 {
		t.Fatalf("island download shape = %.0fx%.0f, want 296x34", w, h)
	}
}

// A finished-download capsule is a button on the island too: the shell's
// own open-on-press must not swallow the press before the view can add the
// file. Other live capsules still open the shelf.
func TestIslandPressOnFinishedDownloadCapsule(t *testing.T) {
	point := mygo.Point{X: 735, Y: islandTopGap + 18}
	done := islandShelf(false, settings.ShellStyleNotch)
	done.settings.LiveActivities.Downloads = true
	done.live = liveQueue{shown: true, until: time.Now().Add(time.Minute),
		cur: liveEvent{kind: liveDownloadDone, label: "a.zip", path: filepath.Join(t.TempDir(), "a.zip")}}
	done.fx.w, done.fx.h, done.peekProgress = 280, islandCapsuleH, 1
	done.mouseDown(true, 0, point)
	if done.visible {
		t.Fatal("the shell opened instead of leaving the finished-download press to its view")
	}

	phones := islandShelf(false, settings.ShellStyleNotch)
	phones.settings.LiveActivities.Headphones = true
	phones.live = liveQueue{shown: true, until: time.Now().Add(time.Minute),
		cur: liveEvent{kind: liveHeadset, label: "AirPods Pro"}}
	phones.fx.w, phones.fx.h, phones.peekProgress = 330, islandCapsuleH, 1
	phones.mouseDown(true, 0, point)
	if !phones.visible {
		t.Fatal("a headphone notice on the island no longer opens the shelf")
	}
}

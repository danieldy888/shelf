//go:build darwin

package main

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/settings"
)

func dragShelf() *shelf {
	return &shelf{
		notchX: 600, notchY: 0, notchW: 180, notchH: 32,
		x: 410, y: 0, h: 560,
		dragging:     true,
		settings:     settings.Default(),
		thumbnails:   map[string]*ui.Bitmap{},
		thumbPending: map[string]bool{},
	}
}

// The incoming-drag frame must never exceed the menu bar's height while
// nothing taller is drawn: the widened hint stays band-high, and the
// player/capsule's tall frames are not retained through a drag.
func TestIncomingDragFrameStaysInMenuBand(t *testing.T) {
	a := dragShelf()
	a.settings.IslandNowPlaying = true
	a.np = &nowPlayingModule{host: a, have: true, last: mac.NowPlayingState{Title: "Track"}}
	f := a.desiredFrame()
	bandW := max(180, a.notchW) + 2*dropBandPadX
	hintW := int(180+dropHintExtraW) + 2*earR // band may widen ±ears to cover the hint's corners
	if f.Height > max(32, a.notchH) {
		t.Fatalf("incoming-drag frame %dpt tall left the menu bar", f.Height)
	}
	if f.Width != max(bandW, hintW) {
		t.Fatalf("incoming-drag frame %dpt wide, want %d (band or hint+ears)", f.Width, max(bandW, hintW))
	}
	if f.X != a.stripCenter()-f.Width/2 || f.Y != a.y {
		t.Fatalf("incoming-drag frame %+v lost the notch anchor", f)
	}
	// Outside the band the drawn hint is also band-high: frame and shell
	// agree that nothing reaches below the menu bar.
	if w, h := a.peekSize(); h > float32(max(32, a.notchH)) {
		t.Fatalf("drag hint %.0fx%.0f exceeds the menu bar", w, h)
	}
}

// Entering the band aims the frame at the drop pill; mid-morph it still
// covers the drawn shell plus ears, and a fold remnant taller than the
// pill keeps its own room.
func TestDropBandFrameFollowsTheDrawnPill(t *testing.T) {
	a := dragShelf()
	a.dropHot = true
	f := a.desiredFrame()
	pillW := 180 + dropPillExtraW
	if f.Height != 32+dropPillBody || f.Width != pillW+2*earR {
		t.Fatalf("in-band frame %+v must cover the %dx%d pill plus ears", f, pillW, 32+dropPillBody)
	}
	// The pill mid-grow: the frame tracks the drawn shell plus ears.
	a.fx.peek, a.fx.w, a.fx.h = true, 240, 48
	f = a.desiredFrame()
	if f.Height != 64 || f.Width != max(pillW, 240)+2*earR {
		t.Fatalf("frame %+v does not follow the drawn pill", f)
	}
	// A fold remnant taller than the pill keeps its own room.
	a.fx.w, a.fx.h = 300, 90
	f = a.desiredFrame()
	if f.Height != 91 || f.Width != 300+2*earR {
		t.Fatalf("frame %+v clipped the fold remnant 300x90", f)
	}
}

// Shelf's own drag-out keeps the resident strip — no band, no hint.
func TestOwnDragKeepsTheNormalStrip(t *testing.T) {
	a := dragShelf()
	a.ownDrag = true
	f := a.desiredFrame()
	bandW := max(180, a.notchW) + 2*dropBandPadX
	if f.Width == bandW && f.Height == max(32, a.notchH) {
		t.Fatalf("own drag-out shrank to the drop band %+v", f)
	}
	if w, h := a.peekSize(); w == 180+dropPillExtraW || h == 32+dropPillBody {
		t.Fatalf("own drag-out drew the drop pill %.0fx%.0f", w, h)
	}
}

// Shake-to-summon arms only for drags carrying files or image data.
func TestDragSummonsBasketOnlyForFilesAndImages(t *testing.T) {
	for _, types := range [][]string{
		{"public.file-url"},
		{"NSFilenamesPboardType", "com.apple.finder.node"},
		{"com.apple.pasteboard.promised-file-url"},
		{"com.apple.NSFilePromiseItemMetaData"},
		{"public.png"},
		{"public.tiff", "public.utf8-plain-text"},
		{"public.heic"},
		{"public.file-promise"},
	} {
		if !dragSummonsBasket(types) {
			t.Fatalf("%v must summon the basket", types)
		}
	}
	for _, types := range [][]string{
		nil,
		{"public.utf8-plain-text"},
		{"public.utf8-plain-text", "NSStringPboardType"}, // Tern tab drag
		{"public.url", "public.utf8-plain-text"},         // a dragged link
		{"public.data"},
	} {
		if dragSummonsBasket(types) {
			t.Fatalf("%v must not summon the basket", types)
		}
	}
}

// The mouse-down observer and the peek shape both see the same drag band:
// outside it a drag press belongs to the app underneath, inside it the
// band accepts.
func TestIncomingDragAcceptsOnlyInsideTheBand(t *testing.T) {
	a := dragShelf()
	bandW := max(180, a.notchW) + 2*dropBandPadX
	f := a.desiredFrame()
	hintW := int(180+dropHintExtraW) + 2*earR
	if f.Width != max(bandW, hintW) {
		t.Fatalf("drag frame %d wide, want %d (band or hint+ears)", f.Width, max(bandW, hintW))
	}
	// Points below the menu bar at the notch's own X must be outside both
	// the frame and dropBand — nothing between them may answer a drop.
	below := mygo.Point{X: a.notchX + a.notchW/2, Y: a.notchY + max(32, a.notchH) + 1}
	if a.dropBand(below) {
		t.Fatalf("point %+v below the menu bar hit the drop band", below)
	}
	if below.Y < f.Y+f.Height && below.X >= f.X && below.X < f.X+f.Width {
		t.Fatalf("point %+v below the menu bar is inside the drag frame %+v", below, f)
	}
}

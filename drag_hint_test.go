//go:build darwin

package main

// Tests for the drop hint during Shelf's own drag-out: the panel toast and
// the notch's 「松开放入」 pill must only answer incoming system drags.

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"shelf/internal/settings"
)

func TestDropHintHiddenDuringOwnDrag(t *testing.T) {
	a := &shelf{
		settings: settings.Default(), visible: true,
		dragging: true, ownDrag: true, notchW: 180, notchH: 32,
	}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if _, ok := view.Find("松开放入"); ok {
		t.Fatal("the panel showed the drop hint during Shelf's own drag-out")
	}
	a.ownDrag = false
	view = ui.NewTester(a.view, panelWidth, panelHeight)
	if _, ok := view.Find("松开放入"); !ok {
		t.Fatal("an incoming drag lost the panel's drop hint")
	}
}

func TestPeekDropHintHiddenDuringOwnDrag(t *testing.T) {
	a := &shelf{
		settings: settings.Default(), notchW: 180, notchH: 32,
		peeking: true, peekProgress: 1,
		dragging: true, ownDrag: true,
	}
	if w, h := a.peekSize(); w == 180+dropPillExtraW || h == 32+dropPillBody {
		t.Fatalf("own drag-out kept the drop pill shape %.0fx%.0f", w, h)
	}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if _, ok := view.Find("松开放入"); ok {
		t.Fatal("the notch showed 松开放入 during Shelf's own drag-out")
	}
	a.ownDrag = false
	// An incoming drag outside the notch band only widens the shell at the
	// notch's own height: the hint, never the pill or a glyph.
	if w, h := a.peekSize(); w != 180+dropHintExtraW || h != 32 {
		t.Fatalf("an incoming drag outside the band got peek %.0fx%.0f, want the band hint", w, h)
	}
	view = ui.NewTester(a.view, panelWidth, panelHeight)
	if _, ok := view.Find("松开放入"); ok {
		t.Fatal("the drag hint outside the band showed the release words")
	}
	// Entering the band grows the real pill.
	a.dropHot = true
	if w, h := a.peekSize(); w != 180+dropPillExtraW || h != 32+dropPillBody {
		t.Fatalf("a drag inside the band got peek %.0fx%.0f, want the drop pill", w, h)
	}
	view = ui.NewTester(a.view, panelWidth, panelHeight)
	if _, ok := view.Find("松开放入"); !ok {
		t.Fatal("an incoming drag inside the band lost 松开放入")
	}
}

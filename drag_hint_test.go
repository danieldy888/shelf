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
	if w, h := a.peekSize(); w != 180+dropPillExtraW || h != 32+dropPillBody {
		t.Fatalf("an incoming drag got peek %.0fx%.0f, want the drop pill", w, h)
	}
	view = ui.NewTester(a.view, panelWidth, panelHeight)
	if _, ok := view.Find("松开放入"); !ok {
		t.Fatal("an incoming drag lost the notch's 松开放入 hint")
	}
}

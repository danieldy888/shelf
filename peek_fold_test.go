//go:build darwin

package main

// The fold after a cancelled drag is subtract-only: the shell keeps the
// shape and content it was showing and just shrinks back into the notch.
// What it must never do is re-aim at the live peekSize()/peekView answer —
// a loaded track would morph the folding shell toward the player and flash
// its title mid-fold (the user's frame-235 glitch).

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// dragPeekShelf builds the pre-fold state: an incoming system drag has the
// drop pill up on the notch while a track is loaded.
func dragPeekShelf() *shelf {
	a := motionWindow()
	fakeTrack(a, true)
	a.settings.IslandNowPlaying = true
	a.dragging = true
	a.peeking = true
	a.peekProgress, a.peekFrom = 1, 1
	return a
}

// foldFrames steps the view while the peek folds and checks the shell only
// ever shrinks and keeps the drop pill's words.
func foldFrames(t *testing.T, a *shelf, view *ui.Tester, keepText string) {
	t.Helper()
	pw, ph := a.fx.w, a.fx.h
	for i := range 30 {
		view.Frame()
		if a.fx.w > pw+0.5 || a.fx.h > ph+0.5 {
			t.Fatalf("frame %d: folding shell grew to %.1fx%.1f (was %.1fx%.1f)", i, a.fx.w, a.fx.h, pw, ph)
		}
		pw, ph = a.fx.w, a.fx.h
		if _, ok := view.Find("Fashion Killa"); ok {
			t.Fatalf("frame %d: the player title appeared mid-fold", i)
		}
		if keepText != "" && a.fx.peekK > 0.02 {
			if _, ok := view.Find(keepText); !ok {
				t.Fatalf("frame %d: %q vanished while the shell is still up (peekK %.2f)", i, keepText, a.fx.peekK)
			}
		}
		view.Advance(16 * time.Millisecond)
	}
}

// TestPeekFoldKeepsShownShape: a cancelled drag folds the drop pill straight
// back — with a track loaded, the shell may only shrink and the player's
// title must not appear.
func TestPeekFoldKeepsShownShape(t *testing.T) {
	a := dragPeekShelf()
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame()
	if _, ok := view.Find("松开放入"); !ok {
		t.Fatal("the drop pill is not up before the fold")
	}
	// Drag ends, pointer not over the notch: the fold begins.
	a.dragging = false
	a.peeking = false
	foldFrames(t, a, view, "松开放入")
}

// TestPeekFoldWithoutTrackAlsoPlain: the same fold with no track loaded
// keeps the drop pill's shape and words.
func TestPeekFoldWithoutTrackAlsoPlain(t *testing.T) {
	a := dragPeekShelf()
	a.np = nil
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame()
	a.dragging = false
	a.peeking = false
	foldFrames(t, a, view, "松开放入")
}

// TestPeekFoldHoverStillGetsThePlayer: if the pointer rests on the notch
// when the drag ends, the pill may morph into the hover player — the fold
// freeze only applies while the peek is going away.
func TestPeekFoldHoverStillGetsThePlayer(t *testing.T) {
	a := dragPeekShelf()
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame()
	// Drag ends while hovering the notch: peeking stays true.
	a.dragging = false
	var grew bool
	for range 40 {
		view.Frame()
		if a.fx.w > float32(180+dropPillExtraW) {
			grew = true
		}
		view.Advance(16 * time.Millisecond)
	}
	if !grew {
		t.Fatal("a hover after the drag never morphed toward the player size")
	}
	if _, ok := view.Find("Fashion Killa"); !ok {
		t.Fatal("the hover player never appeared over the notch")
	}
}

// TestPeekFoldInterruptedByNewDrag: a drag that starts while the pill is
// folding must grow it back out as the drop pill — no empty shell, no
// player flash.
func TestPeekFoldInterruptedByNewDrag(t *testing.T) {
	a := dragPeekShelf()
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	view.Frame()
	a.dragging = false
	a.peeking = false
	for range 8 { // mid-fold
		view.Frame()
		view.Advance(16 * time.Millisecond)
	}
	mid := a.fx.w
	a.dragging = true
	a.peeking = true
	var back bool
	for i := range 30 {
		view.Frame()
		if _, ok := view.Find("Fashion Killa"); ok {
			t.Fatalf("frame %d: the player title appeared while a drag re-raised the pill", i)
		}
		if a.fx.w > mid+40 {
			back = true
		}
		view.Advance(16 * time.Millisecond)
	}
	if !back {
		t.Fatal("the re-raised drag never grew the shell back")
	}
	if _, ok := view.Find("松开放入"); !ok {
		t.Fatal("the re-raised drag lost the drop pill's words")
	}
}

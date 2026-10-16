//go:build darwin

package mac

import "testing"

func TestOverlayHitsNotchBandWhenIdle(t *testing.T) {
	// Idle: only the drawn shell — a bandW × bandH region centered in the
	// strip — is solid, so a file at Y=0 hits a window while clicks on the
	// rest of the strip pass through to the menu bar below.
	if !overlayHits(false, 180, 32, 140, 0, 280, 72) {
		t.Fatal("top of the idle strip (the Spaces edge) must hit the overlay")
	}
	if !overlayHits(false, 180, 32, 140, 31.9, 280, 72) {
		t.Fatal("last pixel of the notch band must hit the overlay")
	}
	if overlayHits(false, 180, 32, 140, 32, 280, 72) {
		t.Fatal("idle pixels below the notch must pass through")
	}
	if overlayHits(false, 180, 32, 140, 60, 280, 72) {
		t.Fatal("peek-pill region must not steal clicks while idle")
	}
	if overlayHits(false, 180, 32, 20, 8, 280, 72) {
		t.Fatal("pixels outside the idle band must pass through to the menu bar")
	}
	if overlayHits(false, 180, 32, 260, 8, 280, 72) {
		t.Fatal("pixels right of the idle band must pass through to the menu bar")
	}
	if !overlayHits(false, 180, 32, 50, 8, 280, 72) {
		t.Fatal("the band's left edge must hit the overlay")
	}
	if overlayHits(false, 180, 32, 49.9, 8, 280, 72) {
		t.Fatal("just left of the band must pass through")
	}
	if !overlayHits(false, 180, 32, 229.9, 8, 280, 72) {
		t.Fatal("the band's right edge must hit the overlay")
	}
	if overlayHits(false, 180, 32, 230, 8, 280, 72) {
		t.Fatal("just right of the band must pass through")
	}
	// The island widens the band to the drawn wings; clicks on them land.
	if !overlayHits(false, 276, 32, 20, 8, 280, 72) {
		t.Fatal("a click on a drawn island wing must hit the overlay")
	}
}

func TestOverlayHitsWholeStripWhenAccepting(t *testing.T) {
	if !overlayHits(true, 180, 32, 140, 60, 280, 72) {
		t.Fatal("peeking/drag must accept the whole overlay, including the hanging pill")
	}
	if overlayHits(false, 180, 0, 140, 0, 280, 72) {
		t.Fatal("a zero band with no acceptAll must not swallow the strip")
	}
	// bandW <= 0 keeps the whole strip width solid: the pre-island rule.
	if !overlayHits(false, 0, 32, 20, 8, 280, 72) {
		t.Fatal("a zero band width must fall back to the full strip")
	}
}

func TestSetOverlayHitTestNilHandle(t *testing.T) {
	SetOverlayHitTest(0, true, 180, 32)
	SetOverlayBand(180, 32)
	SetDragElevated(0, true)
}

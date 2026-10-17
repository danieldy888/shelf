//go:build darwin

package mac

import "testing"

func TestOverlayHitsNotchBandWhenIdle(t *testing.T) {
	// Idle: only the centered hardware-notch band is solid, so a file at
	// Y=0 hits a window while menu-bar items beside the notch keep clicks.
	if !overlayHits(false, 180, 32, 90, 0, 360, 72) {
		t.Fatal("top of the idle strip (the Spaces edge) must hit the overlay")
	}
	if !overlayHits(false, 180, 32, 90, 31.9, 360, 72) {
		t.Fatal("last pixel of the notch band must hit the overlay")
	}
	if overlayHits(false, 180, 32, 90, 32, 360, 72) {
		t.Fatal("idle pixels below the notch must pass through")
	}
	if overlayHits(false, 180, 32, 90, 60, 360, 72) {
		t.Fatal("peek-pill region must not steal clicks while idle")
	}
	// Beside the centered band: the strip is wider than the notch (it must
	// fit the +120pt music pill) and its flanks pass to the menu bar.
	if overlayHits(false, 180, 32, 89.9, 10, 360, 72) {
		t.Fatal("left of the notch band must pass through to the menu bar")
	}
	if overlayHits(false, 180, 32, 270.1, 10, 360, 72) {
		t.Fatal("right of the notch band must pass through to the menu bar")
	}
	if !overlayHits(false, 180, 32, 179, 10, 360, 72) {
		t.Fatal("the band's right edge pixel still hits")
	}
}

func TestOverlayHitsWholeStripWhenAccepting(t *testing.T) {
	if !overlayHits(true, 180, 32, 300, 60, 360, 72) {
		t.Fatal("peeking/drag must accept the whole overlay, including the hanging pill")
	}
	if overlayHits(false, 0, 0, 180, 0, 360, 72) {
		t.Fatal("a zero band with no acceptAll must not swallow the strip")
	}
	// bandW <= 0 means the full strip width (notch width unknown).
	if !overlayHits(false, 0, 32, 10, 10, 360, 72) {
		t.Fatal("an unknown notch width must fall back to the full strip")
	}
}

func TestSetOverlayHitTestNilHandle(t *testing.T) {
	SetOverlayHitTest(0, true, 180, 32)
	SetDragElevated(0, true)
}

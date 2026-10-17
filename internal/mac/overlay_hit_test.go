//go:build darwin

package mac

import "testing"

func TestOverlayHitsNotchBandWhenIdle(t *testing.T) {
	// Idle: only the hardware-notch band is solid, so a file at Y=0 hits a
	// window while clicks on the rest of the strip pass through.
	if !overlayHits(false, 32, 0, 72) {
		t.Fatal("top of the idle strip (the Spaces edge) must hit the overlay")
	}
	if !overlayHits(false, 32, 31.9, 72) {
		t.Fatal("last pixel of the notch band must hit the overlay")
	}
	if overlayHits(false, 32, 32, 72) {
		t.Fatal("idle pixels below the notch must pass through")
	}
	if overlayHits(false, 32, 60, 72) {
		t.Fatal("peek-pill region must not steal clicks while idle")
	}
}

func TestOverlayHitsWholeStripWhenAccepting(t *testing.T) {
	if !overlayHits(true, 32, 60, 72) {
		t.Fatal("peeking/drag must accept the whole overlay, including the hanging pill")
	}
	if overlayHits(false, 0, 0, 72) {
		t.Fatal("a zero band with no acceptAll must not swallow the strip")
	}
}

func TestSetOverlayHitTestNilHandle(t *testing.T) {
	SetOverlayHitTest(0, true, 32)
	SetDragElevated(0, true)
}

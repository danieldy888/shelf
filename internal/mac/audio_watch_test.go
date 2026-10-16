//go:build darwin

package mac

import "testing"

// watchTransition(watched, next) is the pure core behind the re-arm logic:
// it decides which device's per-device listeners come down and which go up.
// The system default-device listener is never part of a transition.

func TestWatchTransitionIdleSwap(t *testing.T) {
	// Device arrives on a machine that had none: attach only.
	detach, attach := watchTransition(0, 82)
	if detach != 0 || attach != 82 {
		t.Fatalf("0->82 = detach %d attach %d, want 0/82", detach, attach)
	}
}

func TestWatchTransitionDeviceGone(t *testing.T) {
	// The watched device disappears: detach it, attach nothing — the
	// system listener stays armed for the next device.
	detach, attach := watchTransition(82, 0)
	if detach != 82 || attach != 0 {
		t.Fatalf("82->0 = detach %d attach %d, want 82/0", detach, attach)
	}
}

func TestWatchTransitionDeviceSwap(t *testing.T) {
	detach, attach := watchTransition(82, 91)
	if detach != 82 || attach != 91 {
		t.Fatalf("82->91 = detach %d attach %d, want 82/91", detach, attach)
	}
}

func TestWatchTransitionSameDeviceNoop(t *testing.T) {
	for _, pair := range [][2]uint32{{0, 0}, {82, 82}} {
		if detach, attach := watchTransition(pair[0], pair[1]); detach != 0 || attach != 0 {
			t.Fatalf("%d->%d must be a no-op, got %d/%d", pair[0], pair[1], detach, attach)
		}
	}
}

// A sequence of transitions must never leave the watch on a stale device
// and must never drop the system listener's arm.
func TestWatchTransitionSequence(t *testing.T) {
	var watched uint32
	for _, next := range []uint32{0, 82, 82, 0, 0, 91, 82} {
		detach, attach := watchTransition(watched, next)
		if detach != 0 && detach != watched {
			t.Fatalf("detaching %d while watching %d", detach, watched)
		}
		// Mirror watchOutputDevice: detach clears, attach installs.
		if detach != 0 {
			watched = 0
		}
		if attach != 0 {
			watched = attach
		}
		if watched != next {
			t.Fatalf("after %d the watched device must be %d, got %d", next, next, watched)
		}
	}
	if watched != 82 {
		t.Fatalf("final watched = %d, want 82", watched)
	}
}

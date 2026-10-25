//go:build darwin

package mac

import (
	"math"
	"testing"
)

// These tests never change the system volume: they only read it.
// SetOutputVolume is deliberately not called here — muting the user's
// machine as a test side effect is not acceptable.

func TestClampVolume(t *testing.T) {
	cases := []struct {
		name string
		in   float32
		want float32
	}{
		{"zero", 0, 0},
		{"one", 1, 1},
		{"middle", 0.5, 0.5},
		{"just below zero", -0.01, 0},
		{"negative", -3, 0},
		{"above one", 2.5, 1},
		{"nan becomes zero", float32(math.NaN()), 0},
	}
	for _, tc := range cases {
		if got := clampVolume(tc.in); got != tc.want {
			t.Errorf("%s: clampVolume(%v) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestOutputVolumeReadOnly checks the live read path against the real
// system. It skips when CoreAudio or the default device offers no
// readable volume (some HDMI and aggregate devices), and it is
// read-only by construction.
func TestOutputVolumeReadOnly(t *testing.T) {
	v, ok := OutputVolume()
	if !ok {
		t.Skip("no readable output volume on this system")
	}
	if v < 0 || v > 1 || math.IsNaN(float64(v)) {
		t.Fatalf("OutputVolume = %v, want a scalar in 0..1", v)
	}
	if !VolumeSupported() {
		t.Fatal("VolumeSupported says false, but OutputVolume read a value")
	}
}

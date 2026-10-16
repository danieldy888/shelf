//go:build darwin

package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/settings"
)

func TestVolumeTrackerPrimesWithoutFlash(t *testing.T) {
	var tr volumeTracker
	now := time.Now()
	tr.step(now, 0.5, false, true)
	if tr.live(now) || tr.pending {
		t.Fatal("first sample must only baseline; startup shows nothing")
	}
}

func TestVolumeTrackerChangeDebounce(t *testing.T) {
	var tr volumeTracker
	start := time.Now()
	tr.step(start, 0.5, false, true)

	// A change raises a pending event that settles after the debounce.
	tr.step(start.Add(10*time.Millisecond), 0.6, false, true)
	if !tr.pending || tr.live(start.Add(10*time.Millisecond)) {
		t.Fatal("a change must pend, not flash immediately")
	}
	tr.step(start.Add(200*time.Millisecond), 0.6, false, true)
	if !tr.live(start.Add(200 * time.Millisecond)) {
		t.Fatal("a quiet sample past the debounce must fire the flash")
	}
	if tr.live(start.Add(200*time.Millisecond + volumeFlash)) {
		t.Fatal("the flash must end at its Until")
	}
}

func TestVolumeTrackerBurstFolds(t *testing.T) {
	var tr volumeTracker
	start := time.Now()
	tr.step(start, 0.5, false, true)

	// A held volume key bursts changes faster than the debounce: all fold
	// into one flash.
	for i := 1; i <= 6; i++ {
		tr.step(start.Add(time.Duration(i*40)*time.Millisecond), 0.5+float32(i)*0.05, false, true)
	}
	if tr.live(start.Add(240 * time.Millisecond)) {
		t.Fatal("still bursting: nothing may flash yet")
	}
	// Quiet resumes at burst-start + debounce (40ms steps still counted
	// against the same edgeAt window only while they keep arriving — the
	// last change at 240ms is past 150ms of the first at 40ms, so the
	// flash fires on the next quiet sample).
	tr.idle(start.Add(400 * time.Millisecond))
	if !tr.live(start.Add(400 * time.Millisecond)) {
		t.Fatal("the settled burst must flash once")
	}
	until := tr.until
	tr.step(start.Add(500*time.Millisecond), 0.8, false, true)
	tr.idle(start.Add(700 * time.Millisecond))
	if tr.until.Before(until) {
		t.Fatal("a new burst must extend, never shorten, the window")
	}
}

func TestVolumeTrackerMuteChangeFlashes(t *testing.T) {
	var tr volumeTracker
	start := time.Now()
	tr.step(start, 0.5, false, true)
	tr.step(start.Add(10*time.Millisecond), 0.5, true, true)
	tr.idle(start.Add(300 * time.Millisecond))
	if !tr.live(start.Add(300 * time.Millisecond)) {
		t.Fatal("a mute toggle alone must flash")
	}
}

func TestVolumeTrackerUnavailableDoesNotFlash(t *testing.T) {
	var tr volumeTracker
	now := time.Now()
	tr.step(now, 0, false, false)
	tr.step(now.Add(500*time.Millisecond), 0, false, false)
	if tr.primed || tr.live(now.Add(600*time.Millisecond)) {
		t.Fatal("a missing output device never produces a flash")
	}
}

func TestVolumeActivityGating(t *testing.T) {
	a := &shelf{settings: settings.Default()}
	a.settings.IslandVolume = true
	m := &volumeModule{host: a}
	now := time.Now()

	if _, ok := m.activity(now); ok {
		t.Fatal("idle tracker must not be live")
	}
	m.tracker = volumeTracker{primed: true, vol: 0.4, until: now.Add(volumeFlash)}
	act, ok := m.activity(now)
	if !ok || act.Priority != volumePriority || act.ID != "volume" {
		t.Fatalf("activity = %+v ok=%v", act, ok)
	}
	if act.Level != 0.4 {
		t.Fatalf("level = %v, want 0.4", act.Level)
	}
	if !act.Until.After(now) {
		t.Fatal("activity must carry the live window")
	}

	a.settings.IslandVolume = false
	if _, ok = m.activity(now); ok {
		t.Fatal("islandVolume=off must silence the module")
	}
}

func TestVolumeIcons(t *testing.T) {
	if speakerIcon(false) == nil || speakerIcon(true) == nil {
		t.Fatal("speaker icons must parse")
	}
	if speakerIcon(false) == speakerIcon(true) {
		t.Fatal("muted and unmuted must be distinct glyphs")
	}
}

func TestVolumeIconPointerStable(t *testing.T) {
	// The island invalidates when Leading changes pointer, so the module
	// must hand out the same cached glyph every tick of a flash.
	a := &shelf{settings: settings.Default()}
	a.settings.IslandVolume = true
	m := &volumeModule{host: a, icons: [2]*ui.SVG{speakerIcon(false), speakerIcon(true)}}
	now := time.Now()
	m.tracker = volumeTracker{primed: true, vol: 0.4, until: now.Add(volumeFlash)}

	first, ok1 := m.activity(now)
	second, ok2 := m.activity(now.Add(500 * time.Millisecond))
	third, ok3 := m.activity(now.Add(volumeFlash - time.Millisecond))
	if !ok1 || !ok2 || !ok3 {
		t.Fatal("all three activity calls inside the window must be live")
	}
	if first.Leading != second.Leading || second.Leading != third.Leading {
		t.Fatal("Leading pointer must be identical across ticks")
	}

	// The muted variant is a different cached pointer, also stable.
	m.tracker.muted = true
	muted1, _ := m.activity(now)
	muted2, _ := m.activity(now.Add(100 * time.Millisecond))
	if muted1.Leading != muted2.Leading {
		t.Fatal("muted Leading must also be cached")
	}
	if muted1.Leading == first.Leading {
		t.Fatal("muted and normal glyphs must be different images")
	}
}

func TestVolumeTrackerEpsilon(t *testing.T) {
	var tr volumeTracker
	start := time.Now()
	tr.step(start, 0.5, false, true)
	// Sub-epsilon jitter is not a user-visible change: it never pends.
	tr.step(start.Add(10*time.Millisecond), 0.5+levelEps/2, false, true)
	if tr.pending || tr.live(start.Add(300*time.Millisecond)) {
		t.Fatal("jitter below levelEps must not flash")
	}
	tr.step(start.Add(20*time.Millisecond), 0.5+levelEps*2, false, true)
	if !tr.pending {
		t.Fatal("a change above levelEps must pend")
	}
}

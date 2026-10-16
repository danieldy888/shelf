//go:build darwin

package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
)

// Volume changes flash higher than battery events but below nothing else
// the island owns; the flash itself mirrors the system HUD's lifetime.
const (
	volumePriority = 30
	volumeFlash    = 1500 * time.Millisecond
	// volumeDebounce lets one "change" span a burst of key repeats or a
	// dragged slider: events closer than this fold into one flash.
	volumeDebounce = 150 * time.Millisecond
	// volumeResample is the safety net alongside the CoreAudio listeners;
	// volumePoll is the fallback when listeners could not be installed.
	volumeResample = 10 * time.Second
	volumePoll     = 500 * time.Millisecond
	// levelEps is the smallest volume-scalar delta worth reporting: below
	// a quarter of a media-key step it is noise.
	levelEps = 0.001
)

// volumeTracker is the pure state machine behind volumeModule: the first
// valid sample primes it (startup never flashes), later differences raise
// a pending event that settles into a flash after volumeDebounce of quiet.
// Feeding the tracker an identical sample (it can be stepped with the
// previous values) advances the debounce without restarting the window.
type volumeTracker struct {
	primed  bool
	vol     float32
	muted   bool
	pending bool
	edgeAt  time.Time // when the current burst of changes began
	until   time.Time // flash window currently live
}

// step folds one observation into the tracker.
func (t *volumeTracker) step(now time.Time, vol float32, muted bool, ok bool) {
	if !ok {
		return
	}
	if !t.primed {
		t.primed, t.vol, t.muted = true, vol, muted
		return
	}
	if volWithin(t.vol, vol) {
		// A sub-epsilon drift keeps the shown value: it must not reset the
		// debounce or count as a new change.
		vol = t.vol
	}
	if vol == t.vol && muted == t.muted {
		t.flush(now)
		return
	}
	t.vol, t.muted = vol, muted
	if !t.pending || now.Sub(t.edgeAt) > volumeDebounce {
		// New burst (or the first): the debounce window starts here.
		t.edgeAt = now
	}
	t.pending = true
	if now.Sub(t.edgeAt) >= volumeDebounce {
		t.emit(now)
	}
}

// volWithin reports whether the new scalar is close enough to the shown
// one to be the same user-visible level.
func volWithin(shown, next float32) bool {
	d := shown - next
	if d < 0 {
		d = -d
	}
	return d < levelEps
}

// idle advances an overdue debounce when no fresh sample is available.
func (t *volumeTracker) idle(now time.Time) { t.emit(now) }

// flush turns a settled pending event into a live flash.
func (t *volumeTracker) flush(now time.Time) {
	if !t.pending || now.Sub(t.edgeAt) < volumeDebounce {
		return
	}
	t.emit(now)
}

func (t *volumeTracker) emit(now time.Time) {
	if !t.pending {
		return
	}
	t.pending = false
	t.until = now.Add(volumeFlash)
}

// live reports whether a flash is up at now.
func (t *volumeTracker) live(now time.Time) bool { return !t.until.IsZero() && now.Before(t.until) }

// volumeModule watches the default output device's volume and mute and
// flashes the island when either changes — the same changes the system
// HUD shows. It never touches keys or writes properties, so it cannot
// suppress the HUD. Sampling happens only on CoreAudio listener signals
// plus a slow resample.
type volumeModule struct {
	host    *shelf
	tracker volumeTracker
	last    time.Time
	period  time.Duration
	icons   [2]*ui.SVG
}

func newVolumeModule(host *shelf) *volumeModule {
	m := &volumeModule{host: host, period: volumePoll, icons: [2]*ui.SVG{speakerIcon(false), speakerIcon(true)}}
	if mac.WatchOutput() {
		m.period = volumeResample
	}
	if s := mac.ReadOutput(); s.OK {
		m.tracker.step(time.Now(), s.Level, s.Muted, true)
	}
	return m
}

func (m *volumeModule) tick(now time.Time) {
	dirty := mac.ConsumeOutputChanged()
	if !dirty && now.Sub(m.last) < m.period {
		m.tracker.idle(now)
		return
	}
	m.last = now
	// A default-device swap that produced no listener event (a HAL restart
	// or a silently removed device) is recovered here: AudioResync re-arms
	// and is a no-op while the watched device is still right.
	mac.AudioResync()
	s := mac.ReadOutput()
	m.tracker.step(now, s.Level, s.Muted, s.OK)
}

// activity answers on every tick while the flash window is live.
func (m *volumeModule) activity(now time.Time) (activity, bool) {
	if !m.host.settings.IslandVolume || !m.tracker.live(now) {
		return activity{}, false
	}
	act := activity{
		ID:       "volume",
		Priority: volumePriority,
		Leading:  m.icon(),
		Level:    m.tracker.vol,
		Until:    m.tracker.until,
	}
	if m.tracker.muted {
		act.LeadingTint = ui.Hex("#FF9F0A")
	}
	return act, true
}

// icon returns the cached speaker glyph so the island's Leading pointer is
// stable across ticks and never re-parses.
func (m *volumeModule) icon() *ui.SVG {
	if m.tracker.muted {
		return m.icons[1]
	}
	return m.icons[0]
}

// speakerIcon draws the glyph once per variant at construction; the muted
// variant drops the sound waves for a cross.
func speakerIcon(muted bool) *ui.SVG {
	const body = `<path fill="currentColor" d="M4 9.2v5.6h3.6l4.4 3.7V5.5L7.6 9.2H4z"/>`
	if muted {
		return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` + body +
			`<path fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" d="m15.8 9.8 4.6 4.6m0-4.6-4.6 4.6"/></svg>`))
	}
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` + body +
		`<path fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" d="M15.2 9.2a3.9 3.9 0 0 1 0 5.6M17.4 7a7.1 7.1 0 0 1 0 10"/></svg>`))
}

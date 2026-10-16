//go:build darwin

package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
)

// Battery island priorities: the plug/unplug flash outranks the persistent
// low-battery warning, and both stay below a volume change.
const (
	batteryPriority    = 20
	lowBatteryPriority = 5

	// batteryFlash is how long a plug/unplug transient stays up.
	batteryFlash = 3 * time.Second
	// batteryResample watches for events the run-loop source missed (sleep
	// wake, core power bookkeeping) even when notifications are installed.
	batteryResample = 10 * time.Second
	// batteryPoll is the fallback interval when IOPSNotification could not
	// be installed; still far below the 20 s the activity spec allows.
	batteryPoll = 2 * time.Second

	// The low-battery band has hysteresis: it opens at lowEnterPercent and
	// closes at lowExitPercent so a charge wobbling across the threshold
	// does not flash the warning on and off.
	lowEnterPercent = 10
	lowExitPercent  = 12
)

// powerSample is one battery observation, decoupled from the IOKit read so
// the tracker stays pure and testable.
type powerSample struct {
	hasBattery bool
	charging   bool // any source other than the battery itself
	percent    int  // 0–100
}

// powerTracker is the pure state machine behind batteryModule: it folds
// samples into plug/unplug edges plus a hysteretic low-battery flag. The
// first sample only establishes a baseline so launching the app never
// produces a transient.
type powerTracker struct {
	primed   bool
	charging bool
	low      bool // inside the hysteretic low-battery band
}

// step folds one sample in; it reports true exactly on a charging-state
// edge (the moment a plug/unplug transient should appear). Samples from a
// machine with no battery are ignored entirely.
func (t *powerTracker) step(s powerSample) bool {
	if !s.hasBattery {
		return false
	}
	if !t.primed {
		t.primed, t.charging = true, s.charging
		t.updateLow(s)
		return false
	}
	edge := s.charging != t.charging
	t.charging = s.charging
	t.updateLow(s)
	return edge
}

// updateLow keeps the low-battery flag hysteretic: it turns on only while
// discharging at or below lowEnterPercent and off again at lowExitPercent —
// or immediately once an external source takes over.
func (t *powerTracker) updateLow(s powerSample) {
	if !s.hasBattery || s.charging {
		t.low = false
		return
	}
	if t.low {
		if s.percent >= lowExitPercent {
			t.low = false
		}
	} else if s.percent <= lowEnterPercent {
		t.low = true
	}
}

// batteryModule reports charger plug/unplug flashes and an ongoing
// low-battery warning to the notch island. It samples only when the IOPS
// run-loop source signals (or on a slow resample), so idle cost is nil.
// Desktops without a battery never get the module: main only constructs it
// when the first read finds one.
type batteryModule struct {
	host    *shelf
	tracker powerTracker
	last    time.Time
	period  time.Duration
	act     activity // current plug/unplug transient
	percent int      // last observed charge, for the trailing text
	fills   [11]*ui.SVG
}

func newBatteryModule(host *shelf) *batteryModule {
	m := &batteryModule{host: host, period: batteryPoll, act: activity{Level: -1}}
	if mac.WatchPowerSources() {
		m.period = batteryResample
	}
	if s := mac.ReadPower(); s.HasBattery {
		m.percent = s.Percent
		m.tracker.step(powerSample{hasBattery: true, charging: s.Charging, percent: s.Percent})
	}
	return m
}

// tick samples on power-source events and a slow resample; the sample
// itself is a couple of CFType reads, cheap at these intervals.
func (m *batteryModule) tick(now time.Time) {
	if !mac.ConsumePowerChanged() && now.Sub(m.last) < m.period {
		return
	}
	m.last = now
	s := mac.ReadPower()
	if !s.HasBattery {
		return
	}
	m.percent = s.Percent
	if m.tracker.step(powerSample{hasBattery: true, charging: s.Charging, percent: s.Percent}) {
		act := activity{
			ID:       "battery",
			Priority: batteryPriority,
			Trailing: fmt.Sprintf("%d%%", s.Percent),
			Level:    -1,
			Until:    now.Add(batteryFlash),
		}
		if s.Charging {
			act.Leading = boltIcon()
			act.LeadingTint = ui.Hex("#30D158")
		} else {
			act.Leading = m.batteryIcon(s.Percent)
		}
		m.act = act
	}
}

// activity answers on every tick: a live plug/unplug transient first, then
// the persistent low-battery warning.
func (m *batteryModule) activity(now time.Time) (activity, bool) {
	if !m.host.settings.IslandBattery {
		return activity{}, false
	}
	if !m.act.Until.IsZero() && now.Before(m.act.Until) {
		return m.act, true
	}
	if m.tracker.low {
		return activity{
			ID:          "battery",
			Priority:    lowBatteryPriority,
			Leading:     m.batteryIcon(m.percent),
			LeadingTint: ui.Hex("#FF453A"),
			Trailing:    fmt.Sprintf("%d%%", m.percent),
			Level:       -1,
		}, true
	}
	return activity{}, false
}

// batteryIcon caches one fill level per decile so repeated ticks reuse the
// same parsed SVG.
func (m *batteryModule) batteryIcon(percent int) *ui.SVG {
	decile := (percent + 5) / 10
	if decile > 10 {
		decile = 10
	}
	if icon := m.fills[decile]; icon != nil {
		return icon
	}
	icon := batteryFillIcon(decile * 10)
	m.fills[decile] = icon
	return icon
}

// boltIcon is a hand-drawn lightning glyph for the plugged-in flash.
func boltIcon() *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="currentColor" d="M13 2 6 13.5h5L9.5 22 17.5 9.5h-5L13 2z"/></svg>`))
}

// batteryFillIcon draws the outline battery glyph filled to percent.
func batteryFillIcon(percent int) *ui.SVG {
	// Inner fill runs 4..18 in x over the 20-wide body interior.
	w := 14.0 * float64(percent) / 100
	fill := ""
	if w >= 0.5 {
		fill = fmt.Sprintf(`<rect x="4" y="9.8" width="%.1f" height="4.4" rx="1" fill="currentColor"/>`, w)
	}
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
		`<rect x="2.2" y="8" width="17.6" height="8" rx="2.2" fill="none" stroke="currentColor" stroke-width="1.6"/>` +
		`<rect x="21.2" y="10.6" width="1.8" height="2.8" rx="0.9" fill="currentColor"/>` + fill + `</svg>`))
}

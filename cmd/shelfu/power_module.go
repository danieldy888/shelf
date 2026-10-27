//go:build darwin

package main

// Charging, full and low-battery notices. IOKit's power-source API is
// polled by the shared tick every two seconds — one dictionary copy, no
// run-loop source to manage — and the transitions are the events: plugging
// in, reaching 100% on the adapter, and each of the low thresholds once per
// discharge. A Mac without a battery never posts anything.

import (
	"time"

	"shelf/internal/mac"
)

const powerPollInterval = 2 * time.Second

type powerModule struct {
	host *shelf
	// probe is the seam: tests feed battery states instead of the machine.
	probe func() (mac.BatteryStatus, bool)
	now   func() time.Time
	next  time.Time

	seeded bool
	last   mac.BatteryStatus
	low20  bool
	low10  bool
	full   bool
}

func newPowerModule(a *shelf) *powerModule {
	return &powerModule{host: a, probe: mac.Battery, now: time.Now}
}

func (m *powerModule) tick(now time.Time) {
	if now.Before(m.next) {
		return
	}
	m.next = now.Add(powerPollInterval)
	if m.probe == nil {
		return
	}
	st, ok := m.probe()
	if !ok {
		m.seeded = false
		return
	}
	if !m.seeded {
		m.adopt(st)
		return
	}
	prev := m.last
	m.last = st
	switch {
	case st.External && !prev.External:
		// Plugged in: a new charge cycle. Right at 100% the words are
		// 已充满 instead of 正在充电.
		m.resetCycle()
		if st.Percent >= 100 {
			m.fullNotice(st, now)
		} else {
			m.chargeNotice(st, now)
		}
	case st.External && st.Percent >= 100 && prev.Percent < 100 && !m.full:
		m.fullNotice(st, now)
	case !st.External:
		if prev.External {
			// A new discharge cycle starts where the battery is now.
			m.low20 = st.Percent <= 20
			m.low10 = st.Percent <= 10
			m.full = false
		}
		if st.Percent <= 20 && !m.low20 {
			m.low20 = true
			m.lowNotice(st, now)
		}
		if st.Percent <= 10 && !m.low10 {
			m.low10 = true
			m.lowNotice(st, now)
		}
	}
}

// adopt records the state the app started on without announcing it: an
// already-full battery or a low battery at launch is not a transition.
func (m *powerModule) adopt(st mac.BatteryStatus) {
	m.seeded = true
	m.last = st
	m.low20 = st.Percent <= 20
	m.low10 = st.Percent <= 10
	m.full = st.External && st.Percent >= 100
}

func (m *powerModule) resetCycle() {
	m.low20, m.low10, m.full = false, false, false
}

func (m *powerModule) chargeNotice(st mac.BatteryStatus, now time.Time) {
	m.host.pushLive(liveEvent{kind: liveCharging, label: "正在充电", pct: st.Percent}, liveHoldCharge, now)
}

func (m *powerModule) fullNotice(st mac.BatteryStatus, now time.Time) {
	m.full = true
	m.host.pushLive(liveEvent{kind: liveCharged, label: "已充满", pct: st.Percent}, liveHoldFull, now)
}

func (m *powerModule) lowNotice(st mac.BatteryStatus, now time.Time) {
	m.host.pushLive(liveEvent{kind: liveLow, label: "电量低", pct: st.Percent}, liveHoldLow, now)
}

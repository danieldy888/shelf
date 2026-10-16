//go:build darwin

package main

import (
	"testing"
	"time"

	"shelf/internal/settings"
)

func batt(charging bool, pct int) powerSample {
	return powerSample{hasBattery: true, charging: charging, percent: pct}
}

func TestPowerTrackerFirstSamplePrimes(t *testing.T) {
	var tr powerTracker
	// Launching the app already on battery must not flash a transient.
	if tr.step(batt(false, 55)) {
		t.Fatal("first sample must only baseline")
	}
	if tr.low {
		t.Fatal("55% must not be low")
	}
}

func TestPowerTrackerPlugEdges(t *testing.T) {
	var tr powerTracker
	tr.step(batt(false, 55))
	if !tr.step(batt(true, 55)) {
		t.Fatal("plugging in must edge")
	}
	if tr.step(batt(true, 60)) {
		t.Fatal("percent changes while plugged are not edges")
	}
	if tr.step(batt(true, 95)) {
		t.Fatal("still plugged, no edge")
	}
	if !tr.step(batt(false, 95)) {
		t.Fatal("unplugging must edge")
	}
	if tr.step(batt(false, 94)) {
		t.Fatal("steady discharging must not edge")
	}
}

func TestPowerTrackerLowHysteresis(t *testing.T) {
	var tr powerTracker
	tr.step(batt(true, 40))
	tr.step(batt(false, 15))
	if tr.low {
		t.Fatal("15% discharging is above the band")
	}
	tr.step(batt(false, 10))
	if !tr.low {
		t.Fatal("10% discharging must open the band")
	}
	tr.step(batt(false, 11))
	if !tr.low {
		t.Fatal("11% inside the band must keep it open")
	}
	tr.step(batt(false, 12))
	if tr.low {
		t.Fatal("12% must close the band")
	}
	tr.step(batt(false, 11))
	if tr.low {
		t.Fatal("11% must not reopen the band")
	}
	tr.step(batt(false, 10))
	if !tr.low {
		t.Fatal("10% again must reopen")
	}
	tr.step(batt(true, 10))
	if tr.low {
		t.Fatal("charging clears the band at once")
	}
}

func TestPowerTrackerNoBatteryNoLow(t *testing.T) {
	var tr powerTracker
	tr.step(powerSample{hasBattery: false})
	if tr.low {
		t.Fatal("a battery-less machine can never be low")
	}
}

func TestBatteryActivityGating(t *testing.T) {
	a := &shelf{settings: settings.Default()}
	a.settings.IslandBattery = true
	m := &batteryModule{host: a}
	now := time.Now()

	// Low battery alone is live at priority 5.
	m.percent = 9
	m.tracker = powerTracker{primed: true, low: true}
	act, ok := m.activity(now)
	if !ok || act.Priority != lowBatteryPriority || act.Trailing != "9%" {
		t.Fatalf("low battery activity = %+v ok=%v", act, ok)
	}
	if act.Level >= 0 {
		t.Fatal("low battery draws no level bar")
	}

	// A live transient wins over the warning.
	m.act = activity{ID: "battery", Priority: batteryPriority, Trailing: "55%", Level: -1, Until: now.Add(batteryFlash)}
	act, ok = m.activity(now)
	if !ok || act.Priority != batteryPriority || act.Trailing != "55%" {
		t.Fatalf("transient = %+v ok=%v", act, ok)
	}
	// Expired transient falls back to the ongoing warning.
	act, ok = m.activity(now.Add(batteryFlash + time.Second))
	if !ok || act.Priority != lowBatteryPriority {
		t.Fatalf("after expiry = %+v ok=%v", act, ok)
	}

	// Setting off silences both.
	a.settings.IslandBattery = false
	if _, ok = m.activity(now); ok {
		t.Fatal("islandBattery=off must silence the module")
	}
}

func TestBatteryIcons(t *testing.T) {
	m := &batteryModule{}
	for _, p := range []int{0, 5, 50, 95, 100} {
		if m.batteryIcon(p) == nil {
			t.Fatalf("batteryIcon(%d) is nil", p)
		}
	}
	if m.batteryIcon(50) != m.batteryIcon(54) {
		t.Fatal("same decile must reuse the cached icon")
	}
	if boltIcon() == nil {
		t.Fatal("boltIcon is nil")
	}
}

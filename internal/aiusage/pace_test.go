package aiusage

import (
	"testing"
	"time"
)

func win(length time.Duration, resetsAt time.Time, used float64) Window {
	return Window{UsedPct: used, Length: length, ResetsAt: resetsAt}
}

var reset = time.Date(2027, 1, 10, 17, 0, 0, 0, time.UTC)

func TestPaceStartOfWindow(t *testing.T) {
	// Now == window start: no elapsed time, even-pace is 100% left.
	now := reset.Add(-5 * time.Hour)
	p := Pace(win(5*time.Hour, reset, 0), now)
	if p.ExpectedLeftPct != 100 {
		t.Fatalf("ExpectedLeftPct = %v", p.ExpectedLeftPct)
	}
	if p.AheadPct != 0 {
		t.Fatalf("AheadPct = %v", p.AheadPct)
	}
	if !p.LastsUntilReset {
		t.Fatal("0% used must last until reset")
	}
}

func TestPaceEndOfWindow(t *testing.T) {
	// Now == reset: e clamps to 1, expected left is 0.
	p := Pace(win(5*time.Hour, reset, 80), reset)
	if p.ExpectedLeftPct != 0 {
		t.Fatalf("ExpectedLeftPct = %v", p.ExpectedLeftPct)
	}
	if p.AheadPct != -20 { // 20 left vs 0 expected → behind pace
		t.Fatalf("AheadPct = %v", p.AheadPct)
	}
	if !p.LastsUntilReset {
		t.Fatal("past reset should report LastsUntilReset")
	}
}

func TestPaceZeroUsed(t *testing.T) {
	p := Pace(win(5*time.Hour, reset, 0), reset.Add(-time.Hour))
	if !p.LastsUntilReset {
		t.Fatal("0% used must last until reset")
	}
	if p.AheadPct != -(100 - p.ExpectedLeftPct) {
		t.Fatalf("AheadPct = %v", p.AheadPct)
	}
}

func TestPaceHundredUsed(t *testing.T) {
	// Window is 20% elapsed but already exhausted.
	now := reset.Add(-4 * time.Hour)
	p := Pace(win(5*time.Hour, reset, 100), now)
	if p.LastsUntilReset {
		t.Fatal("100% used cannot last")
	}
	if p.RunsOutIn != 0 {
		t.Fatalf("RunsOutIn = %v", p.RunsOutIn)
	}
	if p.AheadPct != 80 { // expected 80 left, actually 0
		t.Fatalf("AheadPct = %v", p.AheadPct)
	}
}

func TestPaceEvenPace(t *testing.T) {
	// Half elapsed, half used → exactly on pace.
	now := reset.Add(-150 * time.Minute)
	p := Pace(win(5*time.Hour, reset, 50), now)
	if p.ExpectedLeftPct != 50 {
		t.Fatalf("ExpectedLeftPct = %v", p.ExpectedLeftPct)
	}
	if p.AheadPct != 0 {
		t.Fatalf("AheadPct = %v", p.AheadPct)
	}
	// 50% used in 2.5h → remaining 50% needs 2.5h, exactly the 2.5h left.
	if !p.LastsUntilReset {
		t.Fatalf("on-pace window should last, RunsOutIn %v", p.RunsOutIn)
	}
}

func TestPaceFasterThanEven(t *testing.T) {
	// Half elapsed, 75% used → 25 left vs 50 expected.
	now := reset.Add(-150 * time.Minute)
	p := Pace(win(5*time.Hour, reset, 75), now)
	if p.ExpectedLeftPct != 50 {
		t.Fatalf("ExpectedLeftPct = %v", p.ExpectedLeftPct)
	}
	if p.AheadPct != 25 {
		t.Fatalf("AheadPct = %v", p.AheadPct)
	}
	// rate 75/2.5h = 30%/h → 25% left burns in 50min < 2.5h remaining.
	if p.LastsUntilReset {
		t.Fatal("should not last until reset")
	}
	if p.RunsOutIn != 50*time.Minute {
		t.Fatalf("RunsOutIn = %v", p.RunsOutIn)
	}
}

func TestPaceSlowerThanEven(t *testing.T) {
	// Half elapsed, only 25% used → 75 left vs 50 expected.
	now := reset.Add(-150 * time.Minute)
	p := Pace(win(5*time.Hour, reset, 25), now)
	if p.AheadPct != -25 {
		t.Fatalf("AheadPct = %v", p.AheadPct)
	}
	if !p.LastsUntilReset {
		t.Fatalf("should last, RunsOutIn %v", p.RunsOutIn)
	}
	// rate 25/2.5h = 10%/h → 75% lasts 7.5h ≥ 2.5h.
	if p.RunsOutIn != 7*time.Hour+30*time.Minute {
		t.Fatalf("RunsOutIn = %v", p.RunsOutIn)
	}
}

func TestPaceWeekly(t *testing.T) {
	// Sanity: same math over a 7d window, 1d elapsed, 10% used.
	now := reset.Add(-6 * 24 * time.Hour)
	p := Pace(win(7*24*time.Hour, reset, 10), now)
	// rate 10%/day → 90% lasts 9d ≥ 6d.
	if !p.LastsUntilReset {
		t.Fatal("should last")
	}
	if p.RunsOutIn != 9*24*time.Hour {
		t.Fatalf("RunsOutIn = %v", p.RunsOutIn)
	}
}

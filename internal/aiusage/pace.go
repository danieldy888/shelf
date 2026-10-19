package aiusage

import "time"

// Pace compares a window's usage against an even burn: at the current moment
// an even user would have ExpectedLeftPct left, so AheadPct > 0 means the
// subscription is being consumed faster than evenly (the bar is below the
// even-pace marker).
//
// RunsOutIn projects when the window hits 100% at the current rate; it is
// meaningful only when LastsUntilReset is false. A window that just started,
// has 0% used, or is already over reports LastsUntilReset so the UI shows
// "会持续到重置" rather than a bogus countdown.
func Pace(w Window, now time.Time) PaceInfo {
	var p PaceInfo

	// e: fraction of the window already elapsed, clamped to 0..1.
	var e float64
	if w.Length > 0 {
		e = float64(now.Sub(w.ResetsAt.Add(-w.Length))) / float64(w.Length)
		e = max(0, min(1, e))
	} else if !now.Before(w.ResetsAt) {
		e = 1
	}
	p.ExpectedLeftPct = 100 * (1 - e)
	p.AheadPct = p.ExpectedLeftPct - (100 - w.UsedPct)

	remaining := w.ResetsAt.Sub(now)
	switch {
	case w.UsedPct <= 0:
		p.LastsUntilReset = true
	case remaining <= 0:
		p.LastsUntilReset = true
	case e <= 0:
		p.LastsUntilReset = true
	default:
		elapsed := w.Length - remaining
		// Same as (100-UsedPct)/(UsedPct/elapsed), ordered to stay exact.
		p.RunsOutIn = time.Duration(float64(elapsed) * (100 - w.UsedPct) / w.UsedPct)
		if p.RunsOutIn < 0 {
			p.RunsOutIn = 0
		}
		p.LastsUntilReset = p.RunsOutIn >= remaining
	}
	return p
}

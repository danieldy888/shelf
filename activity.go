//go:build darwin

package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// An activity is live status shown around the closed notch, Dynamic
// Island style: a small leading glyph left of the hardware notch and a
// short trailing part right of it. The shelf panel itself is unchanged.
type activity struct {
	// ID names the source ("nowplaying", "battery", "volume"); the island
	// animates when it changes.
	ID string
	// Priority decides between live activities: transient feedback such
	// as a volume change outranks ongoing ones such as music.
	Priority int
	// Leading is drawn left of the notch: artwork or an icon. nil leaves
	// that side empty.
	Leading ui.ImageSource
	// LeadingTint colours an icon Leading (ignored for bitmaps); zero
	// value means the default text colour.
	LeadingTint ui.Color
	// Trailing is short text right of the notch, e.g. "87%".
	Trailing string
	// Level, when in [0,1], draws a small bar right of the notch instead
	// of (or after) Trailing, e.g. volume. Negative means no bar.
	Level float32
	// Until ends a transient activity; zero means it lasts while its
	// provider keeps returning it.
	Until time.Time
}

// An activityProvider is a module that can show an activity. It is asked
// on every monitoring tick, on the main thread, and must answer quickly.
type activityProvider interface {
	activity(now time.Time) (activity, bool)
}

// A panelCard is a module that adds a card at the top of the expanded
// panel's tray tab, e.g. now-playing controls. It is drawn inside the
// panel's content column and must keep to the panel palette.
type panelCard interface {
	card(c *ui.Context) bool // reports whether it drew anything
}

// currentActivity picks the activity to show: the highest priority among
// live ones, ties broken by module order. Transient activities whose
// Until has passed are skipped.
func currentActivity(mods []module, now time.Time) (activity, bool) {
	var best activity
	found := false
	for _, m := range mods {
		p, ok := m.(activityProvider)
		if !ok {
			continue
		}
		act, live := p.activity(now)
		if !live || (!act.Until.IsZero() && !now.Before(act.Until)) {
			continue
		}
		if !found || act.Priority > best.Priority {
			best, found = act, true
		}
	}
	return best, found
}

//go:build darwin

package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// A module is one optional feature hosted by the notch shell. The shell owns
// the window, the three states and incoming drags; a module owns only its own
// state. Modules run on the main thread from the shared monitoring tick and
// throttle themselves, so a slow poll never needs a goroutine of its own.
type module interface {
	tick(now time.Time)
}

// A liveActivity is a module with something under way (now playing; later
// timers or transfers) for the expanded panel's activity strip, which sits
// above every page. It is drawn inside the panel's content column and must
// keep to the panel palette.
type liveActivity interface {
	live() bool // has something to show now
	// activity draws it at liveH tall, growing to share the strip's width;
	// compact asks for one primary control because another activity sits
	// beside it.
	activity(c *ui.Context, compact bool)
}

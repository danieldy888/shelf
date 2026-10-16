//go:build darwin

package main

import "time"

// A module is one optional feature hosted by the notch shell. The shell owns
// the window, the three states and incoming drags; a module owns only its own
// state. Modules run on the main thread from the shared monitoring tick and
// throttle themselves, so a slow poll never needs a goroutine of its own.
type module interface {
	tick(now time.Time)
}

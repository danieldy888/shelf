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

// A panelCard is a module that adds a card at the top of the expanded
// panel's tray tab, e.g. now-playing controls. It is drawn inside the
// panel's content column and must keep to the panel palette.
type panelCard interface {
	card(c *ui.Context) bool // reports whether it drew anything
}

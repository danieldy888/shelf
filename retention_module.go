//go:build darwin

package main

import (
	"fmt"
	"time"
)

// retentionCheck is how often old items are looked for; the first check
// runs on the first tick after launch.
const retentionCheck = time.Hour

// retentionChoices are the periods offered in the menu-bar menu.
var retentionChoices = []int{1, 7, 30}

// retentionModule removes items older than the retentionDays setting. It
// reads the setting on every check, so a change from the menu applies at
// once without restarting.
type retentionModule struct {
	host *shelf
	last time.Time
}

func (m *retentionModule) tick(now time.Time) {
	if !m.last.IsZero() && now.Sub(m.last) < retentionCheck {
		return
	}
	m.last = now
	m.host.removeExpired(now)
}

// removeExpired applies the retention setting once.
func (a *shelf) removeExpired(now time.Time) {
	days := a.settings.RetentionDays
	if days <= 0 || a.store == nil {
		return
	}
	n, err := a.store.RemoveOlderThan(now.AddDate(0, 0, -days))
	if err != nil {
		// n > 0: removed and saved, but an image file could not be deleted.
		a.report(err)
	}
	if n > 0 {
		a.dropStaleThumbnails()
		a.selection.keepExisting(a.store.Items)
		if err == nil {
			a.status, a.statusUntil = fmt.Sprintf("已自动清理 %d 项超过 %d 天的内容", n, days), now.Add(5*time.Second)
		}
		if a.win != nil {
			a.win.Invalidate()
		}
	}
}

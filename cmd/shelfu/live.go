//go:build darwin

package main

// Realtime activities on the notch: charging, headphones and downloads.
// They share one queue of transient events (one at a time, arrival order)
// that take over the peek shape and hand it back afterwards. A download in
// progress is not an event: it holds the wings for as long as it runs and
// outranks playing music. The shell itself (notch, peek, expansion) is
// untouched; this file only decides which shape is shown and what it says.
//
// Queue rules, unit-tested:
//   - one event at a time, in arrival order;
//   - an event that waited or was hidden while the panel was open keeps its
//     own deadline (hold measured from arrival) and is dropped unseen;
//   - opening the panel cancels what is queued, so nothing pops afterwards;
//   - a switch that is off drops its events at the door.

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The three highlight colours the activities use inside the black shell.
var (
	liveGreen = ui.Hex("#30D158")
	liveBlue  = ui.Hex("#0A84FF")
	liveRed   = ui.Hex("#FF453A")
)

const (
	// liveWingW is how far each side grows for charging and downloads: the
	// notch only gets wider, never taller than its band plus 4. The design
	// sheet's 88pt wing leaves 74pt inside the visible band after its 14pt
	// inset, while the 66pt bar + 10pt gap + a three-digit 13pt number need
	// 98pt; the middle of the shell sits behind the camera and has no
	// pixels, so the wing takes the width its right-hand content needs
	// (14 + 66 + 10 + 22 = 112) and both sides stay symmetric.
	liveWingW   = 112
	liveWingPad = 14
	// liveBarW/liveBarH are the charge bar's size (Droppy's).
	liveBarW = 66
	liveBarH = 6

	liveHoldCharge   = 3 * time.Second
	liveHoldFull     = 3 * time.Second
	liveHoldLow      = 5 * time.Second
	liveHoldHeadset  = 3500 * time.Millisecond
	liveHoldUnplug   = 2 * time.Second
	liveHoldDownload = 4 * time.Second
)

// liveKind names one transient event.
type liveKind int

const (
	liveCharging liveKind = iota
	liveCharged
	liveLow
	liveHeadset
	liveHeadsetOff
	liveDownloadDone
)

// wings reports whether the event uses the wide two-wing shape instead of
// the capsule under the notch.
func (k liveKind) wings() bool {
	return k == liveCharging || k == liveCharged || k == liveLow
}

// liveEvent is one queued activity.
type liveEvent struct {
	kind   liveKind
	label  string   // 正在充电 / 已充满 / 电量低 / device or file name
	levels []string // 左 80% / 右 78% / 盒 50%, when the device reports them
	pct    int
	path   string // downloaded file, for the completion capsule's click
	token  string // device UID: a late battery reading enriches this event
	width  float32
	// deadline is arrival + hold: waiting behind another event or behind a
	// brief panel opening never extends how long the words can stay.
	deadline time.Time
}

// liveQueue is the one-at-a-time event queue.
type liveQueue struct {
	queued []liveEvent
	cur    liveEvent
	until  time.Time
	shown  bool
}

func (a *shelf) liveEnabled(kind liveKind) bool {
	s := a.settings.LiveActivities
	switch kind {
	case liveCharging, liveCharged, liveLow:
		return s.Charging
	case liveHeadset, liveHeadsetOff:
		return s.Headphones
	case liveDownloadDone:
		return s.Downloads
	}
	return false
}

// pushLive queues one activity. Events posted while the panel is open are
// dropped: the design shows nothing transient over an open panel, and a
// late popup after closing is not what the event meant.
func (a *shelf) pushLive(ev liveEvent, hold time.Duration, now time.Time) {
	if !a.liveEnabled(ev.kind) || a.visible {
		return
	}
	ev.deadline = now.Add(hold)
	ev.width = liveCapsuleWidth(ev, float32(max(180, a.notchW)))
	a.live.queued = append(a.live.queued, ev)
	if a.win != nil {
		a.win.Invalidate()
	}
}

// clearLive drops everything queued or showing; opening the panel calls it
// so no event survives behind it.
func (a *shelf) clearLive() {
	a.live = liveQueue{}
}

// liveNow advances the queue and returns the event to draw. allow is false
// while the panel is open or a file drag owns the notch; nothing is
// started then, and the shown event is hidden (not cancelled: its deadline
// still runs).
func (a *shelf) liveNow(now time.Time, allow bool) (liveEvent, bool) {
	q := &a.live
	if q.shown {
		if allow && now.Before(q.until) && a.liveEnabled(q.cur.kind) {
			return q.cur, true
		}
		q.shown = false
		if !now.Before(q.until) {
			q.cur = liveEvent{}
		}
	}
	if !allow {
		return liveEvent{}, false
	}
	for len(q.queued) > 0 {
		ev := q.queued[0]
		q.queued = q.queued[1:]
		if !now.Before(ev.deadline) {
			continue // waited out its own hold unseen
		}
		if !a.liveEnabled(ev.kind) {
			continue // its switch went off after it was queued
		}
		q.cur, q.until, q.shown = ev, ev.deadline, true
		return ev, true
	}
	return liveEvent{}, false
}

// liveEnrich attaches a late battery reading to the event that announced
// the same device, whether it is on screen or still queued.
func (a *shelf) liveEnrich(token string, levels []string) {
	if token == "" {
		return
	}
	apply := func(ev *liveEvent) bool {
		if ev.token != token || ev.kind != liveHeadset {
			return false
		}
		ev.levels = levels
		ev.width = liveCapsuleWidth(*ev, float32(max(180, a.notchW)))
		return true
	}
	changed := false
	if a.live.shown && apply(&a.live.cur) {
		changed = true
	}
	for i := range a.live.queued {
		if apply(&a.live.queued[i]) {
			changed = true
		}
	}
	if changed && a.win != nil {
		a.win.Invalidate()
	}
}

// liveShape is the shell the event needs: the wide wings or a capsule.
func (a *shelf) liveShape(ev liveEvent) (w, h float32) {
	baseW, baseH := float32(max(180, a.notchW)), float32(max(32, a.notchH))
	if ev.kind.wings() {
		return baseW + 2*liveWingW, baseH + npWingsBody
	}
	w = ev.width
	if w <= 0 {
		w = liveCapsuleWidth(ev, baseW)
	}
	return w, baseH + fbBody
}

// liveDownloadDoneShape is the completion capsule's box, computed from the
// event exactly as liveShape does (used by the click test).
func liveCapsuleWidth(ev liveEvent, baseW float32) float32 {
	minW := float32(280)
	switch ev.kind {
	case liveHeadset:
		minW = 290
		if len(ev.levels) > 0 {
			minW = 330
		}
	case liveHeadsetOff:
		minW = 250
	}
	w := liveIconWidth(ev.kind) + 2*fbPadX
	first := false
	add := func(text string, size float32) {
		if text == "" {
			return
		}
		if !first {
			w += fbGap
		}
		first = false
		w += liveTextWidth(text, size)
	}
	switch ev.kind {
	case liveHeadset, liveHeadsetOff:
		add(ev.label, 12)
		for _, lv := range ev.levels {
			add(lv, 12)
		}
		if ev.kind == liveHeadsetOff {
			add("已断开", 12)
		} else if len(ev.levels) == 0 {
			add("已连接", 12)
		}
	case liveDownloadDone:
		add("已下载", 12)
		add(ev.label, 12)
		w += fbGap + 7 // the green dot
	}
	w = max(w, minW, baseW+60)
	return min(w, fbMaxW)
}

// liveTextWidth estimates a capsule line at the shell's 12pt weight; the
// layout then fits the real font inside the width this reserved.
func liveTextWidth(s string, size float32) float32 {
	var w float32
	for _, r := range s {
		switch {
		case r > 0x2E80: // CJK and full-width punctuation
			w += size
		default:
			w += size * 0.56
		}
	}
	return w
}

// liveIconWidth is the leading glyph's width in the capsule.
func liveIconWidth(k liveKind) float32 {
	switch k {
	case liveHeadset:
		return 18
	case liveHeadsetOff:
		return 7
	case liveDownloadDone:
		return 13
	}
	return 13
}

// openDownloaded puts the finished file into the tray and opens its page —
// the completion capsule's click.
func (a *shelf) openDownloaded(ev liveEvent) {
	if ev.path == "" {
		return
	}
	if a.store == nil {
		return
	}
	if err := a.store.AddFiles([]string{ev.path}); err != nil {
		a.report(err)
		return
	}
	a.clearLive()
	a.show(false)
	a.setTab(pageTray)
	if a.currentLevel() == 1 {
		a.setLevel(2)
	}
}

// liveAllow is when the notch may show a transient activity: the panel is
// closed (or fully folded) and no incoming file drag owns the shape.
func (a *shelf) liveAllow() bool {
	return !a.visible && a.progress <= 0.01 && !(a.dragging && !a.ownDrag)
}

// liveUp reports whether a live activity currently occupies the peek
// shape, for the mouse gate. It does not advance the queue.
func (a *shelf) liveUp() bool {
	return a.live.shown || a.downloadRunning()
}

// liveClickable reports whether the drawn shape is the completion capsule,
// whose click belongs to the view and not to the shell's open handler.
func (a *shelf) liveClickable(now time.Time) bool {
	if !a.live.shown || now.After(a.live.until) {
		return false
	}
	return a.live.cur.kind == liveDownloadDone
}

// downloadFile is one browser download that is still in flight.
type downloadFile struct {
	temp   string // the temporary file/bundle
	name   string // final file name
	target string // final path
	done   int64
	total  int64 // 0 when the browser does not say
	mod    time.Time
}

// downloadProgress is the persistent activity's snapshot.
type downloadProgress struct {
	file  downloadFile
	count int // further downloads beyond the newest
}

// downloadRunning reports whether a download currently owns the wings.
func (a *shelf) downloadRunning() bool {
	return a.dl != nil && a.dl.active() && !a.visible && a.settings.LiveActivities.Downloads
}

// liveShapeForDownload is the wings box for a running download.
func (a *shelf) downloadShape() (w, h float32) {
	baseW, baseH := float32(max(180, a.notchW)), float32(max(32, a.notchH))
	return baseW + 2*liveWingW, baseH + npWingsBody
}

// liveStatusLine is the one-line description used for accessibility and
// the report; it keeps the event's wording in one place.
func (ev liveEvent) statusLine() string {
	switch ev.kind {
	case liveDownloadDone:
		return "已下载 " + ev.label
	case liveHeadsetOff:
		return ev.label + " 已断开"
	case liveHeadset:
		return strings.Join(append([]string{ev.label}, ev.levels...), " ")
	case liveCharging, liveCharged, liveLow:
		return fmt.Sprintf("%s %d%%", ev.label, ev.pct)
	}
	return ev.label
}

// announceLive posts one of the power/headset events. Small helper so the
// modules share the timestamp and the sweep.
func (a *shelf) announceLive(ev liveEvent, hold time.Duration) {
	a.pushLive(ev, hold, time.Now())
}

// mygoRunOnMain is the seam the battery lookup uses to deliver its result.
var mygoRunOnMain = mygo.RunOnMain

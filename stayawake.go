//go:build darwin

package main

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
)

// awakeModule keeps the Mac from idle-sleeping for a chosen span, meant
// for letting a long task finish. One named IOKit power assertion
// (「Shelf Stay Awake」) holds while the feature is on: PreventUserIdleSystemSleep
// by default — the screen may still dim and turn off — or
// PreventUserIdleDisplaySleep while 「屏幕也保持亮着」 is chosen. Timed
// holds release on their deadline, on 关闭, and when Shelf quits;
// 一直 never expires. Nothing is persisted: a relaunch starts off.
//
// The module runs off the shared monitoring tick and throttles itself to
// a second-granularity countdown redraw; it owns no goroutine. The power
// calls are seam functions so tests never touch IOKit.
type awakeModule struct {
	host *shelf

	// State: id 0 means off; duration 0 with an id means 「一直」.
	id       mac.PowerAssertionID
	display  bool // 「屏幕也保持亮着」
	duration time.Duration
	until    time.Time // deadline for timed holds; zero for 「一直」
	remain   string    // last countdown text drawn, to redraw on change

	// create/release and now are test seams; nil means the real calls.
	create  func(mac.PowerAssertionKind) (mac.PowerAssertionID, error)
	release func(mac.PowerAssertionID)
	now     func() time.Time
}

// awakeDurations are the timed choices of the 保持唤醒 submenu, in order;
// a duration of 0 is 一直.
var awakeDurations = []struct {
	label string
	d     time.Duration
}{
	{"30 分钟", 30 * time.Minute},
	{"1 小时", time.Hour},
	{"2 小时", 2 * time.Hour},
	{"4 小时", 4 * time.Hour},
	{"一直（直到关闭）", 0},
}

// newAwakeModule wires the feature into the shell; the quit handler makes
// sure a held assertion is released even when the deadline never comes —
// the OS would drop it at exit anyway, this just keeps pmset honest.
func newAwakeModule(host *shelf) *awakeModule {
	m := &awakeModule{host: host}
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { m.stop(time.Now()) })
	return m
}

// awakeMenu is the 保持唤醒 submenu of the tray menu: the timed choices
// with the current one checked, the display toggle, and 关闭 greyed out
// while nothing is held. Rebuilt with the menu on every right-click.
func (a *shelf) awakeMenu() []*mygo.MenuItem {
	m := a.awake

	var items []*mygo.MenuItem
	for _, c := range awakeDurations {
		d := c.d
		items = append(items, &mygo.MenuItem{
			Label: c.label, Type: mygo.MenuItemRadio,
			Checked: m.active() && m.duration == d,
			Click:   func(*mygo.MenuItem, *mygo.Window) { m.start(time.Now(), d) },
		})
	}
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{
			Label: "屏幕也保持亮着", Type: mygo.MenuItemCheckbox, Checked: m.wantsDisplay(),
			Click: func(*mygo.MenuItem, *mygo.Window) { m.setDisplay(time.Now(), !m.display) },
		},
		mygo.Separator(),
		&mygo.MenuItem{
			Label: "关闭", Disabled: !m.active(),
			Click: func(*mygo.MenuItem, *mygo.Window) { m.stop(time.Now()) },
		},
	)
	return items
}

// awakeAssertionName is the name powerd records for the hold (AssertName).
// ASCII only: `pmset -g assertions` prints an empty `named:` for any
// non-ASCII name, so this keeps the hold visible in that listing.
const awakeAssertionName = "Shelf Stay Awake"

func (m *awakeModule) active() bool { return m != nil && m.id != 0 }

// wantsDisplay reads 屏幕也保持亮着; nil-safe like active, for menu
// building on shelves that never ran ready().
func (m *awakeModule) wantsDisplay() bool { return m != nil && m.display }

// start turns the feature on for d (0 = 一直); a fresh pick while on just
// re-arms the timer — the same assertion type stays held, so there is no
// sleep gap.
func (m *awakeModule) start(now time.Time, d time.Duration) {
	m.duration, m.until = d, time.Time{}
	if d > 0 {
		m.until = now.Add(d)
	}
	if m.active() {
		m.refresh(now)
		return
	}
	if err := m.reassert(); err != nil {
		m.duration, m.until = 0, time.Time{}
		m.fail(err)
	}
	m.refresh(now)
}

// stop releases the assertion, if any.
func (m *awakeModule) stop(now time.Time) {
	if !m.active() {
		return
	}
	m.releaseAssert(m.id)
	m.id = 0
	m.duration, m.until = 0, time.Time{}
	m.refresh(now)
}

// setDisplay flips 屏幕也保持亮着. While the feature is on it swaps the
// held assertion for the other type, creating the new one first so a
// failure never leaves a gap; on failure the toggle rolls back.
func (m *awakeModule) setDisplay(now time.Time, on bool) {
	if m.display == on {
		return
	}
	m.display = on
	if m.active() {
		if err := m.reassert(); err != nil {
			m.display = !on
			m.fail(err)
		}
	}
	m.refresh(now)
}

// reassert replaces the held assertion with one matching m.display. The
// new assertion is created before the old id is released, so a failed
// create keeps the old hold and reports an error instead.
func (m *awakeModule) reassert() error {
	id, err := m.createAssert(m.wantKind())
	if err != nil {
		return err
	}
	if m.id != 0 {
		m.releaseAssert(m.id)
	}
	m.id = id
	return nil
}

// wantKind is the assertion type the current display flag asks for.
func (m *awakeModule) wantKind() mac.PowerAssertionKind {
	if m.display {
		return mac.PowerAssertDisplaySleep
	}
	return mac.PowerAssertSystemSleep
}

// fail surfaces a creation error: the panel opens first so the toast is
// actually seen (a collapsed panel paints nothing and the deadline would
// run out unseen), then the 4-second notice starts with the red dot. The
// state reads off, so a re-opened menu shows nothing checked and 关闭 grey.
func (m *awakeModule) fail(err error) {
	m.host.show(false)
	m.host.toastStatus("无法开启保持唤醒："+err.Error(), m.clock().Add(4*time.Second), true)
}

// tick releases a timed hold at its deadline and keeps the countdown
// moving at second granularity — and only while the panel can show it.
func (m *awakeModule) tick(now time.Time) {
	if !m.active() {
		return
	}
	if !m.until.IsZero() && !now.Before(m.until) {
		m.stop(now)
		return
	}
	if m.host.visible {
		m.refresh(now)
	}
}

// live reports whether the activity strip shows the awake row.
func (m *awakeModule) live() bool { return m.active() }

// refresh redraws the panel when the countdown text changed.
func (m *awakeModule) refresh(now time.Time) {
	next := m.remainText(now)
	if next == m.remain {
		return
	}
	m.remain = next
	if m.host.win != nil {
		m.host.win.Invalidate()
	}
}

// remainText is the strip's second line: a m:ss / h:mm:ss countdown for
// timed holds, 「直到关闭」 for 一直.
func (m *awakeModule) remainText(now time.Time) string {
	if !m.active() {
		return ""
	}
	if m.until.IsZero() {
		return "直到关闭"
	}
	rem := m.until.Sub(now)
	if rem < 0 {
		rem = 0
	}
	return "剩余 " + mac.FormatClock(rem.Seconds())
}

func (m *awakeModule) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *awakeModule) createAssert(kind mac.PowerAssertionKind) (mac.PowerAssertionID, error) {
	if m.create != nil {
		return m.create(kind)
	}
	return mac.NewPowerAssertion(kind, awakeAssertionName)
}

func (m *awakeModule) releaseAssert(id mac.PowerAssertionID) {
	if m.release != nil {
		m.release(id)
		return
	}
	mac.ReleasePowerAssertion(id)
}

// activity draws the strip row: the slashed-moon tile, 「保持唤醒」 with
// the countdown (or 「直到关闭」), and × to stop. Sharing the strip
// (compact) it keeps only the time text and the stop button.
var awakeSymbol = symbol(`<path d="M20.2 14.5A8.5 8.5 0 0 1 9.5 3.8a8.5 8.5 0 1 0 10.7 10.7z"/><path d="m4.5 19.5 15-15"/>`)

func (m *awakeModule) activity(c *ui.Context, compact bool) {
	if !m.active() {
		return
	}
	now := m.clock()
	row := ui.Row(c.Key("awake")).Height(liveH).Grow(1).MinWidth(0).Radius(14).Background(fill1).
		Padding(0, 10, 0, 10).Gap(10).AlignItems(ui.Center)
	row.Children(func() {
		if !compact {
			ui.Row(c.Key("awake-tile")).Size(36, 36).Radius(8).Center().Background(fill1).Children(func() {
				ui.Icon(c, awakeSymbol).Size(18, 18).TextColor(ink)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Justify(ui.Center).Children(func() {
				ui.Text(c.Key("awake-title"), "保持唤醒").SingleLine().FontSize(13).FontWeight(600).TextColor(ink)
				ui.Text(c.Key("awake-remain"), m.remainText(now)).SingleLine().FontSize(11).TextColor(ink2)
			})
		} else {
			ui.Text(c.Key("awake-remain"), m.remainText(now)).SingleLine().FontSize(13).TextColor(ink).Grow(1).MinWidth(0)
		}
		b := ui.ButtonBase(c.Key("awake-stop")).Size(24, 24).Radius(12).Center().Background(fill2).Label("关闭保持唤醒")
		b.Children(func() { ui.Icon(c, closeSymbol).Size(10, 10).TextColor(ink2) })
		if b.Clicked() {
			m.stop(now)
		}
	})
}

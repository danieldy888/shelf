//go:build darwin

package main

// Widget bodies for the 主页 canvas. Each widget draws for the size the
// user gave it (1×1, 2×1, 2×2); the music widget adds the progress and
// volume controls the plan asks for.

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/aiusage"
	"shelf/internal/layout"
	"shelf/internal/mac"
	"shelf/internal/store"
)

var (
	homeNavSymbol = symbol(`<path d="M4 11.5 12 5l8 6.5V19a1 1 0 0 1-1 1h-4.5v-5h-5v5H5a1 1 0 0 1-1-1z"/>`)
	volLoSymbol   = symbol(`<path d="M4 10h3l4-3.5v11L7 14H4z"/>`)
	volHiSymbol   = symbol(`<path d="M4 10h3l4-3.5v11L7 14H4z"/><path d="M15 9a4.5 4.5 0 0 1 0 6M17.5 6.5a8 8 0 0 1 0 11"/>`)
)

// volumeControl is the default output device's volume. Tests replace it
// with a fake so no test ever changes the machine's volume.
type volumeControl interface {
	level() (float32, bool)
	set(float32) error
}

type systemVolume struct{}

func (systemVolume) level() (float32, bool) { return mac.OutputVolume() }
func (systemVolume) set(v float32) error    { return mac.SetOutputVolume(v) }

// volumeControl is the seam: nil means the real CoreAudio device.
func (a *shelf) volumeControl() volumeControl {
	if a.vol != nil {
		return a.vol
	}
	return systemVolume{}
}

// homeWidgetBody dispatches a widget's content for its rect.
func (p *homePage) homeWidgetBody(c *ui.Context, card ui.Element, w layout.Widget, edit bool) {
	if edit {
		def := p.homeWidgetDef(w.ID)
		card.Center().Gap(5).Children(func() {
			if def != nil {
				ui.Icon(c, def.icon).Size(20, 20).TextColor(ink)
				fitText(c, def.title, 12, 600, homeSpanW(w.W)-20).TextColor(ink)
			}
			fitText(c, fmt.Sprintf("%d × %d", w.W, w.H), 10, 0, homeSpanW(w.W)-20).TextColor(ink3)
		})
		return
	}
	switch w.ID {
	case "playing":
		p.musicBody(c, card, w)
	case "awake":
		p.awakeBody(c, card, w)
	case "usage":
		p.usageBody(c, card, w)
	case "recent":
		p.recentBody(c, card, w)
	case "clip":
		p.clipBody(c, card, w)
	}
}

// musicBody draws the music widget: 2×2 is the full player (cover,
// title, seekable progress, transport, volume), 2×1 keeps cover + title +
// transport, 1×1 is a compact cover + title.
func (p *homePage) musicBody(c *ui.Context, card ui.Element, w layout.Widget) {
	m := p.host.np
	var st mac.NowPlayingState
	ok := false
	if m != nil {
		st, ok = m.track()
	}
	if !ok {
		card.Center().Gap(7).Children(func() {
			ui.Row(c.Key("hw-empty-art")).Size(44, 44).Radius(10).Background(fill2).
				Opacity(0.6).Center().Children(func() {
				ui.Icon(c, musicSymbol).Size(18, 18).TextColor(ink3)
			})
			fitText(c, "没有在播放", 11, 0, homeSpanW(w.W)-20).TextColor(ink3)
		})
		return
	}
	switch {
	case w.W == 2 && w.H == 2:
		p.musicFull(c, card, st)
	case w.W == 2:
		p.musicWide(c, card, st)
	default:
		p.musicCompact(c, card, st)
	}
}

// musicFull is the 2×2 player, laid out like the prototype's .mus card.
func (p *homePage) musicFull(c *ui.Context, card ui.Element, st mac.NowPlayingState) {
	m := p.host.np
	dur := st.Duration
	now := time.Now()
	pos := st.Position(now)
	if dur > 0 {
		pos = min(max(pos, 0), dur)
	}
	frac := float32(0)
	if dur > 0 {
		frac = float32(pos / dur)
	}
	if p.dragPos && dur > 0 {
		frac = p.posFrac
		pos = float64(frac) * dur
	}
	card.Padding(9, 10, 9, 10).Justify(ui.SpaceBetween).Children(func() {
		ui.Row(c.Key("hw-top")).Gap(9).AlignItems(ui.Center).MinWidth(0).Children(func() {
			m.coverArt(c, "hw-art", 44, 10)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c.Key("hw-title"), st.Title).SingleLine().FontSize(12.5).FontWeight(600).TextColor(ink)
				if meta := m.cardMeta(st); meta != "" {
					ui.Text(c.Key("hw-meta"), meta).SingleLine().FontSize(10.5).TextColor(ink2)
				}
			})
		})
		ui.Row(c.Key("hw-track-row")).AlignItems(ui.Center).Gap(6).Children(func() {
			rem := float64(0)
			if dur > 0 {
				rem = max(0, dur-pos)
			}
			// The labels are as wide as their own text at this font: a
			// fixed 30pt clipped -10:00 and 1:02:03, and the bar takes
			// what is left of the card's 175pt inner width.
			nowLabel, remLabel := mac.FormatClock(pos), "-"+mac.FormatClock(rem)
			nw, rw := clockLabelW(c, nowLabel), clockLabelW(c, remLabel)
			panelLabel(c, nil, nowLabel, 9.5, 500).TextColor(ink3).Width(nw)
			p.progressBar(c, "hw-progress", frac, dur > 0, dur, max(40, homeTrackW-12-nw-rw))
			panelLabel(c, nil, remLabel, 9.5, 500).TextColor(ink3).Width(rw)
		})
		ui.Row(c.Key("hw-ctrl")).Justify(ui.Center).AlignItems(ui.Center).Gap(18).Children(func() {
			m.controlButton(c, "hw-prev", prevFill, "上一首", mac.NPPrevious, 26, 15)
			m.controlButton(c, "hw-toggle", toggleSymbol(st.Playing), playPauseLabel(st.Playing), mac.NPPlayPause, 30, 19)
			m.controlButton(c, "hw-next", nextFill, "下一首", mac.NPNext, 26, 15)
		})
		p.volumeRow(c)
	})
}

// musicWide is the 2×1 player: cover, title/artist, play/pause.
func (p *homePage) musicWide(c *ui.Context, card ui.Element, st mac.NowPlayingState) {
	m := p.host.np
	card.Padding(0, 10, 0, 10).Justify(ui.Center).Children(func() {
		ui.Row(c.Key("hw-wide")).AlignItems(ui.Center).Gap(9).FillWidth().Children(func() {
			m.coverArt(c, "hw-art", 44, 10)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c.Key("hw-title"), st.Title).SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
				if meta := m.cardMeta(st); meta != "" {
					ui.Text(c.Key("hw-meta"), meta).SingleLine().FontSize(10).TextColor(ink2)
				}
			})
			m.controlButton(c, "hw-toggle", toggleSymbol(st.Playing), playPauseLabel(st.Playing), mac.NPPlayPause, 28, 17)
		})
	})
}

// musicCompact is the 1×1 form: cover and a one-line title; a tap
// toggles playback.
func (p *homePage) musicCompact(c *ui.Context, card ui.Element, st mac.NowPlayingState) {
	m := p.host.np
	card.Center().Gap(4).Padding(0, 8, 0, 8).Children(func() {
		m.coverArt(c, "hw-art", 40, 9)
		ui.Text(c.Key("hw-title"), st.Title).SingleLine().FontSize(10.5).FontWeight(600).
			TextColor(ink).MaxWidth(homeCellW() - 16)
	})
}

// homeTrackW is the music 2×2 card's inner width: the 195pt widget span
// less its 10pt side padding.
const homeTrackW = float32(175)

// clockLabelW is the width a 9.5pt time label needs, rounded up with a
// point of slack: the row spends exactly this much on the label.
func clockLabelW(c *ui.Context, s string) float32 {
	w, _ := c.MeasureText(0, ui.Span{Text: s, Size: 9.5, Weight: 500})
	return float32(math.Ceil(float64(w))) + 1
}

// progressBar is the seekable track: a 4pt rail with a knob on hover or
// while dragged. The bar only reports a press to the page; the seek goes
// out once on release.
func (p *homePage) progressBar(c *ui.Context, key string, frac float32, enabled bool, dur float64, width float32) ui.Element {
	if frac < 0 || frac != frac {
		frac = 0
	}
	frac = min(max(frac, 0), 1)
	bar := ui.Row(c.Key(key)).Width(width).Height(12).Label("播放进度")
	bar.PointerPosition()
	if enabled {
		bar.HandleInput(func(ev ui.InputEvent) bool {
			f := min(max(ev.X/width, 0), 1)
			switch ev.Kind {
			case ui.InputPointerDown:
				p.dragPos, p.posFrac = true, f
				return true
			case ui.InputPointerMove:
				if p.dragPos {
					p.posFrac = f
				}
				return true
			case ui.InputPointerUp:
				if p.dragPos {
					p.dragPos = false
					p.posFrac = f
					if p.host.np != nil && dur > 0 {
						p.host.np.seekTo(float64(f) * dur)
					}
				}
				return true
			}
			return false
		})
	}
	active := p.dragPos || bar.Hovered()
	bar.Draw(func(pt *ui.Painter, r ui.Rect) {
		y := r.Y + (r.H-4)/2
		pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: 4}, fill3, 2)
		if frac > 0 {
			pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W * frac, H: 4}, progress, 2)
		}
		if active {
			pt.Fill(ui.Rect{X: r.X + r.W*frac - 5, Y: r.Y + r.H/2 - 5, W: 10, H: 10}, ink, 5)
		}
	})
	return bar
}

// volumeRow is the speaker pair, the volume bar, and the grey-out state
// of a device that cannot change its volume.
func (p *homePage) volumeRow(c *ui.Context) {
	// The row is the only place that knows about the output device, so
	// while it is on screen it asks for the next frame at the poll's pace:
	// a paused panel then still notices a device switch, at the monitor
	// tick's cadence instead of one read per repaint.
	if p.host.visible && p.host.tab == pageHome {
		c.After(volumePoll)
	}
	v, ok := p.volume(c.Now())
	frac := v
	if p.dragVol {
		frac = p.volFrac
	}
	row := ui.Row(c.Key("hw-vol")).AlignItems(ui.Center).Gap(6)
	if !ok {
		row.Opacity(0.4).Tooltip("此设备不支持调音量")
	}
	row.Children(func() {
		ui.Icon(c, volLoSymbol).Size(12, 12).TextColor(ink3)
		p.volumeBar(c, frac, ok)
		ui.Icon(c, volHiSymbol).Size(12, 12).TextColor(ink3)
	})
}

// volumeBar is the 3pt volume rail; a release writes the system volume
// once through the replaceable control.
func (p *homePage) volumeBar(c *ui.Context, frac float32, enabled bool) ui.Element {
	const volBarW = float32(139)
	bar := ui.Row(c.Key("hw-volbar")).Width(volBarW).Height(12).Label("音量")
	bar.PointerPosition()
	if enabled {
		bar.HandleInput(func(ev ui.InputEvent) bool {
			f := min(max(ev.X/volBarW, 0), 1)
			switch ev.Kind {
			case ui.InputPointerDown:
				p.dragVol, p.volFrac = true, f
				return true
			case ui.InputPointerMove:
				if p.dragVol {
					p.volFrac = f
				}
				return true
			case ui.InputPointerUp:
				if p.dragVol {
					p.dragVol = false
					p.volFrac = f
					p.commitVolume(f, c.Now())
				}
				return true
			}
			return false
		})
	}
	active := p.dragVol || bar.Hovered()
	bar.Draw(func(pt *ui.Painter, r ui.Rect) {
		y := r.Y + (r.H-3)/2
		pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: 3}, fill3, 1.5)
		if frac > 0 {
			pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W * frac, H: 3}, ui.RGBA(255, 255, 255, 0.75), 1.5)
		}
		if active {
			pt.Fill(ui.Rect{X: r.X + r.W*frac - 5, Y: r.Y + r.H/2 - 5, W: 10, H: 10}, ink, 5)
		}
	})
	return bar
}

// volumePoll is how often the drawn music widget re-reads the output
// device: the monitor tick's pace, not every frame, so switching the
// system output while the panel is open reaches the bar without a read
// per repaint.
const volumePoll = 500 * time.Millisecond

// volume reads the system volume: once, then on volumePoll's pace while
// the page draws. A drag owns the shown value, so a poll never moves the
// knob under the pointer.
func (p *homePage) volume(now time.Time) (float32, bool) {
	if p.dragVol {
		return p.vol, p.volOK
	}
	if p.volLoaded && now.Sub(p.volAt) < volumePoll {
		return p.vol, p.volOK
	}
	p.volAt = now
	if v, ok := p.host.volumeControl().level(); ok {
		p.vol, p.volOK = v, true
	} else {
		p.volOK = false
	}
	p.volLoaded = true
	return p.vol, p.volOK
}

// commitVolume writes the chosen volume; a device that refuses greys the
// row and says why.
func (p *homePage) commitVolume(v float32, now time.Time) {
	if err := p.host.volumeControl().set(v); err != nil {
		p.volOK = false
		p.volAt = now
		p.volLoaded = true
		p.host.toastStatus("此设备不支持调音量", time.Now().Add(2*time.Second), statusInfo)
		return
	}
	p.vol, p.volOK, p.volAt, p.volLoaded = v, true, now, true
}

// awakeBody draws the keep-awake widget for its size.
func (p *homePage) awakeBody(c *ui.Context, card ui.Element, w layout.Widget) {
	m := p.host.awake
	on := m != nil && m.active()
	big := "关"
	small := "点按开启"
	col := ink3
	if on {
		big, small, col = "一直", "剩余时间", awakeWarn
		if !m.until.IsZero() {
			big = mac.FormatClock(max(0, m.until.Sub(time.Now()).Seconds()))
		}
	}
	if w.W == 1 && w.H == 1 {
		card.Padding(9, 10, 9, 10).Children(func() {
			fitText(c, "保持唤醒", 11, 600, homeCellW()-20).TextColor(ink2)
			ui.Box(c).Height(4)
			fitText(c, big, 19, 700, homeCellW()-20).TextColor(col)
			fitText(c, small, 10, 0, homeCellW()-20).TextColor(ink3)
		})
		return
	}
	card.Padding(0, 12, 0, 12).Justify(ui.Center).Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
				ui.Icon(c, cupSymbol).Size(18, 18).TextColor(ink)
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					fitText(c, "保持唤醒", 11, 600, homeSpanW(w.W)-52).TextColor(ink2)
					fitText(c, big+" · "+small, 9.5, 0, homeSpanW(w.W)-52).TextColor(col)
				})
				if on {
					ui.Box(c.Key("home-awake-dot")).Size(6, 6).Radius(3).Background(awakeWarn)
				}
			})
			if w.H == 2 {
				chips := awakeDurations
				for start := 0; start < len(chips); start += 2 {
					end := min(start+2, len(chips))
					ui.Row(c.Key(fmt.Sprintf("home-awake-chips-%d", start))).Gap(4).Children(func() {
						for _, choice := range chips[start:end] {
							label := choice.label
							if i := strings.Index(label, "（"); i >= 0 {
								label = label[:i]
							}
							p.host.awakeChip(c, "home-chip-"+label, label, choice.d)
						}
					})
				}
			}
		})
	})
}

// usageBody draws the AI usage widget: the least-remaining tool's double
// ring plus its windows.
func (p *homePage) usageBody(c *ui.Context, card ui.Element, w layout.Widget) {
	var five, week *aiusage.Window
	ok := false
	if p.host.usage != nil {
		five, week, ok = p.host.usage.leastWindow()
	}
	label := "AI 用量"
	sub := "暂无数据"
	if ok {
		win := five
		if win == nil {
			win = week
		}
		if win != nil {
			label = fmt.Sprintf("5 小时 %.0f%%", max(0, 100-win.UsedPct))
			if win == week {
				label = fmt.Sprintf("每周 %.0f%%", max(0, 100-win.UsedPct))
			}
			if five != nil && week != nil {
				sub = fmt.Sprintf("每周 %.0f%%", max(0, 100-week.UsedPct))
			}
		}
	}
	switch {
	case w.W == 1 && w.H == 1:
		card.Padding(6, 6, 6, 6).Center().Gap(4).Children(func() {
			p.usageRing(c, 40, five, week)
			fitText(c, label, 9, 0, homeCellW()-12).TextColor(ink2)
		})
	case w.W == 1:
		card.Center().Gap(6).Children(func() {
			p.usageRing(c, 48, five, week)
			fitText(c, label, 9.5, 0, homeCellW()-12).TextColor(ink2)
			fitText(c, sub, 9, 0, homeCellW()-12).TextColor(ink3)
		})
	default:
		card.Padding(0, 10, 0, 10).Justify(ui.Center).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
				p.usageRing(c, 48, five, week)
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					fitText(c, "AI 用量", 11, 600, homeSpanW(w.W)-88).TextColor(ink2)
					fitText(c, label, 10, 0, homeSpanW(w.W)-88).TextColor(ink)
					fitText(c, sub, 9.5, 0, homeSpanW(w.W)-88).TextColor(ink3)
				})
			})
		})
	}
}

// usageRing is the 40/48pt double ring: weekly outside, 5-hour inside.
func (p *homePage) usageRing(c *ui.Context, size float32, five, week *aiusage.Window) ui.Element {
	e := ui.Box(c.Key("home-ring")).Size(size, size).Center()
	e.Draw(func(pt *ui.Painter, r ui.Rect) {
		if p.host.usage == nil {
			return
		}
		lw := r.W * 3.5 / 44
		p.host.usage.drawWindowRing(pt, r, r.W*19/44, lw, week, usageWeekly, false, true)
		p.host.usage.drawWindowRing(pt, r, r.W*13.5/44, lw, five, usageFive, false, true)
	})
	e.Children(func() {
		ui.Icon(c, gaugeSymbol).Size(size*0.3, size*0.3).TextColor(ink3)
	})
	return e
}

// recentBody draws the newest tray files as draggable thumbnails.
func (p *homePage) recentBody(c *ui.Context, card ui.Element, w layout.Widget) {
	items := p.host.itemsFor(pageTray)
	thumb := float32(26)
	cols := 4
	if w.W == 1 {
		thumb, cols = 24, 2
	}
	card.Padding(9, 10, 9, 10).Children(func() {
		fitText(c, "最近文件", 11, 600, homeSpanW(w.W)-20).TextColor(ink2)
		ui.Box(c).Height(6)
		rows := 1
		if w.H == 2 {
			rows = 2
		}
		ui.Column(c).Gap(5).Children(func() {
			for r := range rows {
				ui.Row(c.Key(fmt.Sprintf("home-recent-%d", r))).Gap(6).Children(func() {
					for i := range cols {
						n := r*cols + i
						if n >= len(items) {
							break
						}
						p.host.recentThumbAt(c, items[n], thumb)
					}
				})
			}
			if len(items) == 0 {
				fitText(c, "还没有文件", 10, 0, homeSpanW(w.W)-20).TextColor(ink3)
			}
		})
	})
}

// clipBody draws the newest clipboard capture; a tap on the card copies
// it.
func (p *homePage) clipBody(c *ui.Context, card ui.Element, w layout.Widget) {
	it, ok := p.host.latestClipboardItem()
	title, sub := "", "还没有记录"
	if ok {
		title, sub = itemTitle(it), "点按复制"
	}
	switch {
	case w.W == 1 && w.H == 1:
		card.Padding(6, 8, 6, 8).Center().Gap(3).Children(func() {
			ui.Icon(c, clipNavSymbol).Size(20, 20).TextColor(ink2)
			label := title
			if label == "" {
				label = "剪贴板最新"
			}
			ui.Text(c, label).SingleLine().FontSize(9.5).TextColor(ink).MaxWidth(homeCellW() - 16)
		})
	case w.H == 2:
		card.Padding(9, 10, 9, 10).Children(func() {
			fitText(c, "剪贴板最新", 11, 600, homeSpanW(w.W)-20).TextColor(ink2)
			ui.Box(c).Height(5)
			if ok {
				ui.Text(c, title).SingleLine().FontSize(11.5).FontWeight(600).TextColor(ink)
			}
			fitText(c, sub, 10, 0, homeSpanW(w.W)-20).TextColor(ink3)
			ui.Box(c).Height(4)
			if second, ok := p.host.clipboardItemAt(1); ok {
				ui.Text(c, itemTitle(second)).SingleLine().FontSize(10).TextColor(ink2)
			}
		})
	default:
		card.Padding(0, 10, 0, 10).Justify(ui.Center).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(9).FillWidth().Children(func() {
				ui.Row(c.Key("home-clip-ic")).Size(30, 30).Radius(8).Background(fill2).Center().Children(func() {
					ui.Icon(c, clipNavSymbol).Size(15, 15).TextColor(ink2)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					label := title
					if label == "" {
						label = "剪贴板最新"
					}
					ui.Text(c, label).SingleLine().FontSize(11.5).FontWeight(600).TextColor(ink)
					fitText(c, sub, 10, 0, homeSpanW(w.W)-69).TextColor(ink3)
				})
			})
		})
	}
}

// clipboardItemAt is the n-th clipboard capture (pinned first, newest
// within each group, the page's own order), used by the 2×2 clipboard
// widget's second line.
func (a *shelf) clipboardItemAt(n int) (store.Item, bool) {
	items := a.clipboardHistory()
	if n < 0 || n >= len(items) {
		return store.Item{}, false
	}
	return items[n], true
}

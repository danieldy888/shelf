//go:build darwin

package main

import (
	"image"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
)

// The now-playing module's drawing lives here; nowplaying_module.go owns
// the helper's state. Two surfaces: the hover player that grows out of the
// notch (cover, title, artist, equalizer, progress and transport — the
// notch itself becomes the player, as Droppy does) and the tray tab's card
// (56pt cover, title/「artist · app」, ⏮ ⏯ ⏭, elapsed/remaining).

// Hover player geometry: at least 400pt wide (the notch +220), and
// npPlayerBody tall below the notch.
const (
	npPlayerW      = 400
	npPlayerMinX   = 220 // extra width beyond the notch
	npPlayerBody   = 140
	npPlayerPad    = 16
	npPlayerArt    = 52
	npPlayerArtR   = 11
	npPlayerCtl    = 32
	npPlayerPlay   = 40
	npPlayerCtlGap = 18
	eqBars         = 4
	eqBarW         = 3
	eqBarGap       = 2
	eqBarR         = 1.5
	eqMaxH         = 14
	eqPausedH      = 4 // 暂停时静止为低矮等高
	eqStillH       = 8 // 减少动态效果时的静止高度
)

// Card geometry (DESIGN.md「展开面板」正在播放行)。
const (
	npCardArt  = 56
	npCardArtR = 12
	npCtlSize  = 32 // 上一首/下一首
	npPlaySize = 38 // 播放/暂停
	npTrackH   = 4
	npProgRowH = 14
)

// eqPeriods are the equalizer bars' loop periods, inside the design's
// 0.9–1.4s band; spread phases keep them unrelated.
var eqPeriods = []time.Duration{1300 * time.Millisecond, 940 * time.Millisecond, 1120 * time.Millisecond, 1380 * time.Millisecond}

// Now-playing glyphs, filled where the controls are solid and stroked
// where panel symbols are.
var (
	musicSymbol = symbol(`<path d="M9 18V6l10-2v12M9 18a3 3 0 1 1-6 0 3 3 0 0 1 6 0zM19 16a3 3 0 1 1-6 0 3 3 0 0 1 6 0z"/>`)
	playFill    = fillSymbol(`<path d="M8 5.5 19 12 8 18.5z"/>`)
	pauseFill   = fillSymbol(`<rect x="6" y="5" width="4" height="14" rx="1"/><rect x="14" y="5" width="4" height="14" rx="1"/>`)
	prevFill    = fillSymbol(`<path d="M11 6v12l-8-6zM20 6v12l-8-6z"/>`)
	nextFill    = fillSymbol(`<path d="M13 6v12l8-6zM4 6v12l8-6z"/>`)
)

// showsPlayer reports whether hovering the notch shows the player rather
// than the plain bulge: a track is loaded, the setting is on and no file
// drag is asking for the notch's drop target.
func (m *nowPlayingModule) showsPlayer() bool {
	_, ok := m.track()
	return ok && m.host.settings.IslandNowPlaying && !m.host.dragging
}

// peekPlayer draws the hover player at its final size w×h; the caller
// clips it to the growing shell and passes the peek progress as k, so the
// layout never squeezes while the notch opens. The transport buttons work
// in place; a press anywhere else opens the shelf. It reports whether it
// drew anything.
func (m *nowPlayingModule) peekPlayer(c *ui.Context, w, h, k float32) bool {
	st, ok := m.track()
	if !ok || !m.showsPlayer() {
		return false
	}
	m.ctlPressed = false
	player := ui.Column(c.Key("np-peek")).Width(w).Height(h).
		Padding(0, npPlayerPad, 12, npPlayerPad).Gap(10).Opacity(min(max(k*k, 0), 1)).Label("展开 Shelf")
	player.Children(func() {
		// The camera housing sits in the top band; content starts below it.
		ui.Box(c.Key("np-peek-notch")).Height(float32(max(0, m.host.topInset-2)))
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			m.coverArt(c, "np-peek-art", npPlayerArt, npPlayerArtR)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Justify(ui.Center).Children(func() {
				ui.Text(c.Key("np-peek-title"), st.Title).SingleLine().FontSize(13).FontWeight(600).TextColor(ink)
				if meta := m.cardMeta(st); meta != "" {
					ui.Text(c.Key("np-peek-artist"), meta).SingleLine().FontSize(11).TextColor(ink2)
				}
			})
			m.eqBars(c, st.Playing)
		})
		m.progressRow(c, st, time.Now())
		ui.Row(c.Key("np-peek-ctl")).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Gap(npPlayerCtlGap).Children(func() {
			m.controlButton(c, "np-peek-prev", prevFill, "上一首", mac.NPPrevious, npPlayerCtl, 18)
			m.controlButton(c, "np-peek-toggle", toggleSymbol(st.Playing), playPauseLabel(st.Playing), mac.NPPlayPause, npPlayerPlay, 24)
			m.controlButton(c, "np-peek-next", nextFill, "下一首", mac.NPNext, npPlayerCtl, 18)
		})
	})
	if player.Clicked() && !m.ctlPressed {
		m.host.show(false)
	}
	return true
}

// eqBars draws the four equalizer bars: animated between 4 and 14pt while
// playing (each bar on its own period), a still low row while paused, and
// a still mid row when the desktop asks for less motion.
func (m *nowPlayingModule) eqBars(c *ui.Context, playing bool) {
	col := ink3
	if m.artColor.A > 0 {
		col = m.artColor
	}
	ui.Row(c.Key("np-eq")).Gap(eqBarGap).AlignItems(ui.Center).Height(eqMaxH).Children(func() {
		for i := range eqPeriods {
			bar := ui.Box(c.Key(i)).Width(eqBarW).Radius(eqBarR).Background(col).AlignSelf(ui.End)
			h := float32(eqPausedH)
			switch {
			case playing && !c.Preferences().ReduceMotion:
				k := bar.Loop("eq", eqPeriods[i], ui.Bounce(ui.EaseInOut))
				// 与循环进度混合一个固定相位，让四根条不在同一起伏。
				wave := math.Sin(float64(float32(i)*0.37+k) * math.Pi)
				h = 4 + float32(wave*wave)*float32(eqMaxH-4)
			case playing:
				h = eqStillH
			}
			bar.Height(h)
		}
	})
}

// card draws the now-playing card at the top of the expanded panel's
// tray tab: 56pt cover with shadow, title, 「artist · app」, transport
// controls and the progress track timed from the last helper line. It
// reports whether it drew anything.
func (m *nowPlayingModule) card(c *ui.Context) bool {
	st, ok := m.track()
	if !ok {
		return false
	}
	now := time.Now()
	ui.Column(c.Key("nowplaying")).Gap(10).Margin(panelCardTop, 0, 0, 0).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			m.cardArt(c)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Justify(ui.Center).Children(func() {
				ui.Text(c.Key("np-title"), st.Title).SingleLine().FontSize(14).FontWeight(600).TextColor(ink)
				if meta := m.cardMeta(st); meta != "" {
					ui.Text(c.Key("np-artist"), meta).SingleLine().FontSize(12).TextColor(ink2)
				}
			})
			ui.Row(c.Key("np-controls")).Gap(4).AlignItems(ui.Center).Children(func() {
				m.controlButton(c, "np-prev", prevFill, "上一首", mac.NPPrevious, npCtlSize, 18)
				m.controlButton(c, "np-toggle", toggleSymbol(st.Playing), playPauseLabel(st.Playing), mac.NPPlayPause, npPlaySize, 22)
				m.controlButton(c, "np-next", nextFill, "下一首", mac.NPNext, npCtlSize, 18)
			})
		})
		m.progressRow(c, st, now)
	})
	return true
}

// cardArt is the card's 56pt cover.
func (m *nowPlayingModule) cardArt(c *ui.Context) {
	m.coverArt(c, "np-art", npCardArt, npCardArtR)
}

// coverArt draws the artwork rounded and shadowed, the app icon as-is when
// no cover exists, or a music glyph.
func (m *nowPlayingModule) coverArt(c *ui.Context, key string, size, radius float32) {
	switch {
	case m.art != nil && !m.artIsIcon:
		ui.Image(c.Key(key), m.art).Size(size, size).Radius(radius).Fit(ui.Cover).
			Shadow(0, 6, 18, 0, ui.RGBA(0, 0, 0, 0.5))
	case m.art != nil:
		// The app icon already carries its own rounded shape.
		ui.Image(c.Key(key), m.art).Size(size, size)
	default:
		ui.Row(c.Key(key+"-icon")).Size(size, size).Radius(radius).Center().Background(fill1).Children(func() {
			ui.Icon(c, musicSymbol).Size(size*0.43, size*0.43).TextColor(ink3)
		})
	}
}

// cardMeta is the card's second line 「歌手 · 应用名」, either part alone
// when the other is unknown.
func (m *nowPlayingModule) cardMeta(st mac.NowPlayingState) string {
	app := ""
	if st.App != "" {
		app = m.host.appName(st.App)
	}
	switch {
	case st.Artist != "" && app != "":
		return st.Artist + " · " + app
	case app != "":
		return app
	default:
		return st.Artist
	}
}

// progressRow is the card's progress line: elapsed m:ss on the left, the
// 4pt white-on-fill3 track, remaining -m:ss on the right.
func (m *nowPlayingModule) progressRow(c *ui.Context, st mac.NowPlayingState, now time.Time) {
	ui.Row(c.Key("np-progress")).Gap(8).AlignItems(ui.Center).Height(npProgRowH).Children(func() {
		elapsed := st.Position(now)
		ui.Text(c, mac.FormatClock(elapsed)).SingleLine().FontSize(10).TextColor(ink3)
		track := ui.Row(c.Key("np-bar")).Grow(1).MinWidth(0).Height(npTrackH).Radius(2).Clip().Background(fill3)
		track.Children(func() {
			frac := float32(0.34) // unknown length: a quiet static bar
			if st.Duration > 0 {
				frac = float32(min(max(elapsed/st.Duration, 0), 1))
			}
			ui.Box(c).WidthPercent(frac * 100).FillHeight().Radius(2).Background(progress)
		})
		if st.Duration > 0 {
			// Counting down, as the streaming player shows it.
			ui.Text(c, "-"+mac.FormatClock(st.Duration-elapsed)).SingleLine().FontSize(10).TextColor(ink3)
		}
	})
}

// controlButton is one transport button: a white glyph, no fill, fill2
// while hovered; size 32 for prev/next, 38 for play/pause.
func (m *nowPlayingModule) controlButton(c *ui.Context, key string, icon *ui.SVG, label string, command mac.NowPlayingCommand, size, glyph float32) {
	b := ui.ButtonBase(c.Key(key)).Size(size, size).Center().Radius(size / 2).Label(label)
	if b.Hovered() {
		b.Background(fill2)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(glyph, glyph).TextColor(ink)
	})
	if b.Clicked() {
		m.ctlPressed = true
		m.send(command)
	}
}

// toggleSymbol is the play/pause glyph matching the current state.
func toggleSymbol(playing bool) *ui.SVG {
	if playing {
		return pauseFill
	}
	return playFill
}

// playPauseLabel names the action the button will take.
func playPauseLabel(playing bool) string {
	if playing {
		return "暂停"
	}
	return "播放"
}

// rememberArt stores a freshly decoded cover (or app icon) along with its
// average colour (the equalizer bars' tint), computed once per artwork,
// never per frame. img is the decoded source; isIcon says the picture is
// the app's icon rather than album art.
func (m *nowPlayingModule) rememberArt(img image.Image, isIcon bool) {
	m.art = ui.NewBitmap(img)
	m.artIsIcon = isIcon
	m.artColor = averageColor(img)
}

// forgetArt drops the current track's picture and colour; a new ArtID
// starts art-less until its cover decodes.
func (m *nowPlayingModule) forgetArt() {
	m.art, m.artIsIcon = nil, false
	m.artColor = ui.Color{}
}

// averageColor is the mean of the image's opaque pixels, premultiplied;
// a fully transparent image reports zero.
func averageColor(img image.Image) ui.Color {
	b := img.Bounds()
	var r, g, bl, n uint64
	step := max(1, min(b.Dx(), b.Dy())/24)
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			cr, cg, cb, ca := img.At(x, y).RGBA()
			if ca == 0 {
				continue
			}
			// 未乘 alpha 的通道平均，半透明像素按其不透明度加权。
			r += uint64(cr) * uint64(ca)
			g += uint64(cg) * uint64(ca)
			bl += uint64(cb) * uint64(ca)
			n += uint64(ca)
		}
	}
	if n == 0 {
		return ui.Color{}
	}
	return ui.Color{
		R: uint8(r / n >> 8),
		G: uint8(g / n >> 8),
		B: uint8(bl / n >> 8),
		A: 255,
	}
}

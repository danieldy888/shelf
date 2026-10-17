//go:build darwin

package main

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
)

// nowPlayingModule shows what the system is playing. The closed notch stays
// bare while music plays: no wings, matching Droppy's demo. Hovering the
// notch peeks a compact player (artwork, title/artist, play/pause), and the
// expanded panel carries the full card (artwork, title/artist, ⏮ ⏯ ⏭,
// elapsed and remaining time).
//
// The state comes from mac.NowPlayingHost, which reads MediaRemote
// through an Apple-signed perl host (macOS 15.4 and later deny
// Apple-unsigned readers). The host is started lazily while the
// islandNowPlaying setting is on and stopped when it is turned off, so
// an off switch costs no process at all. The same setting covers every
// now-playing surface here (peek player and panel card).
//
// Artwork arrives in its own MediaRemote notification, often a moment
// after the title. The helper therefore waits a short grace window before
// declaring it missing; the module decodes whatever later line carries it,
// and falls back to the playing app's icon when a track never publishes
// artwork at all.
type nowPlayingModule struct {
	host *shelf

	np      *mac.NowPlayingHost
	started bool
	retryAt time.Time // next start attempt after a failure

	// newHost builds the helper host; nil means the real Apple-signed perl
	// host. A seam for tests.
	newHost func() *mac.NowPlayingHost

	// last is the state the module displays; only a fully parsed helper
	// line updates it.
	last mac.NowPlayingState
	have bool

	// snap is the last state the UI was invalidated for; comparing new
	// states against it keeps the redraw rate tied to visible changes.
	snap      npSnap
	art       *ui.Bitmap // artwork or app icon of the current track
	artIsIcon bool       // art is the app icon fallback, not the cover
	artDone   string     // artID decoded once (success or undecodable blob)
	iconFor   string     // artID whose app-icon fallback was looked up

	// icons caches app icons by bundle id: the fallback is looked up once
	// per app, not once per track.
	icons map[string]*ui.Bitmap

	// newIcon resolves a bundle id's icon; nil means mac.AppIcon. A seam
	// for tests, which must not start AppKit.
	newIcon func(bundle string) *ui.Bitmap

	// peekBtn is the on-screen box of the peek player's play/pause button,
	// written while the peek pill is drawn. The native mouse-down observer
	// reads it so a press there toggles the track instead of expanding the
	// panel.
	peekBtn mygo.Rectangle

	// A play/pause press answers at once. Until the helper agrees (or
	// npOptimisticWindow passes), a contradicting line is treated as stale:
	// the app has not caught up yet, and the button must not flip back.
	optUntil   time.Time
	optPlaying bool
}

// npOptimisticWindow is how long a press outranks contradicting helper
// lines. It is a ceiling for slow apps, not the normal path: a helper line
// that agrees clears the override at once.
const npOptimisticWindow = 1500 * time.Millisecond

// npSnap is the part of a Now Playing state the UI cares about.
type npSnap struct {
	have     bool
	playing  bool
	title    string
	artist   string
	artID    string
	position int // whole seconds, so a playing track redraws once a second
	art      int // 0 none, 1 real cover, 2 app icon fallback
}

// npSnapOf is the pure projection from a state to the compared snapshot.
// Position counts only while the expanded panel can show it: with the
// panel closed a once-a-second redraw would wake the compositor for a
// card no one sees.
func npSnapOf(st mac.NowPlayingState, have, panelVisible bool, now time.Time) npSnap {
	if !have {
		return npSnap{}
	}
	position := 0
	if panelVisible {
		position = int(st.Position(now))
	}
	return npSnap{
		have:     true,
		playing:  st.Playing,
		title:    st.Title,
		artist:   st.Artist,
		artID:    st.ArtID,
		position: position,
	}
}

// npRetryDelay is how long the module waits before starting the helper
// again after a failed attempt (perl missing, bundle unreadable).
const npRetryDelay = 30 * time.Second

// npPeekWidth is the collapsed-hover player's width: room for the cover,
// a readable title, the playing indicator and the play/pause button. The
// resident strip is at least this wide so every part of the pill, not just
// the hardware notch, can receive the press.
const npPeekWidth = 336

// npArtNone, npArtCover and npArtIcon classify what the card shows.
const (
	npArtNone = iota
	npArtCover
	npArtIcon
)

// Now-playing control glyphs, in the same 24×24 stroke style as the
// panel's own symbols.
var (
	musicSymbol  = symbol(`<path d="M9 18V6l10-2v12M9 18a3 3 0 1 1-6 0 3 3 0 0 1 6 0zM19 16a3 3 0 1 1-6 0 3 3 0 0 1 6 0z"/>`)
	playSymbol   = symbol(`<path d="M8 5.5 19 12 8 18.5z"/>`)
	pauseSymbol2 = symbol(`<path d="M9 5v14M15 5v14"/>`)
	prevSymbol   = symbol(`<path d="M18 6v12L9 12zM6 6v12"/>`)
	nextSymbol   = symbol(`<path d="M6 6v12l9-6zM18 6v12"/>`)
	// eqSymbol is the compact "playing" mark beside the peek title: four
	// bars, the same idea as Droppy's waveform, without pretending to be a
	// live spectrum we cannot read.
	eqSymbol = symbol(`<path d="M5 14v-4M10 18V6M15 16V8M20 14v-4"/>`)
)

// tick keeps the helper alive according to the setting and folds new
// helper lines into the displayed state.
func (m *nowPlayingModule) tick(now time.Time) {
	if !m.host.settings.IslandNowPlaying {
		m.stop()
		return
	}
	if !m.started {
		if !m.retryAt.IsZero() && now.Before(m.retryAt) {
			return
		}
		host := m.spawnHost()
		if err := host.Start(); err != nil {
			// perl or the bundle is missing: stay quiet and retry later
			// rather than logging on every 30 ms tick.
			m.retryAt = now.Add(npRetryDelay)
			return
		}
		m.np, m.started = host, true
		m.retryAt = time.Time{}
	} else if m.np.Dead() {
		// The helper exited (perl gone, a pipe broke). Drop the stale
		// state; a later tick starts a fresh host.
		m.np.Stop()
		m.np, m.started = nil, false
		m.retryAt = now.Add(npRetryDelay)
		m.last, m.have = mac.NowPlayingState{}, false
		m.art, m.artIsIcon, m.artDone, m.iconFor = nil, false, "", ""
		m.optUntil = time.Time{}
		m.invalidate(npSnap{})
		return
	}
	m.refresh(now)
}

// spawnHost builds the helper host through the test seam when one is set.
func (m *nowPlayingModule) spawnHost() *mac.NowPlayingHost {
	if m.newHost != nil {
		return m.newHost()
	}
	return mac.NewNowPlayingHost()
}

// refresh pulls the latest state from the host and invalidates the
// window when what is shown changed (track, playing state, artwork, or
// the once-a-second moving position).
func (m *nowPlayingModule) refresh(now time.Time) {
	st, have := m.np.State()
	if have && !m.optUntil.IsZero() {
		switch {
		case st.Playing == m.optPlaying:
			// The helper caught up: the press is no longer an assumption.
			m.optUntil = time.Time{}
		case now.Before(m.optUntil):
			// The app has not processed the command yet (or emitted a stale
			// line): keep the user's answer and its clock behaviour.
			st.Playing = m.optPlaying
			if st.Playing {
				if st.Rate <= 0 {
					st.Rate = 1
				}
			} else {
				st.Rate = 0
			}
		default:
			m.optUntil = time.Time{}
		}
	}
	if have && st.ArtID != m.last.ArtID {
		// A new track starts without last track's picture: the card must
		// not keep showing stale art while the new one is being read.
		m.art, m.artIsIcon = nil, false
	}
	m.last, m.have = st, have
	if have {
		m.decodeArt(st)
	}
	next := npSnapOf(st, have, m.host.visible, now)
	if have {
		next.art = m.artState()
	}
	m.invalidate(next)
}

// invalidate marks snap as current and redraws when it differs from what
// the UI was last told.
func (m *nowPlayingModule) invalidate(next npSnap) {
	if next == m.snap {
		return
	}
	m.snap = next
	if m.host.win != nil {
		m.host.win.Invalidate()
	}
}

// artState classifies what the card shows, so swapping between a cover,
// an app icon and nothing invalidates the window even when the track id
// is unchanged.
func (m *nowPlayingModule) artState() int {
	switch {
	case m.art != nil && m.artIsIcon:
		return npArtIcon
	case m.art != nil:
		return npArtCover
	}
	return npArtNone
}

// decodeArt decodes the artwork of a newly seen track once, on the main
// thread (the view's thread), and drops the previous track's bitmap. A
// late line that finally carries the cover replaces an earlier app-icon
// fallback for the same track.
func (m *nowPlayingModule) decodeArt(st mac.NowPlayingState) {
	if st.ArtID == "" || st.ArtID == m.artDone {
		return
	}
	if len(st.Art) > 0 {
		m.artDone = st.ArtID
		m.art, m.artIsIcon = nil, false
		if b, err := ui.DecodeBitmap(st.Art); err == nil {
			m.art = b
		}
		return
	}
	if !st.ArtMissing || st.App == "" || m.iconFor == st.ArtID {
		// The helper has not given up on the artwork yet, this line cannot
		// name an app to draw, or the fallback is already in place. A later
		// line naming the bundle must still get its chance.
		return
	}
	m.iconFor = st.ArtID
	m.art, m.artIsIcon = m.icon(st.App), true
}

// icon resolves an app icon through the test seam when one is set.
func (m *nowPlayingModule) icon(bundle string) *ui.Bitmap {
	if m.newIcon != nil {
		return m.newIcon(bundle)
	}
	return m.appIcon(bundle)
}

// appIcon returns the playing app's icon as a decoded bitmap, or nil. The
// answer is cached per bundle id: NetEase and other apps that never
// publish artwork still identify themselves, so the card shows a real
// app icon instead of a generic music glyph.
func (m *nowPlayingModule) appIcon(bundle string) *ui.Bitmap {
	if bundle == "" {
		return nil
	}
	if m.icons == nil {
		m.icons = make(map[string]*ui.Bitmap)
	}
	if b, ok := m.icons[bundle]; ok {
		return b
	}
	var b *ui.Bitmap
	if png := mac.AppIcon(bundle, 64); len(png) > 0 {
		if decoded, err := ui.DecodeBitmap(png); err == nil {
			b = decoded
		}
	}
	m.icons[bundle] = b
	return b
}

// stop ends the helper and forgets its state; called when the setting is
// off, so a disabled feature costs no process at all.
func (m *nowPlayingModule) stop() {
	if m.np != nil {
		m.np.Stop()
		m.np = nil
	}
	m.started = false
	m.retryAt = time.Time{}
	m.last, m.have = mac.NowPlayingState{}, false
	m.art, m.artIsIcon, m.artDone, m.iconFor = nil, false, "", ""
	m.optUntil = time.Time{}
	m.invalidate(npSnap{})
}

// track returns the current state while it names something the card can
// show, playing or paused (the controls are most useful while paused).
func (m *nowPlayingModule) track() (mac.NowPlayingState, bool) {
	if !m.have || m.last.Title == "" {
		return mac.NowPlayingState{}, false
	}
	return m.last, true
}

// hasTrack reports whether a now-playing card may be shown: the setting is
// on and the source names a track, paused or not.
func (m *nowPlayingModule) hasTrack() bool {
	if !m.host.settings.IslandNowPlaying {
		return false
	}
	_, ok := m.track()
	return ok
}

// playing reports whether something is playing right now.
func (m *nowPlayingModule) playing() bool {
	return m.have && m.last.Playing
}

// peekPlayer draws the compact player in the peeking notch pill: artwork,
// title and artist, the playing mark and a play/pause button. It reports
// whether it drew anything; a drag in flight keeps the drop hint instead.
func (m *nowPlayingModule) peekPlayer(c *ui.Context) bool {
	st, ok := m.track()
	if !ok || !m.host.settings.IslandNowPlaying || m.host.dragging {
		m.peekBtn = mygo.Rectangle{}
		return false
	}
	shellW, shellH := m.host.shellSize(0)
	x0 := float32(m.host.stripCenter()) - shellW/2
	y0 := float32(m.host.y)
	t := c.Theme()
	const (
		pad     = float32(10)
		artSize = float32(34)
		btnSize = float32(28)
	)
	m.peekBtn = mygo.Rectangle{
		X:      int(x0 + shellW - pad - btnSize),
		Y:      int(y0 + (shellH-btnSize)/2),
		Width:  int(btnSize),
		Height: int(btnSize),
	}
	ui.Row(c.Key("np-peek")).Width(shellW).Height(shellH).
		Padding(0, pad, 0, pad).AlignItems(ui.Center).Gap(8).Children(func() {
		if m.art != nil {
			img := ui.Image(c.Key("np-peek-art"), m.art).Size(artSize, artSize)
			if !m.artIsIcon {
				img.Radius(7).Fit(ui.Cover)
			}
		} else {
			ui.Icon(c.Key("np-peek-glyph"), musicSymbol).Size(artSize, artSize).TextColor(textMuted)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Justify(ui.Center).Children(func() {
			ui.Text(c.Key("np-peek-title"), st.Title).SingleLine().FontSize(12).FontWeight(600).TextColor(textPrimary)
			if st.Artist != "" {
				ui.Text(c.Key("np-peek-artist"), st.Artist).SingleLine().FontSize(10).TextColor(textMuted)
			}
		})
		eq := t.Accent
		if !st.Playing {
			eq = textMuted
		}
		ui.Icon(c.Key("np-peek-eq"), eqSymbol).Size(16, 16).TextColor(eq)
		m.controlButton(c, "np-peek-toggle", toggleSymbol(st.Playing), playPauseLabel(st.Playing), mac.NPPlayPause, btnSize)
	})
	return true
}

// overPeekButton reports whether a screen point lands on the peek player's
// play/pause button; a native press there must reach the button rather than
// expanding the panel.
func (m *nowPlayingModule) overPeekButton(p mygo.Point) bool {
	r := m.peekBtn
	return r.Width > 0 && r.Height > 0 &&
		p.X >= r.X && p.X < r.X+r.Width &&
		p.Y >= r.Y && p.Y < r.Y+r.Height
}

// card draws the now-playing card at the top of the expanded panel's
// tray tab: artwork, title and artist, transport controls and a progress
// bar timed from the last helper line. It reports whether it drew
// anything.
func (m *nowPlayingModule) card(c *ui.Context) bool {
	st, ok := m.track()
	if !ok {
		return false
	}
	now := time.Now()
	ui.Column(c.Key("nowplaying")).Gap(8).Padding(10).Radius(8).
		Background(cardSurface).Children(func() {
		ui.Row(c).Gap(10).Children(func() {
			switch {
			case m.art != nil && !m.artIsIcon:
				ui.Image(c.Key("np-art"), m.art).Size(48, 48).Radius(6).Fit(ui.Cover)
			case m.art != nil:
				// The app icon already carries its own rounded shape.
				ui.Image(c.Key("np-art"), m.art).Size(48, 48)
			default:
				ui.Icon(c.Key("np-art-icon"), musicSymbol).Size(40, 40).TextColor(textMuted)
			}
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Justify(ui.Center).Children(func() {
				ui.Text(c.Key("np-title"), st.Title).SingleLine().FontSize(13).FontWeight(600).TextColor(textPrimary)
				if st.Artist != "" {
					ui.Text(c.Key("np-artist"), st.Artist).SingleLine().FontSize(11).TextColor(textMuted)
				}
			})
			ui.Row(c.Key("np-controls")).Gap(4).Children(func() {
				m.controlButton(c, "np-prev", prevSymbol, "上一首", mac.NPPrevious, 30)
				m.controlButton(c, "np-toggle", toggleSymbol(st.Playing), playPauseLabel(st.Playing), mac.NPPlayPause, 30)
				m.controlButton(c, "np-next", nextSymbol, "下一首", mac.NPNext, 30)
			})
		})
		ui.Row(c.Key("np-progress")).Gap(8).AlignItems(ui.Center).Children(func() {
			elapsed := st.Position(now)
			ui.Text(c, mac.FormatClock(elapsed)).SingleLine().FontSize(10).TextColor(textMuted)
			value := float64(-1) // unknown length: an active bar
			if st.Duration > 0 {
				value = min(max(elapsed/st.Duration, 0), 1)
			}
			ui.Progress(c.Key("np-bar"), value).Grow(1)
			if st.Duration > 0 {
				// Counting down, as the streaming player shows it.
				ui.Text(c, "-"+mac.FormatClock(st.Duration-elapsed)).SingleLine().FontSize(10).TextColor(textMuted)
			}
		})
	})
	return true
}

// controlButton is one transport button with an accessibility label.
func (m *nowPlayingModule) controlButton(c *ui.Context, key string, icon *ui.SVG, label string, command mac.NowPlayingCommand, size float32) {
	b := ui.ButtonBase(c.Key(key)).Size(size, size).Center().Radius(size / 2).Label(label)
	if b.Hovered() {
		b.Background(cardHover)
	} else {
		b.Background(cardSurface)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(size/2, size/2).TextColor(textPrimary)
	})
	if b.Clicked() {
		m.send(command)
	}
}

// send forwards a transport command to the playing app. Play/pause flips
// the shown state at once — the next helper line (the helper samples hot
// for a moment after a command) reconciles it with the truth, so a slow or
// missing source notification cannot leave the button on the old glyph.
func (m *nowPlayingModule) send(command mac.NowPlayingCommand) {
	if m.np == nil {
		return
	}
	m.np.Send(command)
	if command != mac.NPPlayPause || !m.have {
		return
	}
	now := time.Now()
	// Freeze or resume from where the track is now, not from the last
	// helper anchor: the button press itself must not move the clock.
	m.last.Elapsed = m.last.Position(now)
	m.last.Received = now
	m.last.Playing = !m.last.Playing
	m.optUntil, m.optPlaying = now.Add(npOptimisticWindow), m.last.Playing
	next := npSnapOf(m.last, true, m.host.visible, now)
	next.art = m.artState()
	m.invalidate(next)
}

// toggleSymbol is the play/pause glyph matching the current state.
func toggleSymbol(playing bool) *ui.SVG {
	if playing {
		return pauseSymbol2
	}
	return playSymbol
}

// playPauseLabel names the action the button will take.
func playPauseLabel(playing bool) string {
	if playing {
		return "暂停"
	}
	return "播放"
}

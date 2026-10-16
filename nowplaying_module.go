//go:build darwin

package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
)

// nowPlayingModule shows what the system is playing: a live activity
// around the closed notch (artwork left of the notch; the trailing wing
// stays empty — the source has no short value to put there) and, in the
// expanded panel, a now-playing card with artwork, title/artist and
// ⏮ ⏯ ⏭ controls.
//
// The state comes from mac.NowPlayingHost, which reads MediaRemote
// through an Apple-signed perl host (macOS 15.4 and later deny
// Apple-unsigned readers). The host is started lazily while the
// islandNowPlaying setting is on and stopped when it is turned off, so
// an off switch costs no process at all.
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
	snap     npSnap
	art      *ui.Bitmap // decoded artwork of the current track
	artTried string     // artID whose decode was attempted
}

// npSnap is the part of a Now Playing state the UI cares about.
type npSnap struct {
	have     bool
	playing  bool
	title    string
	artist   string
	artID    string
	position int // whole seconds, so a playing track redraws once a second
}

// npSnapOf is the pure projection from a state to the compared snapshot.
func npSnapOf(st mac.NowPlayingState, have bool, now time.Time) npSnap {
	if !have {
		return npSnap{}
	}
	return npSnap{
		have:     true,
		playing:  st.Playing,
		title:    st.Title,
		artist:   st.Artist,
		artID:    st.ArtID,
		position: int(st.Position(now)),
	}
}

// npRetryDelay is how long the module waits before starting the helper
// again after a failed attempt (perl missing, bundle unreadable).
const npRetryDelay = 30 * time.Second

// nowPlayingPriority sits between the low-battery warning and the
// plug/unplug flash: an ongoing track yields to battery news, while
// volume changes and charger edges outrank both.
const nowPlayingPriority = 10

// Now-playing control glyphs, in the same 24×24 stroke style as the
// panel's own symbols.
var (
	musicSymbol  = symbol(`<path d="M9 18V6l10-2v12M9 18a3 3 0 1 1-6 0 3 3 0 0 1 6 0zM19 16a3 3 0 1 1-6 0 3 3 0 0 1 6 0z"/>`)
	playSymbol   = symbol(`<path d="M8 5.5 19 12 8 18.5z"/>`)
	pauseSymbol2 = symbol(`<path d="M9 5v14M15 5v14"/>`)
	prevSymbol   = symbol(`<path d="M18 6v12L9 12zM6 6v12"/>`)
	nextSymbol   = symbol(`<path d="M6 6v12l9-6zM18 6v12"/>`)
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
	m.last, m.have = st, have
	if have {
		m.decodeArt(st)
	}
	m.invalidate(npSnapOf(st, have, now))
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

// decodeArt decodes the artwork of a newly seen track once, on the main
// thread (the view's thread), and drops the previous track's bitmap. A
// track whose artwork failed to decode is never retried.
func (m *nowPlayingModule) decodeArt(st mac.NowPlayingState) {
	if st.ArtID == "" || st.ArtID == m.artTried {
		return
	}
	if len(st.Art) == 0 {
		// The helper decides an artwork is missing for good; until then a
		// later line may still carry it.
		if st.ArtMissing {
			m.artTried = st.ArtID
			m.art = nil
		}
		return
	}
	m.artTried = st.ArtID
	m.art = nil
	if b, err := ui.DecodeBitmap(st.Art); err == nil {
		m.art = b
	}
}

// stop ends the helper and forgets its state; called when the setting is
// off, so a disabled island costs no process at all.
func (m *nowPlayingModule) stop() {
	if m.np != nil {
		m.np.Stop()
		m.np = nil
	}
	m.started = false
	m.retryAt = time.Time{}
	m.last, m.have = mac.NowPlayingState{}, false
	m.art, m.artTried = nil, ""
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

// playing reports whether something is playing right now; the island
// only shows while this holds.
func (m *nowPlayingModule) playing() bool {
	return m.have && m.last.Playing
}

// activity feeds the notch island: artwork (or a music glyph) left of
// the notch, nothing on the right. It answers only while something is
// playing and the setting is on.
func (m *nowPlayingModule) activity(_ time.Time) (activity, bool) {
	if !m.host.settings.IslandNowPlaying || !m.playing() {
		return activity{}, false
	}
	act := activity{
		ID:       "nowplaying",
		Priority: nowPlayingPriority,
		Level:    -1, // no bar: now playing has no level to show
	}
	if m.art != nil {
		act.Leading = m.art
	} else {
		act.Leading = musicSymbol
		act.LeadingTint = textPrimary
	}
	return act, true
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
			if m.art != nil {
				ui.Image(c.Key("np-art"), m.art).Size(44, 44).Radius(6)
			} else {
				ui.Icon(c.Key("np-art-icon"), musicSymbol).Size(40, 40).TextColor(textMuted)
			}
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Justify(ui.Center).Children(func() {
				ui.Text(c.Key("np-title"), st.Title).SingleLine().FontSize(13).FontWeight(600).TextColor(textPrimary)
				if st.Artist != "" {
					ui.Text(c.Key("np-artist"), st.Artist).SingleLine().FontSize(11).TextColor(textMuted)
				}
			})
			ui.Row(c.Key("np-controls")).Gap(4).Children(func() {
				m.controlButton(c, "np-prev", prevSymbol, "上一首", mac.NPPrevious)
				m.controlButton(c, "np-toggle", toggleSymbol(st.Playing), playPauseLabel(st.Playing), mac.NPPlayPause)
				m.controlButton(c, "np-next", nextSymbol, "下一首", mac.NPNext)
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
				ui.Text(c, mac.FormatClock(st.Duration)).SingleLine().FontSize(10).TextColor(textMuted)
			}
		})
	})
	return true
}

// controlButton is one transport button with an accessibility label.
func (m *nowPlayingModule) controlButton(c *ui.Context, key string, icon *ui.SVG, label string, command mac.NowPlayingCommand) {
	b := ui.ButtonBase(c.Key(key)).Size(30, 30).Center().Radius(15).Label(label)
	if b.Hovered() {
		b.Background(cardHover)
	} else {
		b.Background(cardSurface)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(15, 15).TextColor(textPrimary)
	})
	if b.Clicked() {
		m.send(command)
	}
}

// send forwards a transport command to the playing app. The helper will
// report the new state through its next line; the command itself can
// fail silently when nothing is playing.
func (m *nowPlayingModule) send(command mac.NowPlayingCommand) {
	if m.np == nil {
		return
	}
	m.np.Send(command)
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

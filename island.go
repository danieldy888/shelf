//go:build darwin

package main

import (
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The notch island: while the shelf is closed, a live activity widens the
// black notch shell into Dynamic-Island-style wings — a leading glyph left
// of the hardware notch, trailing text and/or a level bar right of it.
// The shell Path is the same one peek and expand interpolate; the wings
// fold away as either of those states takes over.
//
// Animation: islandK runs 0 → 1 as the wings grow out of the notch and
// back to 0 as they fold away. A new activity id first folds the wings
// shut (islandSwap), swaps the shown content, then grows them back — the
// Dynamic Island's "morph" between sources.

// islandAnimation is one fold of the wings, in or out.
const islandAnimation = 160 * time.Millisecond

// islandWingW is how far each wing reaches past the hardware notch: room
// for the glyph and a short value, about a notch-height of content plus
// padding. The idle strip is always this wide so the wings never resize
// the resident window.
func (a *shelf) islandWingW() float32 {
	return float32(max(32, a.notchH)) + 16
}

// islandStripW is the strip width that always fits the island fully open.
func (a *shelf) islandStripW() int {
	return max(180, a.notchW) + 2*int(a.islandWingW()+0.5)
}

// islandRect is the on-screen box of the drawn island shell, matching
// shellSize(0) in view(): clicks on it expand the shelf.
func (a *shelf) islandRect() mygo.Rectangle {
	w, _ := a.shellSize(0)
	return mygo.Rectangle{X: a.stripCenter() - int(w/2), Y: a.y, Width: int(w + 0.5), Height: max(32, a.notchH)}
}

// overIsland reports whether a screen point lands on the drawn island.
func (a *shelf) overIsland(p mygo.Point) bool {
	r := a.islandRect()
	return r.Width > 0 && insideRect(p, r)
}

// hasIsland reports whether a live activity is driving the island (versus
// only the fold-out tail of one that just ended).
func (a *shelf) hasIsland() bool { return a.island.ID != "" }

// islandShown reports whether island pixels are on screen at all.
func (a *shelf) islandShown() bool { return a.hasIsland() || a.islandK > 0.01 }

// idleBand is the pixel region of the resident strip that receives events
// while collapsed: exactly the drawn shell — the bare notch, or the notch
// plus its wings. Everything around it passes through to the menu bar.
func (a *shelf) idleBand() (w, h int) {
	sw, _ := a.shellSize(0)
	return int(sw + 0.5), max(8, int(max(32, a.notchH)))
}

// peekSize is the peek pill's resting size. While a track is loaded it
// widens to fit the compact player (cover, title, playing mark, play/pause);
// otherwise it is the small "click to expand" capsule. The idle strip is
// always wide enough for both, so a track coming or going never resizes
// the resident window.
func (a *shelf) peekSize() (w, h float32) {
	baseW := float32(max(180, a.notchW))
	baseH := float32(max(32, a.notchH))
	w = baseW + 56
	if a.np != nil && a.np.hasTrack() {
		w = max(w, npPeekWidth)
	}
	return w, baseH + 18
}

// shellSize is the one interpolation the shell Path, the idle hit-test
// band and the island layout all share: hardware notch → (wings) → peek
// pill → expanded panel. openK is the expand progress the caller uses
// (view() substitutes 1 for headless tests).
func (a *shelf) shellSize(openK float32) (w, h float32) {
	baseW := float32(max(180, a.notchW))
	baseH := float32(max(32, a.notchH))
	peekW, peekH := a.peekSize()
	expW, expH := float32(panelWidth-16), float32(a.panelH()-8)
	w0 := baseW + (peekW-baseW)*a.peekProgress
	h0 := baseH + (peekH-baseH)*a.peekProgress
	// The wings fold back into the shell as it peeks or expands.
	w0 += 2 * a.islandWingW() * a.islandK * (1 - a.peekProgress) * (1 - openK)
	return w0 + (expW-w0)*openK, h0 + (expH-h0)*openK
}

// islandContentK is how visible the glyph/text/bar are: they fade in a
// little ahead of the wings and out with any fold.
func (a *shelf) islandContentK(openK float32) float32 {
	k := a.islandK * (1 - a.peekProgress) * (1 - openK) * 1.5
	return min(max(k, 0), 1)
}

// islandTick runs on every monitoring tick, after the modules: it asks for
// the current activity, invalidates the window when the shown activity
// changes, and re-arms the wing animation on appear/change/disappear.
func (a *shelf) islandTick(now time.Time) {
	act, live := currentActivity(a.modules, now)
	switch {
	case a.islandSwap:
		// Mid-swap the wings are folding shut; keep the outgoing content
		// and let the animation swap it in when the fold completes.
	case live && a.hasIsland() && act.ID == a.island.ID:
		if act != a.island {
			a.island = act
			if a.win != nil {
				a.win.Invalidate()
			}
		}
	case live && act.ID != a.island.ID:
		a.islandNext = act
		if a.hasIsland() {
			// A new source: fold the old wings shut, swap, grow back.
			a.islandSwap = true
		} else {
			a.island = act
		}
		a.islandFrom, a.islandStart = a.islandK, now
		if a.win != nil {
			a.win.Invalidate()
		}
	case !live && a.hasIsland():
		// The source stopped: fold the wings shut, then forget it.
		a.islandNext = activity{}
		a.islandSwap = true
		a.islandFrom, a.islandStart = a.islandK, now
		if a.win != nil {
			a.win.Invalidate()
		}
	}
}

// islandTextW is the trailing string's drawn width at size. The answer is
// cached per (text, size) — a live activity measures once, not per frame.
func (a *shelf) islandTextW(c *ui.Context, text string, size float32) float32 {
	if text == "" {
		return 0
	}
	if a.islandFitSrc == text && a.islandFitSize == size {
		return a.islandFitW
	}
	w, _ := a.islandMeasure(c, text, size)
	a.islandFitSrc, a.islandFitSize, a.islandFitW = text, size, w
	return w
}

// islandMeasure is the one place the island lays text out for measurement;
// the counter exists so tests can prove a repainted frame re-measures
// nothing.
func (a *shelf) islandMeasure(c *ui.Context, text string, size float32) (float32, float32) {
	a.islandMeasures++
	return c.MeasureText(0, ui.Span{Text: text, Size: size})
}

// islandFit clips trailing text to avail points with an ellipsis, measured
// the same way it is drawn; the answer is cached per (text, size, avail)
// so a live activity does not re-measure every frame.
func (a *shelf) islandFit(c *ui.Context, text string, size, avail float32) string {
	if text == "" {
		return ""
	}
	if a.islandFitIn == text && a.islandFitInSize == size && a.islandFitAvail == avail {
		return a.islandFitText
	}
	out := ""
	if a.islandTextW(c, text, size) <= avail {
		out = text
	} else if w, _ := a.islandMeasure(c, "…", size); w <= avail {
		// Binary-search the longest prefix that fits with an ellipsis.
		lo, hi := 0, utf8.RuneCountInString(text)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			cand := string([]rune(text)[:mid]) + "…"
			if w, _ := a.islandMeasure(c, cand, size); w <= avail {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		out = string([]rune(text)[:lo]) + "…"
	}
	a.islandFitIn, a.islandFitInSize, a.islandFitAvail, a.islandFitText = text, size, avail, out
	return out
}

// islandView draws the wings' content while the shelf is collapsed: the
// leading glyph (rounded artwork or a tinted icon), the notch spacer, and
// trailing text and/or a level bar. It is called inside the shell's
// column; the wings' black shell behind it is the shared shell Path.
func (a *shelf) islandView(c *ui.Context, shellW float32) {
	baseH := float32(max(32, a.notchH))
	wing := a.islandWingW()
	glyph := baseH - 8
	act := a.island
	contentK := a.islandContentK(0)

	row := ui.Row(c.Key("island")).Width(shellW).Height(baseH).Clip().AlignItems(ui.Center)
	row.Children(func() {
		ui.Box(c.Key("isle-l")).Width(wing).Height(baseH).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			if act.Leading != nil {
				img := ui.Image(c, act.Leading).Size(glyph, glyph).Radius(6).Fit(ui.Cover)
				if act.LeadingTint != (ui.Color{}) {
					img.TextColor(act.LeadingTint)
				}
			}
		})
		ui.Box(c).Width(max(shellW-2*wing, 0))
		ui.Box(c.Key("isle-r")).Width(wing).Height(baseH).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			bar := act.Level >= 0
			trailMax := wing - 8
			textW := a.islandTextW(c, act.Trailing, 12)
			barW := float32(0)
			if bar {
				barW = min(20, trailMax)
				if textW > 0 {
					room := trailMax - 4 - textW
					if room < 6 {
						// No room for both: keep the value readable and
						// drop the bar instead of truncating it.
						barW = 0
					} else {
						barW = min(barW, room)
					}
				}
			}
			textAvail := trailMax - barW
			if barW > 0 && textW > 0 {
				textAvail -= 4 // gap between text and bar
			}
			inner := ui.Row(c).AlignItems(ui.Center).Gap(4)
			inner.Children(func() {
				if s := a.islandFit(c, act.Trailing, 12, textAvail); s != "" {
					ui.Text(c, s).SingleLine().FontSize(12).TextColor(textPrimary)
				}
				if bar {
					level := min(max(act.Level, 0), 1)
					ui.Box(c.Key("isle-bar")).Width(barW).Height(4).Radius(2).Clip().Background(cardSurface).Children(func() {
						ui.Box(c).Width(barW * level).Height(4).Radius(2).Background(textPrimary)
					})
				}
			})
		})
	})
	row.Opacity(contentK)
}

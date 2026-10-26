//go:build darwin

package main

import (
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
)

func (p *toolsPage) dragSpec(id string) layout.Card {
	for _, spec := range p.host.toolsLayout().Cards {
		if spec.ID == id {
			return spec
		}
	}
	spec := layout.Card{ID: id, Span: 1}
	if def := toolCardDefFor(id); def != nil {
		spec.Span = def.span
	}
	return spec
}

// dragPreview is the transient window a dragged card follows the pointer
// in once its 1.06× copy would leave the page's content box: every page
// draws that copy itself, inside a Clip(), so it disappears at the edge.
// The preview is not a pin and never enters layout.json; a drop outside the
// panel hands its window to the committed pin instead of opening another.
type dragPreview struct{ it *pinItem }

// window is the preview's pin item, nil while no preview is up.
func (p *dragPreview) window() *pinItem {
	if p == nil {
		return nil
	}
	return p.it
}

// close destroys the preview window now: a drag that returns to the page,
// or one that is cancelled, must not leave the copy on the desktop.
func (p *dragPreview) close() {
	if it := p.window(); it != nil {
		p.it = nil
		if it.win != nil {
			it.win.Hide() // remove immediately; native close can animate out
			it.win.Destroy()
			it.win = nil
		}
	}
}

// take hands the window over to a committed pin.
func (p *dragPreview) take() *pinItem {
	it := p.window()
	p.it = nil
	return it
}

// syncDragPreview shows, moves, or closes the preview window for a card of
// span columns and cardW pixels grabbed at grabDX/grabDY. inside means the
// copy still fits the page's content box, where the page draws it and the
// window must stay out of the way. Both pages share it: the tools grid and
// the home canvas differ only in where their content box is.
func (a *shelf) syncDragPreview(pv *dragPreview, id string, span, cardW int, grabDX, grabDY, srcW, srcH float32, cursor mygo.Point, inside bool) {
	if inside || a.winH == 0 {
		pv.close()
		return
	}
	display := displayIDAt(cursor, 0)
	frame := pinClamp(pinFrameUnder(cursor, grabDX, grabDY, srcW, srcH, cardW), pinArea(display))
	if pv.it == nil {
		pv.it = &pinItem{owner: a, cardID: id, span: span, cardW: cardW,
			display: display, edge: pinFree, expanded: true, frame: frame}
		pinOpenPreview(pv.it)
		return
	}
	pv.it.display, pv.it.frame = display, frame
	pv.it.applyFrame()
}

func (p *toolsPage) closeDragPreview() { p.preview.close() }

// Events carry the pointer's actual location. A cursor poll can lag the
// event during fast direction changes, especially while a window is moving.
func (p *toolsPage) toolPointer(ev ui.InputEvent) mygo.Point {
	d := p.press
	if d == nil {
		return mygo.Point{}
	}
	// The captured parked card has no visual transform. Its animated
	// content origin plus the viewport origin maps the event to screen.
	x := p.viewport.X + d.cardX + ev.X
	y := p.viewport.Y + d.cardY + ev.Y - p.scroll.Y
	return mygo.Point{
		X: p.host.frame.X + int(math.Round(float64(x))),
		Y: p.host.frame.Y + int(math.Round(float64(y))),
	}
}

// The original card retains pointer capture. The preview is a non-activating,
// mouse-transparent window, not a live pin and not yet a persisted placement.
// Switch before any part of the lifted card reaches the scroll clip, rather
// than waiting for the cursor itself to leave the panel.
func (p *toolsPage) syncDragPreview(cursor mygo.Point) {
	a, d := p.host, p.press
	if d == nil || !d.dragging || a.winH == 0 {
		p.preview.close()
		return
	}
	spec := p.dragSpec(d.id)
	w, h := toolColW(spec.Span), float32(toolCellH)
	left := float32(cursor.X) - d.grabDX - w*0.03
	top := float32(cursor.Y) - d.grabDY - h*0.03
	v := p.viewport
	vx, vy := float32(a.frame.X)+v.X, float32(a.frame.Y)+v.Y
	inside := v.W > 0 && left >= vx && top >= vy && left+w*1.06 <= vx+v.W && top+h*1.06 <= vy+v.H
	cardW := pinCardW
	if spec.Span == 2 {
		cardW = pinCardW2
	}
	a.syncDragPreview(&p.preview, d.id, spec.Span, cardW, d.grabDX, d.grabDY, w, h, cursor, inside)
}

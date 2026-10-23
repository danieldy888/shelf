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

func (p *toolsPage) closeDragPreview() {
	if it := p.preview; it != nil {
		p.preview = nil
		if it.win != nil {
			it.win.Hide() // remove immediately; native close can animate out
			it.win.Destroy()
			it.win = nil
		}
	}
}

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
		p.closeDragPreview()
		return
	}
	spec := p.dragSpec(d.id)
	w := toolColW(spec.Span)
	left := float32(cursor.X) - d.grabDX - w*0.03
	top := float32(cursor.Y) - d.grabDY - toolCellH*0.03
	v := p.viewport
	vx, vy := float32(a.frame.X)+v.X, float32(a.frame.Y)+v.Y
	if v.W > 0 && left >= vx && top >= vy && left+w*1.06 <= vx+v.W && top+toolCellH*1.06 <= vy+v.H {
		p.closeDragPreview()
		return
	}
	display := displayIDAt(cursor, 0)
	cardW := pinCardW
	if spec.Span == 2 {
		cardW = pinCardW2
	}
	frame := pinClamp(mygo.Rectangle{
		X: cursor.X - int(math.Round(float64(d.grabDX))), Y: cursor.Y - int(math.Round(float64(d.grabDY))),
		Width: cardW, Height: pinCardH,
	}, pinArea(display))
	if p.preview == nil {
		p.preview = &pinItem{owner: a, cardID: d.id, span: spec.Span, cardW: cardW,
			display: display, edge: pinFree, expanded: true, frame: frame}
		p.preview.openWindow(true)
		return
	}
	p.preview.display, p.preview.frame = display, frame
	p.preview.applyFrame()
}

//go:build darwin

package main

// Pure grid math for the tools page: pack the card list into 4 columns,
// find the insertion index a dragged card's pointer means, and build the
// virtual list a drop previews. Split from tools_page.go so tests can
// exercise it without a view.

import (
	"math"

	"shelf/internal/layout"
)

// toolColW is a card's pixel width for its column span.
func toolColW(span int) float32 {
	if span >= 2 {
		return 2*(toolStageW-3*toolCellGap)/4 + toolCellGap
	}
	return (toolStageW - 3*toolCellGap) / 4
}

// packToolCards lays cards into 4 columns, greedy left to right, top to
// bottom, each card on the leftmost columns of the next row that fits
// its span (spans never share a row beyond their own width: a 2-span
// card takes two columns). editing appends the dashed + slot.
func packToolCards(cards []layout.Card, editing bool) []toolCell {
	var cells []toolCell
	colW := toolColW(1)
	x, y := float32(0), float32(0)
	col := 0 // columns used on the current row
	for _, card := range cards {
		span := card.Span
		if span != 2 {
			span = 1
		}
		if col+span > toolCols {
			x, col = 0, 0
			y += toolCellH + toolCellGap
		}
		cells = append(cells, toolCell{id: card.ID, x: x, y: y, w: toolColW(span)})
		x += colW*float32(span) + toolCellGap*float32(span)
		col += span
	}
	if editing {
		if col+1 > toolCols {
			x, y = 0, y+toolCellH+toolCellGap
		}
		cells = append(cells, toolCell{plus: true, x: x, y: y, w: colW})
	}
	return cells
}

// toolVirtualCards is the list the grid would have if the dragged card
// dropped at index now: cards minus it, with it re-inserted there. The
// parked copy keeps its own span.
func toolVirtualCards(cards []layout.Card, dragID string, index int) []layout.Card {
	var (
		rest  []layout.Card
		drag  layout.Card
		found bool
	)
	for _, card := range cards {
		if card.ID == dragID {
			drag, found = card, true
			continue
		}
		rest = append(rest, card)
	}
	if !found {
		return cards
	}
	index = max(0, min(index, len(rest)))
	out := make([]layout.Card, 0, len(rest)+1)
	out = append(out, rest[:index]...)
	out = append(out, drag)
	return append(out, rest[index:]...)
}

// toolInsertIndex is where the pointer means to drop the dragged card:
// the index inside cards (without it) whose packed cell's centre is
// nearest, in row-major scan order. It returns -1 when the drag id is
// unknown. px, py are stage content coordinates.
//
// current is the index last reported; it wins ties so the target does
// not flicker between two cells while the pointer hovers a boundary.
func toolInsertIndex(cards []layout.Card, dragID string, current int, px, py float32) int {
	var drag layout.Card
	rest := cards[:0:0]
	for _, card := range cards {
		if card.ID == dragID {
			drag = card
			continue
		}
		rest = append(rest, card)
	}
	if drag.ID == "" {
		return -1
	}
	best, bestD := -1, float64(math.MaxFloat64)
	for i := 0; i <= len(rest); i++ {
		cells := packToolCards(toolVirtualCards(cards, dragID, i), false)
		for _, cell := range cells {
			if cell.id != dragID {
				continue
			}
			dx := px - (cell.x + cell.w/2)
			dy := py - (cell.y + toolCellH/2)
			d := float64(dx*dx + dy*dy)
			if d < bestD || (d == bestD && i == current) {
				best, bestD = i, d
			}
		}
	}
	return best
}

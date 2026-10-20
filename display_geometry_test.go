//go:build darwin

package main

import (
	"testing"

	"github.com/egoist/mygo"
)

// Multi-display geometry audit: all shelf rects live in one global space —
// CG display coordinates, Y down from the primary screen's top-left corner —
// so secondary displays show up as non-zero or negative origins. These tests
// replay place()'s results for synthetic layouts and pin down that the strip,
// the hover/drag triggers, the peek pill and the drag-guard corridor all stay
// anchored to the shelf's own display.

// secondaryShelfBelow reproduces place() on an external main display
// (1440 tall, origin 0,0) with the notched built-in below it.
func secondaryShelfBelow() *shelf {
	return &shelf{
		dispBounds: mygo.Rectangle{X: 0, Y: 1440, Width: 1470, Height: 956},
		x:          (1470 - panelWidth) / 2,
		y:          1440,
		h:          400,
		notchX:     646, notchY: 1440, notchW: 180, notchH: 32,
		hasNotch: true,
	}
}

// secondaryShelfNegative reproduces place() on a panel arranged left of and
// above the primary, giving both coordinates negative origins.
func secondaryShelfNegative() *shelf {
	return &shelf{
		dispBounds: mygo.Rectangle{X: -1920, Y: -1080, Width: 1920, Height: 1080},
		x:          -1920 + (1920-panelWidth)/2,
		y:          -1080,
		h:          400,
		notchX:     -1920 + (1920-180)/2, notchY: -1080, notchW: 180, notchH: 32,
		hasNotch: false, // external panels have no camera housing: fallback box
	}
}

func TestSecondaryDisplayKeepsStripAtItsOwnTop(t *testing.T) {
	for name, a := range map[string]*shelf{
		"built-in below external main": secondaryShelfBelow(),
		"external left+above primary":  secondaryShelfNegative(),
	} {
		t.Run(name, func(t *testing.T) {
			strip := a.desiredFrame()
			if strip.Y != a.dispBounds.Y {
				t.Fatalf("strip top %d left the display's top edge %d", strip.Y, a.dispBounds.Y)
			}
			if strip.Y+strip.Height > a.dispBounds.Y+a.dispBounds.Height {
				t.Fatalf("strip %+v escapes display %+v", strip, a.dispBounds)
			}
			cx := a.stripCenter()
			if cx != a.notchX+a.notchW/2 {
				t.Fatalf("strip center %d not on notch center %d", cx, a.notchX+a.notchW/2)
			}
			if strip.X != cx-strip.Width/2 {
				t.Fatalf("strip %+v not centred on notch", strip)
			}
			a.visible = true
			panel := a.desiredFrame()
			if panel.Y != a.dispBounds.Y || panel.Width != panelWidth+2*earR || panel.Height != a.panelH() {
				t.Fatalf("expanded panel %+v lost the display-top anchor", panel)
			}
			if panel.X+panel.Width/2 != cx {
				t.Fatalf("expanded panel %+v not centred on notch %d", panel, cx)
			}
		})
	}
}

func TestSecondaryDisplayTriggersStayOnTheirDisplay(t *testing.T) {
	a := secondaryShelfBelow()
	// Points inside the secondary display's own top zone hit.
	if !a.overNotch(mygo.Point{X: 736, Y: 1440 + 20}) {
		t.Fatal("hover inside the secondary display's notch band ignored")
	}
	if !a.nearTrigger(mygo.Point{X: 736, Y: 1440 + 40}) {
		t.Fatal("drag trigger inside the secondary display's top ignored")
	}
	// The same local coordinates on the PRIMARY display must not fire —
	// this is exactly where an unflipped Y would ghost-trigger.
	if a.overNotch(mygo.Point{X: 736, Y: 20}) {
		t.Fatal("primary-screen point hovered a notch 1440pt away")
	}
	if a.nearTrigger(mygo.Point{X: 736, Y: 40}) {
		t.Fatal("primary-screen point armed a trigger 1440pt away")
	}
	// Just above the secondary display's top edge is the primary's bottom.
	if a.nearTrigger(mygo.Point{X: 736, Y: 1439}) {
		t.Fatal("point on the other display armed the trigger")
	}
	if a.overNotch(mygo.Point{X: 736, Y: 1439}) {
		t.Fatal("point on the other display hovered the notch")
	}
}

func TestNegativeCoordinateDisplayTriggers(t *testing.T) {
	a := secondaryShelfNegative()
	cx := a.notchX + a.notchW/2 // -1050
	if !a.nearTrigger(mygo.Point{X: cx, Y: -1080}) {
		t.Fatal("cursor on the display's top edge must arm the drag trigger")
	}
	if !a.overNotch(mygo.Point{X: cx, Y: -1080 + 16}) {
		t.Fatal("cursor in the notch band must hover")
	}
	// The primary display occupies Y≥0 and the display ends at Y=0; nothing
	// outside its bounds may touch this shelf's triggers.
	for _, p := range []mygo.Point{{X: cx, Y: 0}, {X: cx, Y: -1081}, {X: 500, Y: -1050}} {
		if a.nearTrigger(p) || a.overNotch(p) {
			t.Fatalf("point %+v outside the display fired a trigger", p)
		}
	}
}

func TestSecondaryDisplayPeekPillFollowsNotch(t *testing.T) {
	a := secondaryShelfNegative()
	pill := a.peekPillRect()
	cx := a.notchX + a.notchW/2
	if pill.Y != a.y || pill.X != cx-pill.Width/2 {
		t.Fatalf("peek pill %+v not anchored under the notch at (%d,%d)", pill, cx, a.y)
	}
	if !a.overPeekPill(mygo.Point{X: cx, Y: a.y + 10}) {
		t.Fatal("pill click point rejected on a negative-Y display")
	}
	if a.overPeekPill(mygo.Point{X: cx, Y: 10}) {
		t.Fatal("pill must not exist on the primary display's top strip")
	}
}

// The drag-guard corridor must reproduce the drop strip in the same global
// space CGEvent locations use — including negative origins — so the filter
// and the pin actually cover the secondary display's top edge.
func TestDragGuardRegionOnSecondaryDisplays(t *testing.T) {
	for name, a := range map[string]*shelf{
		"built-in below external main": secondaryShelfBelow(),
		"external left+above primary":  secondaryShelfNegative(),
	} {
		t.Run(name, func(t *testing.T) {
			strip := a.desiredFrame()
			g := a.dragGuardRegion()
			if g.Y != strip.Y || g.X != a.stripCenter()-notchTriggerWidth/2 {
				t.Fatalf("guard %+v not centred on the strip %+v", g, strip)
			}
			if g.Y+g.Height > strip.Y+strip.Height {
				t.Fatalf("guard landing below the drop strip: %+v vs %+v", g, strip)
			}
			if g.Height <= 38 {
				t.Fatalf("guard height %d ends inside the menu bar; Spaces edge would fire", g.Height)
			}
		})
	}
}

// insideRect gates every "cursor left the shelf's display" decision in
// monitorDrag/pointerDragged; it must treat negative origins literally.
func TestInsideRectOnNegativeDisplayBounds(t *testing.T) {
	r := mygo.Rectangle{X: -1920, Y: -1080, Width: 1920, Height: 1080}
	for _, tt := range []struct {
		p    mygo.Point
		want bool
	}{
		{mygo.Point{X: -1920, Y: -1080}, true},  // top-left corner inclusive
		{mygo.Point{X: -1, Y: -1}, true},        // bottom-right inside edge
		{mygo.Point{X: 0, Y: -1}, false},        // just past the right edge
		{mygo.Point{X: -1, Y: 0}, false},        // just below the bottom edge
		{mygo.Point{X: -1921, Y: -1080}, false}, // just left of it
		{mygo.Point{X: 100, Y: 100}, false},     // primary display interior
	} {
		if got := insideRect(tt.p, r); got != tt.want {
			t.Fatalf("insideRect(%v, %+v) = %v, want %v", tt.p, r, got, tt.want)
		}
	}
}

// A click on the secondary display's own menu-bar strip counts as menu bar
// (never collapses the shelf); a click lower on that display still dismisses.
func TestMouseDownMenuBarRuleOnSecondaryDisplay(t *testing.T) {
	a := secondaryShelfBelow()
	a.visible = true
	bar := mygo.Point{X: 200, Y: a.notchY + 10}
	a.mouseDown(false, bar)
	if !a.visible {
		t.Fatal("click inside the secondary display's menu-bar strip collapsed the shelf")
	}
	a.mouseDown(false, mygo.Point{X: 200, Y: a.notchY + 200})
	if a.visible {
		t.Fatal("click on the desktop area below the menu bar must collapse the shelf")
	}
}

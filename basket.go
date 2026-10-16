//go:build darwin

package main

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
)

// Shake-to-summon basket: while a system file drag is in flight, wiggling
// the pointer left-right summons a small floating card next to the cursor
// that accepts the drop. The shake math lives in shake.go; this file owns
// the window, its placement and its lifetime.

const (
	basketWidth    = 220
	basketHeight   = 160
	basketGap      = 18 // 光标与篮子之间留出的安全距离
	basketPad      = 8  // 篮子离可见屏缘的内边距
	basketEdge     = 10 // 卡片四周的透明余量
	basketAfterEnd = 3 * time.Second
)

// basketState is the floating drop card. The window is created lazily on
// the first shake and destroyed on close, so Shelf without a shake costs
// no extra overlay.
type basketState struct {
	win       *mygo.Window
	frame     mygo.Rectangle // last applied screen rect
	dropped   bool           // content arrived; showing the confirmation
	endAt     time.Time      // the drag session ended at
	leftSince time.Time      // pointer left the card after the drag ended
	hideAt    time.Time      // confirmation deadline after a drop
}

// basketFrame picks the card's on-screen rect: beside the cursor, clamped
// inside the display's visible frame so it can never sit under the menu
// bar or off-screen. The card prefers down-right of the cursor, flips to
// the left when there is no room, and above when the bottom edge would
// leave the visible area.
func basketFrame(visible mygo.Rectangle, cursor mygo.Point) mygo.Rectangle {
	x := cursor.X + basketGap
	y := cursor.Y + basketGap
	if x+basketWidth > visible.X+visible.Width-basketPad {
		x = cursor.X - basketGap - basketWidth
	}
	if y+basketHeight > visible.Y+visible.Height-basketPad {
		y = cursor.Y - basketGap - basketHeight
	}
	x = min(max(x, visible.X+basketPad), max(visible.X+basketPad, visible.X+visible.Width-basketPad-basketWidth))
	y = min(max(y, visible.Y+basketPad), max(visible.Y+basketPad, visible.Y+visible.Height-basketPad-basketHeight))
	return mygo.Rectangle{X: x, Y: y, Width: basketWidth, Height: basketHeight}
}

// showBasket summons the card next to the cursor. Repeated calls while it
// is open do nothing. A haptic tick marks the moment it appears, and the
// card rides above other windows like the shelf panel.
func (a *shelf) showBasket(p mygo.Point) {
	if a.basket.win != nil || a.quitting {
		return
	}
	d := mygo.Screen.DisplayNearestPoint(p)
	a.basket.frame = basketFrame(d.WorkArea, p)
	a.basket.leftSince, a.basket.endAt, a.basket.hideAt = time.Time{}, time.Time{}, time.Time{}
	a.basket.dropped = false
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: appName + " 篮子", Width: basketWidth, Height: basketHeight,
		X: a.basket.frame.X, Y: a.basket.frame.Y,
		Hidden: true, Frameless: true, Transparent: true, DisableShadow: true,
		AlwaysOnTop: true, SkipTaskbar: true, Panel: true,
		DisableMove: true, DisableResize: true, DisableMinimize: true, DisableMaximize: true, DisableFullScreen: true,
		Content: ui.View(a.basketView),
	})
	a.basket.win = w
	w.SetVisibleOnAllWorkspaces(true)
	mac.ConfigureNotchWindow(w.NativeHandle())
	mac.SetWindowFrame(w.NativeHandle(), a.basket.frame.X, a.basket.frame.Y, a.basket.frame.Width, a.basket.frame.Height)
	w.OnClose(func(e *mygo.CloseEvent) {
		// A system close (Space switch, Exposé tidy-up) dismisses the card.
		a.basket = basketState{}
	})
	w.ShowInactive()
	mac.SetDragElevated(w.NativeHandle(), true)
	mac.HapticTick()
}

// closeBasket tears the card down. Idempotent.
func (a *shelf) closeBasket() {
	w := a.basket.win
	a.basket = basketState{}
	if w != nil {
		w.Destroy()
	}
}

// basketTick drives the card's lifetime from the 30 ms monitor: it closes
// once the drag ends (after a short linger while the pointer still rests
// on it), about a second after the pointer leaves a lingering card, and at
// the confirmation deadline after content landed.
func (a *shelf) basketTick(now time.Time, p mygo.Point) {
	b := &a.basket
	if b.win == nil {
		return
	}
	if !b.hideAt.IsZero() {
		if !now.Before(b.hideAt) {
			a.closeBasket()
		}
		return
	}
	if a.dragging {
		b.endAt, b.leftSince = time.Time{}, time.Time{}
		return
	}
	if b.endAt.IsZero() {
		b.endAt = now
	}
	if !insideRect(p, b.frame) {
		if !b.dropped {
			a.closeBasket()
			return
		}
		if b.leftSince.IsZero() {
			b.leftSince = now
		}
		if now.Sub(b.leftSince) >= time.Second {
			a.closeBasket()
		}
		return
	}
	b.leftSince = time.Time{}
	if !b.dropped && now.Sub(b.endAt) >= basketAfterEnd {
		a.closeBasket()
	}
}

// basketReceive is the card's drop path: the same intake as the notch,
// then a brief confirmation before the card closes.
func (a *shelf) basketReceive(data transfer.Data) {
	a.receive(data)
	b := &a.basket
	b.dropped = true
	b.hideAt = time.Now().Add(900 * time.Millisecond)
	if b.win != nil {
		b.win.Invalidate()
	}
}

// basketView draws the card: a dark rounded panel with the shelf icon and
// 放到这里, highlighted while a drag is over it, briefly confirming after
// a drop. The outer padding leaves a transparent margin around the card.
func (a *shelf) basketView(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	opts := transfer.DropOptions{Formats: []transfer.Format{transfer.FileList, transfer.URIList, transfer.PNG, transfer.Text}, Operations: transfer.Copy}
	zone := ui.Column(c.Key("basket")).Fill().Padding(basketEdge).Clip()
	_, over := ui.DataDragOver(zone, opts)
	zone.Children(func() {
		card := ui.Column(c.Key("card")).Fill().Radius(20).Center().Gap(12)
		if a.basket.dropped || over {
			card.Background(ui.Hex("#000000")).Border(2, c.Theme().Accent)
		} else {
			card.Background(ui.Hex("#000000")).Border(2, hairline)
		}
		card.Children(func() {
			ui.Icon(c, shelfSymbol).Size(40, 40).TextColor(c.Theme().Accent)
			label := "放到这里"
			if a.basket.dropped {
				label = "已放入 Shelf"
			}
			ui.Text(c, label).FontSize(14).FontWeight(600).TextColor(textPrimary)
		})
	})
	if drop, ok := ui.DropData(zone, opts); ok {
		a.basketReceive(drop.Data)
	}
}

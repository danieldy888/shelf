//go:build darwin

package main

import (
	"log"
	"time"

	"github.com/egoist/mygo"

	"shelf/internal/mac"
)

// Incoming system drags: detection, opening the shelf near the notch, and the
// protection that keeps the Spaces bar from opening at the top edge.

// dragGuard owns the current filter, including a failed setup attempt so that
// the same error is not retried on every monitoring tick. A failure while
// untrusted is retried once Accessibility is granted, without a restart.
type dragGuard struct {
	stop      func()
	region    mac.DragGuardRegion
	attempted bool
	trusted   bool // Accessibility state at the last attempt
}

// dragGuardRegion is the notch corridor the event filter clamps, in the
// same coordinates as the resident drop strip.
func (a *shelf) dragGuardRegion() mac.DragGuardRegion {
	strip := a.desiredFrame()
	// Land below the menu bar's exclusive strip, still inside the drop
	// surface. A 40pt corridor on a 38pt notch sits inside the menu bar
	// and Spaces still fires.
	h := min(max(a.notchH+16, 48), strip.Height-1)
	if h < 8 {
		h = 8
	}
	return mac.DragGuardRegion{
		X: a.stripCenter() - notchTriggerWidth/2, Y: a.y,
		Width: notchTriggerWidth, Height: h,
	}
}

// Protect the notch's approach corridor, including its centre. The lower edge
// is below the camera housing but still inside the shelf's drag trigger.
// The event filter (Accessibility only) is armed once and stays up for file
// drags and window moves alike; the cursor pin needs no permission and runs
// only while a file drag is in progress.
func (a *shelf) syncDragGuard() {
	if a.win == nil {
		a.stopDragGuard()
		return
	}
	region := a.dragGuardRegion()
	mac.SetDragPin(region, a.dragging && !a.quitting)
	if a.guard.stop != nil {
		if a.guard.region != region {
			mac.SetDragGuardRegion(region)
			a.guard.region = region
		}
		return
	}
	if a.guard.attempted && (a.guard.trusted || !mac.AccessibilityTrusted()) {
		return
	}
	stop, err := mac.StartDragGuard(region)
	a.guard = dragGuard{stop: stop, region: region, attempted: true, trusted: mac.AccessibilityTrusted()}
	if err != nil {
		log.Printf("刘海拖拽保护未启用：%v", err)
	}
}

func (a *shelf) stopDragGuard() {
	mac.SetDragPin(mac.DragGuardRegion{}, false)
	if a.guard.stop != nil {
		a.guard.stop()
	}
	a.guard = dragGuard{}
}

// pointerDragged is the NSEvent mouse-dragged callback. Finder has already
// declared the drag pasteboard by the first pixel of movement; waiting for
// the 30 ms poll lets the cursor reach Y=0 first.
func (a *shelf) pointerDragged() {
	if a.quitting {
		return
	}
	active, serial := mac.DragState()
	if serial != a.dragSerial {
		a.dragSerial = serial
		a.suppressed = false
	}
	if !active {
		return
	}
	a.dragging, a.pointerDrag = true, true
	p := mygo.Screen.CursorScreenPoint()
	if !a.visible && !insideRect(p, a.dispBounds) {
		a.place(p)
	}
	if !a.suppressed {
		a.setPeek(true)
	}
	a.syncDragGuard()
	a.syncMousePassThrough()
	if !a.suppressed && !a.visible && a.nearTrigger(p) {
		// 拖入首次触及刘海触发区时给一下力反馈，提示落点在顶部。
		mac.HapticTick()
		a.show(true)
	}
}

// monitorDrag updates placement, drag protection and interaction on the main thread.
func (a *shelf) monitorDrag(now time.Time) {
	active, serial := mac.DragState()
	if serial != a.dragSerial {
		a.dragSerial = serial
		a.suppressed = false
	}
	a.dragging = active
	if !active {
		a.suppressed = false
		if !mac.LeftMouseDown() {
			a.pointerDrag = false
		}
	}
	p := mygo.Screen.CursorScreenPoint()
	if !a.visible && !insideRect(p, a.dispBounds) {
		a.place(p)
	}
	// 摇晃召唤：只在系统拖拽进行中、且不是自己拖出或已放弃拖放的会话里
	// 采样；竖直拖动没有 x 采样变化，不会误触发篮子。
	if active && !a.suppressed && !a.ownDrag {
		if a.shake.add(now, float64(p.X)) {
			a.showBasket(p)
		}
	} else {
		a.shake.reset()
	}
	overNotch := a.overNotch(p)
	nearEdge := a.nearTrigger(p)
	// Dragging or hovering reveals the pill; only a click or a real incoming
	// drag in the trigger zone can open the full panel.
	if active || overNotch {
		if !a.suppressed || overNotch {
			a.setPeek(true)
		}
	}
	a.syncDragGuard()
	a.syncMousePassThrough()
	if active && !a.suppressed && !a.visible && nearEdge {
		// Incoming drags open immediately; empty-handed hover never does.
		mac.HapticTick()
		a.show(true)
	}
	if !active && !a.visible && !overNotch {
		a.setPeek(false)
	}
	// 自动收起：仅拖拽唤出的托盘在未收到内容、光标离开面板与触发区
	// edgeLeaveDelay 后缩回刘海；点击探头/托盘唤出的托盘由用户显式收起。
	// 动画途中不判离开。
	if a.visible && a.edgeOpened && !a.received {
		inside := p.X >= a.x-24 && p.X < a.x+panelWidth+24 && p.Y >= a.y && p.Y < a.y+a.panelH()+24
		if !inside && !nearEdge && a.animationStart.IsZero() {
			if a.leftSince.IsZero() {
				a.leftSince = now
			}
			if now.Sub(a.leftSince) >= edgeLeaveDelay {
				a.hide()
			}
		} else {
			a.leftSince = time.Time{}
		}
	} else {
		a.leftSince = time.Time{}
	}

	a.basketTick(now, p)
}
func insideRect(p mygo.Point, r mygo.Rectangle) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
}

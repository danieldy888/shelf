//go:build darwin

package main

import (
	"errors"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"testing"
	"time"
)

func TestUnarmedToastReadInPanelDoesNotReplay(t *testing.T) {
	for _, hover := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "hovered"}[hover], func(t *testing.T) {
			a := motionWindow()
			a.peeking = hover
			view := ui.NewTester(a.view, panelWidth, panelHeight)
			a.toastStatus("已转换 3 项", time.Now().Add(8*time.Second), statusOK)
			if hover {
				view.Frame()
			}
			if a.fb.armed {
				t.Fatal("fixture already displayed the capsule")
			}
			a.peeking = false
			a.visible, a.progress = true, 1
			view.Frame()
			a.statusShown = time.Now().Add(-2 * time.Second)
			a.hide(hideExplicit)
			a.progress = 0
			view.Frame()
			view.Advance(900 * time.Millisecond)
			view.Frame()
			if a.fb.text != "" || a.fb.drawn {
				t.Fatalf("read toast replayed: %q", a.fb.text)
			}
		})
	}
}

func TestDeliveredCollapsedMarginClickDoesNotDisappear(t *testing.T) {
	for _, capsule := range []bool{false, true} {
		a := motionShelf()
		a.peeking = !capsule
		a.fb.drawn = capsule
		a.fx = shellMotion{w: 240, h: 54}
		if capsule {
			a.toastStatus("已复制", time.Now().Add(3*time.Second), statusOK)
			a.fb.drawn = true
		}
		r := a.peekPillRect()
		a.mouseDown(true, mygo.Point{X: r.X - 5, Y: a.y + 40})
		if !a.visible {
			t.Fatalf("delivered margin click was swallowed (capsule=%v)", capsule)
		}
	}
}

func TestDownloadingCapsuleIsBriefButPanelKeepsDeadline(t *testing.T) {
	for _, opened := range []bool{false, true} {
		t.Run(map[bool]string{false: "closed", true: "handoff"}[opened], func(t *testing.T) {
			stubUpdater(t)
			a := updateShelf()
			a.visible = opened
			up := &mygo.Update{Version: "1.2.3"}
			a.update = up
			a.installUpdate()
			if time.Until(a.statusUntil) < 9*time.Minute {
				t.Fatal("panel deadline was shortened")
			}
			if opened {
				a.hide(hideExplicit)
			}
			a.fb.arm(time.Now())
			if got := a.fb.until.Sub(a.fb.shownAt); got > 4*time.Second {
				t.Fatalf("download capsule holds %v", got)
			}
			for _, err := range []error{nil, errors.New("offline")} {
				a.installedUpdate(up, err)
				a.fb.arm(time.Now())
				if a.fb.hold < 7*time.Second {
					t.Fatal("terminal update result inherited the progress limit")
				}
				if err != nil && a.fb.color() != fbRed {
					t.Fatal("update failure lost its error colour")
				}
			}
		})
	}
}

//go:build darwin

package mac

import (
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// overlayHits is the idle-strip hit rule, in a flipped view whose Y=0 is the
// screen's top edge. Only the bandW × bandH region centered on the strip is
// solid while idle: it follows the drawn notch shell (bare notch, or the
// widened island wings), so file drags at Y=0 hit a window instead of the
// Spaces edge while menu-bar items beside the drawn shell keep their clicks.
// bandW <= 0 means the full strip width.
func overlayHits(acceptAll bool, bandW, bandH int, localX, localY, viewW, viewH float64) bool {
	if acceptAll {
		return true
	}
	if bandH <= 0 || viewH <= 0 {
		return false
	}
	band := float64(bandH)
	if band > viewH {
		band = viewH
	}
	if localY < 0 || localY >= band {
		return false
	}
	w := float64(bandW)
	if w <= 0 || w > viewW {
		w = viewW
	}
	x0 := (viewW - w) / 2
	return localX >= x0 && localX < x0+w
}

var (
	overlayHitOnce  sync.Once
	overlayHitIMP   uintptr
	overlayAccept   atomic.Bool
	overlayBandW    atomic.Int64
	overlayBandH    atomic.Int64
	msgConvertPoint func(obj id, sel objc.SEL, p NSPoint, view id) NSPoint
)

func overlayHitTest(self id, _ objc.SEL, px, py float64) id {
	b := msgRect(self, sel("bounds"))
	var localX, localY float64
	if msgConvertPoint != nil {
		superview := send(self, "superview")
		local := msgConvertPoint(self, sel("convertPoint:fromView:"), NSPoint{X: px, Y: py}, superview)
		localX, localY = local.X, local.Y
	} else {
		localX, localY = px, py
	}
	if overlayHits(overlayAccept.Load(), int(overlayBandW.Load()), int(overlayBandH.Load()), localX, localY, b.Size.Width, b.Size.Height) {
		return self
	}
	return 0
}

func overlaySurfaceView(window id) id {
	content := send(window, "contentView")
	if content == 0 {
		return 0
	}
	subs := send(content, "subviews")
	if sendInt(subs, "count") == 0 {
		return 0
	}
	view := send(subs, "objectAtIndex:", 0)
	cls := id(objc.GetClass("MyGoSurfaceView"))
	if cls == 0 || send(view, "isKindOfClass:", uintptr(cls)) == 0 {
		return 0
	}
	return view
}

func installOverlayHitTest(window id) {
	overlayHitOnce.Do(func() {
		load()
		purego.RegisterFunc(&msgConvertPoint, msgSendAddr)
		overlayHitIMP = purego.NewCallback(overlayHitTest)
	})
	view := overlaySurfaceView(window)
	if view == 0 || overlayHitIMP == 0 {
		return
	}
	classReplaceMethod(uintptr(send(view, "class")), sel("hitTest:"), overlayHitIMP, "@@:{CGPoint=dd}")
}

func registerOverlayDropTypes(window id) {
	arr := send(class("NSMutableArray"), "arrayWithCapacity:", 8)
	for _, t := range []string{
		"public.file-url",
		"NSFilenamesPboardType",
		"public.url",
		"public.utf8-plain-text",
		"public.png",
		"public.tiff",
		"public.html",
		"public.utf16-plain-text",
	} {
		send(arr, "addObject:", uintptr(nsString(t)))
	}
	register := func(v id) {
		if v != 0 {
			send(v, "registerForDraggedTypes:", uintptr(arr))
		}
	}
	content := send(window, "contentView")
	register(content)
	register(overlaySurfaceView(window))
}

// SetOverlayBand moves only the idle strip's solid region. The island's
// width animation tracks it frame by frame without re-installing the
// hit-test method or re-registering drop types. Main thread only.
func SetOverlayBand(bandWidth, bandHeight int) {
	if bandHeight < 1 {
		bandHeight = 32
	}
	overlayBandW.Store(int64(bandWidth))
	overlayBandH.Store(int64(bandHeight))
}

// SetOverlayHitTest keeps the overlay in WindowServer's hit-test list (never
// ignoresMouseEvents) and decides which pixels of the flipped surface are
// solid. acceptAll is peeking, expanded, or an in-flight drag; otherwise
// only the bandWidth × bandHeight region centered at the top of the strip
// receives events — the drawn notch shell — so a file dragged onto the
// camera housing hits a drop target instead of the Spaces edge and clicks
// on the island's wings land on the shelf, while the strip around the shell
// passes through to the menu bar. Main thread only.
func SetOverlayHitTest(window uintptr, acceptAll bool, bandWidth, bandHeight int) {
	SetOverlayBand(bandWidth, bandHeight)
	overlayAccept.Store(acceptAll)
	if window == 0 {
		return
	}
	if err := Init(); err != nil {
		return
	}
	withPool(func() {
		w := id(window)
		installOverlayHitTest(w)
		registerOverlayDropTypes(w)
		if send(w, "ignoresMouseEvents") != 0 {
			send(w, "setIgnoresMouseEvents:", 0)
			send(w, "orderFrontRegardless")
		}
	})
}

const (
	// StatusWindowLevel is the notch overlay's resting level
	// (NSStatusWindowLevel): above the menu bar so the shelf can occupy
	// the screen's top edge.
	StatusWindowLevel = 25

	// PopUpMenuWindowLevel is NSPopUpMenuWindowLevel, used while a file
	// drag needs the overlay above the menu bar's exclusive strip.
	PopUpMenuWindowLevel = 101

	// SearchKeyboardLevel is where the overlay sits while the clipboard
	// search has the keyboard (shelf.focusMode). Input-method candidate
	// panels are drawn by the input method's own process at
	// kCGDockWindowLevel (20 — measured on 簡體中文輸入法's cached
	// candidate window); any level above it leaves the candidate
	// completely behind the 440×400 opaque panel it is anchored inside.
	// Level 19 stays below that band yet above torn-off menus (18) and
	// floating palettes (3). The cost of dipping under the band is that
	// the menu bar (24) covers the notch-height strip at the panel's top:
	// content there is a spacer plus the title row, while the search
	// field and the list sit below it, so the keyboard flow is unaffected.
	SearchKeyboardLevel = 19
)

// SetWindowLevel puts the window at level and re-orders it frontmost of
// that band. Level changes alone do not promise topmost placement within
// the band the window joins. Main thread only.
func SetWindowLevel(window uintptr, level int) {
	if window == 0 {
		return
	}
	if err := Init(); err != nil {
		return
	}
	withPool(func() {
		w := id(window)
		if sendInt(w, "level") == level {
			return
		}
		send(w, "setLevel:", uintptr(level))
		send(w, "orderFrontRegardless")
	})
}

// WindowLevel reads the window's current NSWindow level. Main thread only.
func WindowLevel(window uintptr) int {
	if window == 0 {
		return 0
	}
	var level int
	withPool(func() {
		level = sendInt(id(window), "level")
	})
	return level
}

// SetDragElevated raises the overlay above the menu bar for the duration of
// a file drag so the menu bar's exclusive strip cannot steal the drop.
// Restores the status level afterwards. Main thread only.
func SetDragElevated(window uintptr, elevate bool) {
	level := StatusWindowLevel
	if elevate {
		level = PopUpMenuWindowLevel
	}
	SetWindowLevel(window, level)
}

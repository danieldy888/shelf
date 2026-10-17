//go:build darwin

package mac

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// cstr copies a NUL-terminated C string (class_getName's char* return).
func cstr(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	b := unsafe.Slice((*byte)(p), 4096)
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// classOrigs remembers, per patched class, the IMP a swizzle replaced, so
// the patched IMP can forward any instance it does not own. mygo hands
// every window the same few classes, so a patch installed on the class is
// visible to siblings — the only window-specific part is which instances
// the patched IMP answers for. Install and lookup both happen on the main
// thread (all callers run on it; AppKit's own dispatch is main-thread).
var classOrigs = map[id]map[objc.SEL]uintptr{}

// assocKeys are the associated-object keys marking the objects the
// class-level patches answer for: assocOverlay marks the notch's surface
// view (the hitTest: band rule), assocUnconstrain the notch/basket windows
// (unclamped constrainFrameRect:). Association dies with the object, so a
// recycled address can never inherit a patch — the failure mode a bare
// stored pointer would leave once the basket window closes.
var (
	assocOverlay     byte
	assocUnconstrain byte
	assocSentinel    id
)

// markAssoc flags obj as owning its class's patched method. Policy 0
// (OBJC_ASSOCIATION_ASSIGN) stores without retaining; the sentinel is a
// retained +1 string so the pointer is permanently valid. Main thread only.
func markAssoc(obj id, key *byte) {
	if assocSentinel == 0 {
		assocSentinel = retainedString("shelf")
	}
	setAssocObject(obj, unsafe.Pointer(key), assocSentinel, 0)
}

// hasAssoc reports whether obj carries the patch ownership mark.
func hasAssoc(obj id, key *byte) bool {
	return getAssocObject(obj, unsafe.Pointer(key)) != 0
}

// classPatch replaces selName on cls with imp and returns the saved
// original IMP: the one the class resolved selName to before patching
// (inherited or its own). A repeat patch of the same class keeps the first
// original. Main thread only.
func classPatch(cls id, selName string, imp uintptr, types string) uintptr {
	key := sel(selName)
	if origs, ok := classOrigs[cls]; ok {
		if orig, ok := origs[key]; ok {
			return orig
		}
	}
	// Ask the runtime for the IMP the class resolves selName to *before*
	// patching: class_replaceMethod would add our IMP on a class that only
	// inherits the method and then report no previous one.
	orig := classGetMethodImplementation(uintptr(cls), key)
	classReplaceMethod(uintptr(cls), key, imp, types)
	if classOrigs[cls] == nil {
		classOrigs[cls] = map[objc.SEL]uintptr{}
	}
	classOrigs[cls][key] = orig
	return orig
}

// origPointFns caches the Go-callable wrapper for each saved original IMP
// so a hitTest: call does not rebuild a purego closure every event.
var origPointFns = map[uintptr]func(obj id, sel objc.SEL, p NSPoint) id{}

// origIMP looks up the IMP that answered selName on self's class before
// this package patched it, walking the superclass chain so a subclass that
// inherited the patch still finds its stock implementation. 0 means no
// ancestor was patched or the selector was never implemented.
func origIMP(self id, selName string) uintptr {
	key := sel(selName)
	for cls := id(send(self, "class")); cls != 0; cls = id(classGetSuperclass(uintptr(cls))) {
		if orig, ok := classOrigs[cls][key]; ok {
			return orig
		}
	}
	return 0
}

// callOrigPoint forwards a (self, sel, NSPoint) call to the saved original
// IMP. 0 means the class never implemented the selector; answering 0 is
// safer than re-sending, which would recurse into the patched IMP.
func callOrigPoint(self id, selName string, p NSPoint) id {
	orig := origIMP(self, selName)
	if orig == 0 {
		return 0
	}
	fn, ok := origPointFns[orig]
	if !ok {
		purego.RegisterFunc(&fn, orig)
		origPointFns[orig] = fn
	}
	return fn(self, sel(selName), p)
}

// callOrigRectScreen forwards a (self, sel, NSRect, screen) call — the
// constrainFrameRect:toScreen: signature — to the saved original IMP the
// same way. A missing original returns the rect unchanged, matching what
// the window already wanted.
func callOrigRectScreen(self id, cmd objc.SEL, fx, fy, fw, fh float64, screen id) NSRect {
	orig := origIMP(self, "constrainFrameRect:toScreen:")
	if orig == 0 {
		return NSRect{Origin: NSPoint{X: fx, Y: fy}, Size: NSSize{Width: fw, Height: fh}}
	}
	fn, ok := origRectScreenFns[orig]
	if !ok {
		purego.RegisterFunc(&fn, orig)
		origRectScreenFns[orig] = fn
	}
	return fn(self, cmd, fx, fy, fw, fh, screen)
}

// overlayHits is the idle-strip hit rule, in a flipped view whose Y=0 is the
// screen's top edge. File drags to the hardware notch must always land on a
// window — otherwise WindowServer treats Y=0 as the Spaces edge. Clicks on
// the rest of the idle strip still pass through to the app below.
func overlayHits(acceptAll bool, bandH int, localY, viewH float64) bool {
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
	return localY >= 0 && localY < band
}

var (
	overlayHitOnce  sync.Once
	overlayHitIMP   uintptr
	overlayAccept   atomic.Bool
	overlayBand     atomic.Int64
	msgConvertPoint func(obj id, sel objc.SEL, p NSPoint, view id) NSPoint
)

func overlayHitTest(self id, _ objc.SEL, px, py float64) id {
	if !hasAssoc(self, &assocOverlay) {
		// Not the overlay's view: behave exactly as the class did before
		// the patch. The class-wide method is installed once, so other
		// mygo windows keep their ordinary hit testing.
		return callOrigPoint(self, "hitTest:", NSPoint{X: px, Y: py})
	}
	b := msgRect(self, sel("bounds"))
	var localY float64
	if msgConvertPoint != nil {
		superview := send(self, "superview")
		local := msgConvertPoint(self, sel("convertPoint:fromView:"), NSPoint{X: px, Y: py}, superview)
		localY = local.Y
	} else {
		localY = py
	}
	if overlayHits(overlayAccept.Load(), int(overlayBand.Load()), localY, b.Size.Height) {
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
	// The band rule is per window even though hitTest: sits on the shared
	// class: only the view carrying the assocOverlay mark takes the band
	// path; every other instance forwards to the saved original IMP.
	classPatch(id(send(view, "class")), "hitTest:", overlayHitIMP, "@@:{CGPoint=dd}")
	markAssoc(view, &assocOverlay)
}

var (
	bridgeOnce    sync.Once
	msgHitTest    func(obj id, sel objc.SEL, p NSPoint) id
	cgScrollWheel func(source uintptr, units int32, wheelCount uint32, w1, w2, w3 int32) uintptr
	cgEventSetLoc func(ev uintptr, p NSPoint)
)

// loadBridge binds the symbols the real-window tests drive a window with:
// a point-taking hitTest: message, CGEvent's scroll-wheel factory and
// location setter, and CFRelease for the created event (drag_guard.go
// binds the same symbol; re-registering the var is idempotent).
func loadBridge() {
	bridgeOnce.Do(func() {
		load()
		purego.RegisterFunc(&msgHitTest, msgSendAddr)
		purego.RegisterLibFunc(&cgScrollWheel, libCG, "CGEventCreateScrollWheelEvent2")
		purego.RegisterLibFunc(&cgEventSetLoc, libCG, "CGEventSetLocation")
		purego.RegisterLibFunc(&cfRelease, purego.RTLD_DEFAULT, "CFRelease")
	})
}

// SurfaceHitTest sends hitTest: to window's mygo surface view at the
// content point (x, y) — top-left origin, matching MyGo's flipped surface
// coordinates — and reports whether any view accepted it. It is the native
// side of "does this window answer clicks here": used by the gated
// real-window test that proves the notch's hit-test band does not leak
// into other windows. Main thread only.
func SurfaceHitTest(window uintptr, x, y float64) bool {
	if window == 0 {
		return false
	}
	if err := Init(); err != nil {
		return false
	}
	var hit bool
	withPool(func() {
		loadBridge()
		view := overlaySurfaceView(id(window))
		if view == 0 {
			return
		}
		// hitTest: wants the superview's unflipped coordinate space; mygo
		// callers think top-left, so Y comes in off the view's bottom.
		b := msgRect(view, sel("bounds"))
		hit = msgHitTest(view, sel("hitTest:"), NSPoint{X: x, Y: b.Size.Height - y}) != 0
	})
	return hit
}

// SurfaceScroll delivers a synthetic scroll-wheel event straight to
// window's mygo surface view (a window-local [view scrollWheel:] call — it
// never touches the system event stream). (x, y) is the content point the
// scroll lands on, top-left origin; deltaY is the wheel delta in pixels.
// Reports whether an event was delivered. A CGEvent-created NSEvent has no
// window; its locationInWindow is the CGEvent's global top-left screen
// point converted into the receiving view's coordinates, so the event is
// stamped with the screen point matching (x, y) inside this window.
// Main thread only.
func SurfaceScroll(window uintptr, x, y, deltaY float64) bool {
	if window == 0 {
		return false
	}
	if err := Init(); err != nil {
		return false
	}
	var ok bool
	withPool(func() {
		loadBridge()
		w := id(window)
		view := overlaySurfaceView(w)
		if view == 0 || cgScrollWheel == nil {
			return
		}
		// wheelCount 1, wheel1 = vertical delta; 1 = kCGScrollEventUnitPixel.
		// The wheel delta reports the gesture, so scrolling content down
		// (deltaY > 0) arrives as a negative wheel1.
		cg := cgScrollWheel(0, 1, 1, int32(-deltaY), 0, 0)
		if cg == 0 {
			return
		}
		defer cfRelease(cg)
		// A window-less NSEvent reports locationInWindow as the CGEvent's
		// location converted to AppKit's bottom-left global space, and the
		// receiver treats it as window-content coordinates. Stamp the
		// CGEvent with the bottom-left global point that reports back as
		// (x, viewH − y): the content point the scroll should land on.
		b := msgRect(view, sel("bounds"))
		screenH := primaryScreenHeight()
		cgEventSetLoc(cg, NSPoint{X: x, Y: screenH - (b.Size.Height - y)})
		ev := send(class("NSEvent"), "eventWithCGEvent:", cg)
		if ev == 0 {
			return
		}
		send(view, "scrollWheel:", uintptr(ev))
		ok = true
	})
	return ok
}

// WindowClassName returns window's Objective-C class name, for the gated
// test's assertion that no window was reclassed (the per-window patches
// gate on pointers inside one shared-class method, not subclasses).
// Main thread only.
func WindowClassName(window uintptr) string {
	if window == 0 {
		return ""
	}
	if err := Init(); err != nil {
		return ""
	}
	var name string
	withPool(func() {
		name = cstr(classGetName(send(id(window), "class")))
	})
	return name
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

// SetOverlayHitTest keeps the overlay in WindowServer's hit-test list (never
// ignoresMouseEvents) and decides which pixels of the flipped surface are
// solid. acceptAll is peeking, expanded, or an in-flight drag; otherwise
// only the hardware-notch band at the top of the strip receives events, so
// a file dragged onto the camera housing hits a drop target instead of the
// Spaces edge. Main thread only.
func SetOverlayHitTest(window uintptr, acceptAll bool, bandHeight int) {
	if bandHeight < 1 {
		bandHeight = 32
	}
	overlayAccept.Store(acceptAll)
	overlayBand.Store(int64(bandHeight))
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

//go:build darwin

// Package mac holds the AppKit/CoreGraphics calls Shelf needs and the mygo
// public API does not expose: pasteboard change counters, the system drag
// session probe, the pressed-button state and per-file icons.
//
// Everything is driven through the Objective-C runtime with purego, the
// same convention as mygo's internal/darwin (module-private, so it cannot
// be imported and is reimplemented here). No goroutines or event taps are
// installed. Mouse-only observation lives in clicks_darwin.go and requests
// no Accessibility or Input Monitoring permission. Call Init once on
// the main thread; every exported function must be called on the main
// thread (the parent's event loop already is).
package mac

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// id is an Objective-C object pointer, as in internal/darwin/objc.go.
type id = objc.ID

// NSSize mirrors the 64-bit CoreGraphics size.
type NSSize struct{ Width, Height float64 }

var (
	libObjC, libAppKit, libCG uintptr
	msgSendAddr               uintptr
	dragBoardName             id

	// Typed variants for arguments/returns that travel in floating-point
	// registers (RegisterFunc, not raw SyscallN).
	msgSetSize func(obj id, sel objc.SEL, s NSSize)

	// CGEventSourceButtonState reports hardware button state; reading it
	// requires no Input Monitoring permission (only *posting* events does).
	cgEventSourceButtonState func(state int32, button uint32) bool

	loadOnce sync.Once
	loadErr  error
)

const (
	// kCGEventSourceStateCombinedSessionState merges every event source in
	// the login session: the state seen by user-driven devices.
	kCGEventSourceStateCombinedSessionState = 0
	kCGMouseButtonLeft                      = 0

	// NSWindowCollectionBehavior bits (window.h). AppKit allows at most
	// one of the three FullScreen* bits at a time.
	collectionCanJoinAllSpaces    = 1 << 0
	collectionFullScreenPrimary   = 1 << 7
	collectionFullScreenAuxiliary = 1 << 8
	collectionFullScreenNone      = 1 << 9

	nsPNGFileType = 4 // NSPNGFileType
)

// load opens the frameworks once, mirroring internal/darwin's load(); it
// needs only libobjc, AppKit and CoreGraphics.
func load() {
	loadOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				loadErr = fmt.Errorf("mac: framework setup failed: %v", r)
			}
		}()
		var err error
		dlopen := func(path string) uintptr {
			h, e := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
			if e != nil {
				err = fmt.Errorf("mac: cannot load %s: %w", path, e)
				return 0
			}
			return h
		}
		libObjC = dlopen("/usr/lib/libobjc.A.dylib")
		libAppKit = dlopen("/System/Library/Frameworks/AppKit.framework/AppKit")
		libCG = dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
		if err != nil {
			loadErr = err
			return
		}
		msgSendAddr, err = purego.Dlsym(libObjC, "objc_msgSend")
		if err != nil {
			loadErr = fmt.Errorf("mac: %w", err)
			return
		}
		address, err := purego.Dlsym(libAppKit, "NSPasteboardNameDrag")
		if err != nil {
			loadErr = fmt.Errorf("mac: drag pasteboard constant: %w", err)
			return
		}
		// Keep the foreign address at the FFI boundary; copy its NSString
		// pointer into Go storage without treating native memory as a Go pointer.
		var copySymbol func(dst *id, src uintptr, size uintptr) uintptr
		purego.RegisterLibFunc(&copySymbol, purego.RTLD_DEFAULT, "memcpy")
		copySymbol(&dragBoardName, address, unsafe.Sizeof(dragBoardName))
		purego.RegisterFunc(&msgSetSize, msgSendAddr)
		purego.RegisterLibFunc(&cgEventSourceButtonState, libCG, "CGEventSourceButtonState")
	})
}

var (
	selMu    sync.RWMutex
	selCache = map[string]objc.SEL{}
	clsCache = map[string]id{}
)

// sel returns the selector for name, cached (internal/darwin convention).
func sel(name string) objc.SEL {
	selMu.RLock()
	s, ok := selCache[name]
	selMu.RUnlock()
	if ok {
		return s
	}
	s = objc.RegisterName(name)
	selMu.Lock()
	selCache[name] = s
	selMu.Unlock()
	return s
}

// class returns the class object for name, cached. It panics for classes
// that do not exist since that is a programming error.
func class(name string) id {
	selMu.RLock()
	c, ok := clsCache[name]
	selMu.RUnlock()
	if ok {
		return c
	}
	c = id(objc.GetClass(name))
	if c == 0 {
		panic("mac: Objective-C class " + name + " not found")
	}
	selMu.Lock()
	clsCache[name] = c
	selMu.Unlock()
	return c
}

// send sends a message whose arguments are integers or object pointers, as
// send() in internal/darwin/objc.go. It lazily loads the frameworks.
//
//go:uintptrescapes
func send(obj id, selector string, args ...uintptr) id {
	load()
	var a [12]uintptr
	a[0], a[1] = uintptr(obj), uintptr(sel(selector))
	n := copy(a[2:], args)
	r, _, _ := purego.SyscallN(msgSendAddr, a[:n+2]...)
	return id(r)
}

//go:uintptrescapes
func sendInt(obj id, selector string, args ...uintptr) int {
	return int(send(obj, selector, args...))
}

//go:uintptrescapes
func sendInt64(obj id, selector string, args ...uintptr) int64 {
	return int64(send(obj, selector, args...))
}

const nsUTF8StringEncoding = 4

// nsString returns an autoreleased NSString.
func nsString(s string) id {
	b := unsafe.StringData(s)
	str := send(send(class("NSString"), "alloc"), "initWithBytes:length:encoding:",
		uintptr(unsafe.Pointer(b)), uintptr(len(s)), nsUTF8StringEncoding)
	runtime.KeepAlive(s)
	return send(str, "autorelease")
}

// goString copies an NSString into a Go string.
func goString(str id) string {
	if str == 0 {
		return ""
	}
	p := send(str, "UTF8String")
	if p == 0 {
		return ""
	}
	n := sendInt(str, "lengthOfBytesUsingEncoding:", nsUTF8StringEncoding)
	return string(unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n))
}

// goBytes copies an NSData.
func goBytes(data id) []byte {
	if data == 0 {
		return nil
	}
	n := sendInt(data, "length")
	if n == 0 {
		return []byte{}
	}
	p := send(data, "bytes")
	return append([]byte(nil), unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n)...)
}

var poolPushFn, poolPopFn uintptr

// withPool runs fn inside an autorelease pool on the calling thread, as
// internal/darwin does: the pool belongs to the thread that pushed it.
func withPool(fn func()) {
	_ = Init()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool, _, _ := purego.SyscallN(poolPushFn)
	defer purego.SyscallN(poolPopFn, pool)
	fn()
}

// Init opens the Objective-C runtime and the frameworks this package uses.
// Call it once on the main thread before the other functions; they call it
// lazily themselves too, so calling Init first only surfaces the error
// early. It installs no monitors, event taps or observers and requests no
// permissions.
func Init() error {
	load()
	if loadErr == nil {
		poolPushFn, _ = purego.Dlsym(libObjC, "objc_autoreleasePoolPush")
		poolPopFn, _ = purego.Dlsym(libObjC, "objc_autoreleasePoolPop")
		if poolPushFn == 0 || poolPopFn == 0 {
			loadErr = errors.New("mac: autorelease pool entry points missing")
		}
	}
	return loadErr
}

// generalPasteboard returns the shared NSPasteboard (NSGeneralPboard).
func generalPasteboard() id { return send(class("NSPasteboard"), "generalPasteboard") }

// dragPasteboard returns the pasteboard that backs drag sessions
// (NSDragPboard). The object persists across sessions; only its contents
// change, so changeCount is the serial that marks drag activity.
func dragPasteboard() id {
	return send(class("NSPasteboard"), "pasteboardWithName:", uintptr(dragBoardName))
}

// ClipboardChangeCount returns the general pasteboard's changeCount: it
// increments for every clipboard mutation system-wide. Poll it (~0.5 s)
// instead of keeping clipboard text in memory; when it differs from
// the last seen value, read mygo.Clipboard for the new item. Safe while
// the app is inactive; main thread only.
func ClipboardChangeCount() int64 {
	var n int64
	withPool(func() { n = sendInt64(generalPasteboard(), "changeCount") })
	return n
}

// ClipboardTypes returns the general pasteboard's declared type
// identifiers in the source's order of preference (most faithful first).
// It reads metadata only, never the clipboard contents. Main thread only.
func ClipboardTypes() []string {
	var out []string
	withPool(func() { out = pasteboardTypes(generalPasteboard()) })
	return out
}

// pasteboardTypes lists board's declared types; call inside withPool.
func pasteboardTypes(board id) []string {
	types := send(board, "types")
	n := sendInt(types, "count")
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, goString(send(types, "objectAtIndex:", uintptr(i))))
	}
	return out
}

// LeftMouseDown reports whether the left (primary) mouse button is
// physically held right now, read through CGEventSourceButtonState on the
// combined-session state — the same source the window server consults, so
// it stays true during a drag into or out of another app. This is state,
// not an event tap: no permission prompt. Main thread only.
func LeftMouseDown() bool {
	load()
	return cgEventSourceButtonState(kCGEventSourceStateCombinedSessionState, kCGMouseButtonLeft)
}

// dragTypeKnown lists types a Finder/NSDraggingSession item exposes on the
// drag pasteboard. It is a confirmation signal for a freshly bumped board
// (a count bump with no declared type means the board was cleared, not a
// drag), NOT an activity signal by itself: types stay declared after the
// drop, so testing them alone makes an ordinary click after any past drag
// look like a new drag.
var dragTypeKnown = []string{
	"NSFilenamesPboardType",
	"public.file-url",
	"public.url",
	"public.utf8-plain-text",
	"public.utf16-plain-text",
	"public.tiff",
	"public.png",
	"public.data",
	"public.content",
	"com.apple.NSFilePromiseItemMetaData",
	"com.apple.pasteboard.promised-file-url",
}

// dragPasteboardHasTypes reports whether the drag pasteboard declares at
// least one known public type right now. Stale after a drop; only
// meaningful at the moment a changeCount bump is observed.
func dragPasteboardHasTypes(board id) bool {
	for _, name := range pasteboardTypes(board) {
		if slices.Contains(dragTypeKnown, name) {
			return true
		}
	}
	return false
}

// dragTracker is the state machine behind DragState, kept pure (no ObjC)
// so its boundary behavior is unit-testable: step takes the observed drag
// pasteboard changeCount, whether the board declared items at that count,
// and whether the left button is held.
type dragTracker struct {
	primed  bool  // the first sample only baselines, never reports a drag
	base    int64 // changeCount attributed to the current state
	serial  int64
	active  bool
	wasDown bool
}

// step folds one observation into the tracker.
//
// Semantics, matching the DragState doc comment:
//   - first sample only establishes a baseline (stale board contents from
//     before the app started are never reported as a drag);
//   - button release ends the hold and re-baselines the board, so the next
//     press needs a *new* count to arm;
//   - on a fresh press, a board change that arrived since the previous
//     sample arms immediately IF the board declares items — a typed bump
//     only ever comes from a drag session declaring items, and a press
//     that coincides with one is the drag's own start;
//   - while a hold is already active, any count change is consumed into
//     the same serial (session rewrites do not look like a new drag);
//   - during a hold that is not yet a drag, the first count change arms
//     exactly one serial per hold — but only if the board declares items
//     (typed); a bump on an empty board is a clear, not a drag.
func (t *dragTracker) step(count int64, typed, down bool) {
	if !t.primed {
		t.primed = true
		t.base = count
		t.wasDown = down
		return
	}
	if !down {
		t.active = false
		t.wasDown = false
		t.base = count
		return
	}
	if !t.wasDown {
		t.wasDown = true
		if count != t.base && typed {
			// Drag began inside the same poll window as the press.
			t.serial++
			t.active = true
		} else {
			t.active = false
		}
		t.base = count
		return
	}
	if t.active {
		if count != t.base {
			t.base = count // consume session rewrites into the same drag
		}
		return
	}
	if count != t.base && typed {
		t.serial++
		t.active = true
	}
	t.base = count
}

// tracker is the live DragState state, main thread only.
var tracker dragTracker

// DragState snapshots whether a system drag session is in flight.
//
// Detection: NSDraggingSession's pasteboard is the shared NSDragPboard.
// Its changeCount increments when a dragging source declares items — i.e.
// when a drag starts — and again if the source rewrites promised data
// mid-drag. A drag also requires the button held, so
//
//	active = a changeCount arrived *while the left button was down*
//	         AND the board then carried a known drag type,
//
// and stays active until the button comes up. serial increments once per
// detected drag generation (per button hold), so the parent can tell
// "the same drag I've been tracking" from "a new drag began" without a
// global monitor.
//
// Stale-board caveat: NSDragPboard keeps the last session's declared types
// after the drop ends. That is why changeCount, not types, drives the
// state machine, why a fresh press re-baselines the board, and why the
// first sample after Init only primes: an ordinary click — or the very
// first observation — cannot resurrect the board's previous drag.
//
// Accepted limitations:
//   - up to one poll interval of latency (parent polls ~30 ms);
//   - a drag in flight before the first sample is invisible until its
//     source next writes the board (usually never) — the alternative,
//     treating a held button over a stale board as a drag, false-fires on
//     every click-hold after any drag;
//   - a source that never writes NSDragPboard (custom private drags) is
//     invisible;
//   - our own outbound drags (mygo StartDataDrag) count as drags too —
//     the parent should suppress on its own session instead.
//
// Reading the metadata is legal while inactive and triggers no prompt.
// Main thread only.
func DragState() (active bool, serial int64) {
	withPool(func() {
		board := dragPasteboard()
		count := sendInt64(board, "changeCount")
		down := LeftMouseDown()
		typed := count != tracker.base && dragPasteboardHasTypes(board)
		tracker.step(count, typed, down)
	})
	return tracker.active, tracker.serial
}

// resolveCollectionBehavior returns behavior with all three mutually
// exclusive fullscreen bits (Primary/Auxiliary/None) removed and
// FullScreenAuxiliary plus CanJoinAllSpaces set. mygo's window options
// set exactly one of them (FullScreenNone for DisableFullScreen,
// otherwise FullScreenPrimary); OR-ing Auxiliary on top without clearing
// the old bit violates AppKit's "at most one" rule, so the conflicting
// bit is cleared first and every other behavior bit is preserved.
func resolveCollectionBehavior(behavior uint) uint {
	behavior &^= collectionFullScreenPrimary | collectionFullScreenAuxiliary | collectionFullScreenNone
	return behavior | collectionCanJoinAllSpaces | collectionFullScreenAuxiliary
}

// JoinFullScreenSpaces makes the panel usable alongside another app's
// fullscreen window: it resolves the collection behavior (see above) and
// applies it, then reads back the window's actual behavior for the
// caller to log/verify. This never activates or makes a window key.
func JoinFullScreenSpaces(window uintptr) uint {
	var out uint
	withPool(func() {
		win := id(window)
		behavior := resolveCollectionBehavior(uint(sendInt(win, "collectionBehavior")))
		send(win, "setCollectionBehavior:", uintptr(behavior))
		out = uint(sendInt(win, "collectionBehavior"))
	})
	return out
}

// FileIcon returns the PNG bytes of the Finder icon for path rendered at
// size×size points (NSImage honors the display's backing scale). Pipeline:
// NSWorkspace.iconForFile: → NSImage.TIFFRepresentation → NSBitmapImageRep
// representationUsingType:NSPNGFileType. nil is returned when the icon or
// conversion fails (a nonexistent path still yields the generic document
// icon). Read-only: never writes the file or the clipboard, no network.
// Main thread only; cache per path — the parent does.
func FileIcon(path string, size int) []byte {
	var out []byte
	withPool(func() {
		ws := send(class("NSWorkspace"), "sharedWorkspace")
		icon := send(ws, "iconForFile:", uintptr(nsString(path)))
		if icon == 0 {
			return
		}
		if size > 0 {
			msgSetSize(icon, sel("setSize:"), NSSize{Width: float64(size), Height: float64(size)})
		}
		tiff := send(icon, "TIFFRepresentation")
		if tiff == 0 {
			return
		}
		rep := send(send(class("NSBitmapImageRep"), "alloc"), "initWithData:", uintptr(tiff))
		if rep == 0 {
			return
		}
		defer send(rep, "release")
		png := send(rep, "representationUsingType:properties:", nsPNGFileType, 0)
		out = goBytes(png)
	})
	return out
}

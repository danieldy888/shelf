//go:build darwin

package mac

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

// Window list introspection for tests and diagnostics: CGWindowList is how
// WindowServer itself sees our overlay — a shown basket that cannot be
// found here is invisible no matter what AppKit believes.

var (
	cfLibrary           uintptr
	cfDictGetValue      func(dict, key id) id
	cfArrayCount        func(arr id) int
	cfArrayValueAtIndex func(arr id, idx int) id
	cfReleaseWL         func(cf uintptr)
	windowListCopy      func(option uint32, winID uint32) id
	rectFromDict        func(dict id, rect *NSRect) bool
	windowBoundsKey     id
	windowListOnce      sync.Once
	windowListErr       error
)

// kCGWindowListOptionIncludingWindow asks the list for one window only.
const kCGWindowListOptionIncludingWindow = 1 << 3

// loadWindowList resolves the CoreFoundation/CoreGraphics entry points once.
// Called lazily by WindowScreenRect; Init must already have run (it has —
// callers reach us only after the app is up).
func loadWindowList() error {
	windowListOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				windowListErr = fmt.Errorf("mac: window list setup: %v", r)
			}
		}()
		var err error
		cfLibrary, err = purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			windowListErr = err
			return
		}
		purego.RegisterLibFunc(&cfDictGetValue, cfLibrary, "CFDictionaryGetValue")
		purego.RegisterLibFunc(&cfArrayCount, cfLibrary, "CFArrayGetCount")
		purego.RegisterLibFunc(&cfArrayValueAtIndex, cfLibrary, "CFArrayGetValueAtIndex")
		purego.RegisterLibFunc(&cfReleaseWL, cfLibrary, "CFRelease")
		purego.RegisterLibFunc(&windowListCopy, libCG, "CGWindowListCopyWindowInfo")
		purego.RegisterLibFunc(&rectFromDict, libCG, "CGRectMakeWithDictionaryRepresentation")
		keyAddr, err := purego.Dlsym(libCG, "kCGWindowBounds")
		if err != nil {
			windowListErr = err
			return
		}
		var copySymbol func(dst *id, src uintptr, size uintptr) uintptr
		purego.RegisterLibFunc(&copySymbol, purego.RTLD_DEFAULT, "memcpy")
		copySymbol(&windowBoundsKey, keyAddr, 8)
	})
	return windowListErr
}

// WindowNumber returns the NSWindow's CGWindowID — the number
// CGWindowListCopyWindowInfo and `screencapture -l` both use.
// Main thread only.
func WindowNumber(window uintptr) int {
	var n int
	withPool(func() {
		n = sendInt(id(window), "windowNumber")
	})
	return n
}

// WindowScreenRect returns the on-screen rect WindowServer reports for the
// window with CGWindowID num, in MyGo top-left screen coordinates. ok is
// false while the window is not ordered in. Main thread only.
func WindowScreenRect(num int) (r NSRect, ok bool) {
	if err := Init(); err != nil || num == 0 {
		return r, false
	}
	if err := loadWindowList(); err != nil {
		return r, false
	}
	withPool(func() {
		list := windowListCopy(kCGWindowListOptionIncludingWindow, uint32(num))
		if list == 0 {
			return
		}
		defer cfReleaseWL(uintptr(list))
		if cfArrayCount(list) < 1 {
			return
		}
		dict := cfArrayValueAtIndex(list, 0)
		bounds := cfDictGetValue(dict, windowBoundsKey)
		if bounds == 0 {
			return
		}
		var out NSRect
		if rectFromDict(bounds, &out) {
			r, ok = out, true
		}
	})
	return r, ok
}

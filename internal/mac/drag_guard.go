//go:build darwin

package mac

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
)

// CGPoint uses global display coordinates, with Y increasing downwards.
type CGPoint struct{ X, Y float64 }

// DragGuardRegion is the notch's protected approach corridor, in screen points.
// Drag events inside it stop at its lower edge, which remains inside the shelf's
// drop target. Other displays and the rest of the menu bar are unaffected.
type DragGuardRegion struct{ X, Y, Width, Height int }

const (
	kCGHIDEventTap                = 0
	kCGSessionEventTap            = 1
	kCGHeadInsertEventTap         = 0
	kCGEventTapOptionDefault      = 0
	kCGEventLeftMouseDown         = 1
	kCGEventMouseMoved            = 5
	kCGEventLeftMouseDragged      = 6
	kCGEventOtherMouseDragged     = 27
	cgEventTapDisabledByTimeout   = 0xFFFFFFFE
	cgEventTapDisabledByUserInput = 0xFFFFFFFF
)

var (
	libCoreFoundation             uintptr
	cgEventTapCreate              func(tap, place, options uint32, mask uint64, callback, userInfo uintptr) uintptr
	cgEventTapEnable              func(tap uintptr, enable bool)
	cgEventGetLocation            func(event uintptr) CGPoint
	cgEventSetLocation            func(event uintptr, point CGPoint)
	cgWarpMouseCursorPosition     func(point CGPoint)
	axIsProcessTrusted            func() bool
	axIsProcessTrustedWithOptions func(options uintptr) bool
	axPrompted                    atomic.Bool
	cfRunLoopGetMain              func() uintptr
	cfMachPortCreateRunLoopSource func(allocator, port uintptr, order int) uintptr
	cfRunLoopAddSource            func(runLoop, source, mode uintptr)
	cfMachPortInvalidate          func(port uintptr)
	cfRunLoopSourceInvalidate     func(source uintptr)
	cfRelease                     func(cf uintptr)
	cfRunLoopCommonModes          uintptr
	dragGuardOnce                 sync.Once
	dragGuardErr                  error
	dragGuardArmed                atomic.Pointer[dragGuardState]
	dragGuardCallbackOnce         sync.Once
	dragGuardCallback             uintptr
)

type dragGuardState struct {
	region DragGuardRegion
	port   uintptr
}

func loadDragGuard() error {
	dragGuardOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				dragGuardErr = fmt.Errorf("mac: drag guard setup failed: %v", r)
			}
		}()
		var err error
		libCoreFoundation, err = purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			dragGuardErr = err
			return
		}
		services, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			dragGuardErr = err
			return
		}
		purego.RegisterLibFunc(&axIsProcessTrusted, services, "AXIsProcessTrusted")
		purego.RegisterLibFunc(&axIsProcessTrustedWithOptions, services, "AXIsProcessTrustedWithOptions")
		purego.RegisterLibFunc(&cgEventTapCreate, libCG, "CGEventTapCreate")
		purego.RegisterLibFunc(&cgEventTapEnable, libCG, "CGEventTapEnable")
		purego.RegisterLibFunc(&cgEventGetLocation, libCG, "CGEventGetLocation")
		purego.RegisterLibFunc(&cgEventSetLocation, libCG, "CGEventSetLocation")
		purego.RegisterLibFunc(&cgWarpMouseCursorPosition, libCG, "CGWarpMouseCursorPosition")
		purego.RegisterLibFunc(&cfRunLoopGetMain, libCoreFoundation, "CFRunLoopGetMain")
		purego.RegisterLibFunc(&cfMachPortCreateRunLoopSource, libCoreFoundation, "CFMachPortCreateRunLoopSource")
		purego.RegisterLibFunc(&cfRunLoopAddSource, libCoreFoundation, "CFRunLoopAddSource")
		purego.RegisterLibFunc(&cfMachPortInvalidate, libCoreFoundation, "CFMachPortInvalidate")
		purego.RegisterLibFunc(&cfRunLoopSourceInvalidate, libCoreFoundation, "CFRunLoopSourceInvalidate")
		purego.RegisterLibFunc(&cfRelease, libCoreFoundation, "CFRelease")
		addr, err := purego.Dlsym(libCoreFoundation, "kCFRunLoopCommonModes")
		if err != nil {
			dragGuardErr = err
			return
		}
		var copySymbol func(dst *uintptr, src uintptr, size uintptr) uintptr
		purego.RegisterLibFunc(&copySymbol, purego.RTLD_DEFAULT, "memcpy")
		copySymbol(&cfRunLoopCommonModes, addr, unsafe.Sizeof(cfRunLoopCommonModes))
	})
	return dragGuardErr
}

// AccessibilityTrusted reports whether this process may install an active
// event tap. prompt=true asks macOS once (System Settings on recent
// releases) if the answer is currently no.
func AccessibilityTrusted(prompt bool) bool {
	if err := loadDragGuard(); err != nil {
		return false
	}
	if axIsProcessTrusted() {
		return true
	}
	if !prompt || !axPrompted.CompareAndSwap(false, true) {
		return axIsProcessTrusted()
	}
	if axIsProcessTrustedWithOptions == nil {
		return false
	}
	trusted := false
	withPool(func() {
		key := nsString("AXTrustedCheckOptionPrompt")
		num := send(class("NSNumber"), "numberWithBool:", 1)
		dict := send(class("NSDictionary"), "dictionaryWithObject:forKey:", uintptr(num), uintptr(key))
		trusted = axIsProcessTrustedWithOptions(uintptr(dict))
	})
	return trusted
}

func clampDragPoint(p CGPoint, r DragGuardRegion) CGPoint {
	if r.Width <= 0 || r.Height <= 0 || p.X < float64(r.X) || p.X >= float64(r.X+r.Width) ||
		p.Y < float64(r.Y) || p.Y >= float64(r.Y+r.Height) {
		return p
	}
	p.Y = float64(r.Y + r.Height)
	return p
}

// A single trampoline serves every drag; purego callbacks cannot be freed.
func dragGuardCallbackAddr() uintptr {
	dragGuardCallbackOnce.Do(func() {
		dragGuardCallback = purego.NewCallback(func(_ uintptr, typ uint32, event uintptr, _ uintptr) uintptr {
			s := dragGuardArmed.Load()
			if s == nil {
				return event
			}
			switch typ {
			case kCGEventMouseMoved:
				if !cgEventSourceButtonState(kCGEventSourceStateCombinedSessionState, kCGMouseButtonLeft) {
					return event
				}
				fallthrough
			case kCGEventLeftMouseDown, kCGEventLeftMouseDragged, kCGEventOtherMouseDragged:
				if event != 0 {
					p := cgEventGetLocation(event)
					if q := clampDragPoint(p, s.region); q != p {
						// Rewrite the event and warp the hardware cursor.
						// WindowServer's Spaces edge reads the cursor, not
						// only the dragged-event location.
						cgEventSetLocation(event, q)
						cgWarpMouseCursorPosition(q)
					}
				}
			case cgEventTapDisabledByTimeout, cgEventTapDisabledByUserInput:
				// The event argument is NOT the tap's mach port.
				cgEventTapEnable(s.port, true)
			}
			return event
		})
	})
	return dragGuardCallback
}

// StartDragGuard installs an active mouse-drag filter. It never requests a
// permission or moves the cursor separately from its event. Missing trust or
// native setup failure is returned to the UI instead of silently disabling it.
// Only one guard may be active; start/stop are called on the AppKit main thread.
func StartDragGuard(region DragGuardRegion) (func(), error) {
	if region.Width <= 0 || region.Height <= 0 {
		return nil, errors.New("刘海拖拽保护区域无效")
	}
	if err := Init(); err != nil {
		return nil, err
	}
	if err := loadDragGuard(); err != nil {
		return nil, err
	}
	if !AccessibilityTrusted(false) {
		return nil, errors.New("Shelf 尚未获得辅助功能授权，无法拦截拖到屏幕顶部时的桌面栏；请在系统设置的隐私与安全性中勾选 Shelf")
	}
	if dragGuardArmed.Load() != nil {
		return nil, errors.New("mac: a drag guard is already active")
	}
	// HID-level first: WindowServer's edge gesture reads the cursor
	// before a session tap can rewrite it. Fall back to a session tap
	// when the process is trusted but the HID tap is refused.
	const dragMask = 1<<kCGEventLeftMouseDragged | 1<<kCGEventLeftMouseDown | 1<<kCGEventMouseMoved | 1<<kCGEventOtherMouseDragged
	port := cgEventTapCreate(kCGHIDEventTap, kCGHeadInsertEventTap, kCGEventTapOptionDefault,
		dragMask, dragGuardCallbackAddr(), 0)
	if port == 0 {
		port = cgEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap, kCGEventTapOptionDefault,
			dragMask, dragGuardCallbackAddr(), 0)
	}
	if port == 0 {
		return nil, errors.New("mac: unable to create active drag filter")
	}
	source := cfMachPortCreateRunLoopSource(0, port, 0)
	if source == 0 {
		cfMachPortInvalidate(port)
		cfRelease(port)
		return nil, errors.New("mac: unable to create drag filter run-loop source")
	}
	dragGuardArmed.Store(&dragGuardState{region: region, port: port})
	cfRunLoopAddSource(cfRunLoopGetMain(), source, cfRunLoopCommonModes)
	cgEventTapEnable(port, true)
	var once sync.Once
	return func() {
		once.Do(func() {
			dragGuardArmed.Store(nil)
			cgEventTapEnable(port, false)
			cfRunLoopSourceInvalidate(source)
			cfMachPortInvalidate(port)
			cfRelease(source)
			cfRelease(port)
		})
	}, nil
}

// SetDragGuardRegion updates the armed corridor without tearing the tap
// down. No-op when no guard is running.
func SetDragGuardRegion(region DragGuardRegion) {
	s := dragGuardArmed.Load()
	if s == nil || region.Width <= 0 || region.Height <= 0 {
		return
	}
	dragGuardArmed.Store(&dragGuardState{region: region, port: s.port})
}

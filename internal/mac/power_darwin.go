//go:build darwin

package mac

// Battery state through IOKit IOPowerSources, the same source the menu-bar
// battery item reads. Reads only: no power assertions, no SMUs, nothing
// that can change the machine's power management. A CFRunLoop source on the
// main run loop raises a flag on any power-source event so the parent never
// has to poll faster than its existing tick.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
)

// The drag guard binds its own CoreFoundation functions under generic
// names (cfRelease, cfRunLoopAddSource); the ones below are power-private
// so either feature can load without the other.
var (
	libPowerCF uintptr

	iopsCopyPowerSourcesInfo   func() uintptr
	iopsCopyPowerSourcesList   func(info uintptr) uintptr
	iopsGetPowerSourceDesc     func(info, ps uintptr) uintptr
	iopsGetProvidingSourceType func(info uintptr) uintptr
	iopsNotifyRunLoopSource    func(cb uintptr, ctx uintptr) uintptr
	powerCFRelease             func(ref uintptr)
	powerCFRunLoopGetMain      func() uintptr
	powerCFRunLoopAddSource    func(rl, source, mode uintptr)
	powerCFDefaultMode         uintptr // kCFRunLoopDefaultMode's value

	powerOnce    sync.Once
	powerErr     error
	powerWatch   sync.Once
	powerDirty   atomic.Bool
	powerWatchOK bool
	powerCB      uintptr
)

const (
	powerSourceBattery = "Battery Power"
	// IOPSCurrentCapacity is a percentage of MaxCapacity (normally 100).
	// These keys are #defines in IOPowerSources.h, not exported symbols.
	kIOPSType        = "Type"
	kIOPSTypeBattery = "InternalBattery"
	kIOPSIsPresent   = "Is Present"
	kIOPSCapacity    = "Current Capacity"
	kIOPSMaxCapacity = "Max Capacity"
)

// PowerSnapshot is one observation of the machine's power state.
// On desktops and VMs without a battery HasBattery is false and the other
// fields are meaningless.
type PowerSnapshot struct {
	HasBattery bool
	// Charging is true whenever the machine is not running on battery —
	// plugged in (AC or UPS), including a full battery on the charger.
	Charging bool
	Percent  int // 0–100, only valid with HasBattery
}

// ReadPower snapshots the current power source and the internal battery's
// charge. Cheap enough to poll a few times a second; main thread only.
func ReadPower() PowerSnapshot {
	var s PowerSnapshot
	if err := ensurePower(); err != nil {
		return s
	}
	withPool(func() {
		info := iopsCopyPowerSourcesInfo()
		if info == 0 {
			return
		}
		defer powerCFRelease(info)
		s.Charging = goString(id(iopsGetProvidingSourceType(info))) != powerSourceBattery
		list := iopsCopyPowerSourcesList(info)
		if list == 0 {
			return
		}
		defer powerCFRelease(list)
		board := id(list)
		for i, n := 0, sendInt(board, "count"); i < n; i++ {
			ps := uintptr(send(board, "objectAtIndex:", uintptr(i)))
			d := id(iopsGetPowerSourceDesc(info, ps))
			if d == 0 || goString(send(d, "objectForKey:", uintptr(nsString(kIOPSType)))) != kIOPSTypeBattery {
				continue
			}
			pres := send(d, "objectForKey:", uintptr(nsString(kIOPSIsPresent)))
			// boolValue is a BOOL return: only the low byte is defined.
			if pres == 0 || send(pres, "boolValue")&0xFF == 0 {
				// An internal battery slot that is absent is still not a battery.
				continue
			}
			cur := sendInt(send(d, "objectForKey:", uintptr(nsString(kIOPSCapacity))), "intValue")
			max := sendInt(send(d, "objectForKey:", uintptr(nsString(kIOPSMaxCapacity))), "intValue")
			s.HasBattery = true
			s.Percent = cur
			if max > 0 && max != 100 {
				s.Percent = (cur*100 + max/2) / max
			}
			if s.Percent < 0 {
				s.Percent = 0
			} else if s.Percent > 100 {
				s.Percent = 100
			}
		}
	})
	return s
}

// ensurePower opens IOKit and CoreFoundation once. A failure leaves the
// battery feature inert (ReadPower reports no battery) — a Mac whose
// IOPowerSources cannot be read is treated like a desktop.
func ensurePower() error {
	powerOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				powerErr = fmt.Errorf("mac: power bindings: %v", r)
			}
		}()
		var err error
		libIOKit, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			powerErr = err
			return
		}
		if libPowerCF, err = purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_GLOBAL); err != nil {
			powerErr = err
			return
		}
		reg := func(f any, h uintptr, name string) {
			if err == nil {
				purego.RegisterLibFunc(f, h, name)
			}
		}
		reg(&iopsCopyPowerSourcesInfo, libIOKit, "IOPSCopyPowerSourcesInfo")
		reg(&iopsCopyPowerSourcesList, libIOKit, "IOPSCopyPowerSourcesList")
		reg(&iopsGetPowerSourceDesc, libIOKit, "IOPSGetPowerSourceDescription")
		reg(&iopsGetProvidingSourceType, libIOKit, "IOPSGetProvidingPowerSourceType")
		reg(&iopsNotifyRunLoopSource, libIOKit, "IOPSNotificationCreateRunLoopSource")
		reg(&powerCFRelease, libPowerCF, "CFRelease")
		reg(&powerCFRunLoopGetMain, libPowerCF, "CFRunLoopGetMain")
		reg(&powerCFRunLoopAddSource, libPowerCF, "CFRunLoopAddSource")
		if err != nil {
			powerErr = err
			return
		}
		address, e := purego.Dlsym(libPowerCF, "kCFRunLoopDefaultMode")
		if e != nil {
			powerErr = e
			return
		}
		var copySymbol func(dst *uintptr, src uintptr, size uintptr) uintptr
		purego.RegisterLibFunc(&copySymbol, purego.RTLD_DEFAULT, "memcpy")
		copySymbol(&powerCFDefaultMode, address, unsafe.Sizeof(powerCFDefaultMode))
	})
	return powerErr
}

// WatchPowerSources installs an IOPSNotification run-loop source on the
// main run loop so every power-source event (charger, battery capacity
// updates) sets the flag ConsumePowerChanged hands out. Harmless to call
// repeatedly; reports whether notifications were installed — false means
// the caller should fall back to slow polling. Main thread only.
func WatchPowerSources() bool {
	if err := ensurePower(); err != nil {
		return false
	}
	powerWatch.Do(func() {
		powerCB = purego.NewCallback(func(uintptr) { powerDirty.Store(true) })
		src := iopsNotifyRunLoopSource(powerCB, 0)
		if src == 0 {
			return
		}
		rl := powerCFRunLoopGetMain()
		if rl == 0 || powerCFDefaultMode == 0 {
			powerCFRelease(src)
			return
		}
		powerCFRunLoopAddSource(rl, src, powerCFDefaultMode)
		powerCFRelease(src)
		powerWatchOK = true
	})
	return powerWatchOK
}

// ConsumePowerChanged reports whether a power-source event arrived since
// the last call, clearing the flag either way.
func ConsumePowerChanged() bool { return powerDirty.Swap(false) }

// providingPowerSource returns the display name of the source the machine
// is drawing from ("AC Power", "Battery Power", "UPS Power") for probes.
func providingPowerSource() string {
	if err := ensurePower(); err != nil {
		return ""
	}
	var name string
	withPool(func() {
		info := iopsCopyPowerSourcesInfo()
		if info == 0 {
			return
		}
		defer powerCFRelease(info)
		name = goString(id(iopsGetProvidingSourceType(info)))
	})
	return name
}
